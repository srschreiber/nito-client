// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/voice"
	"github.com/srschreiber/nito-client/sounds"
)

// ShowVoiceSettingsMsg is emitted by RoomActionsComponent to open the voice settings screen.
type ShowVoiceSettingsMsg struct{}

// HideVoiceSettingsMsg is emitted by VoiceSettingsScreen when the user presses ESC.
type HideVoiceSettingsMsg struct{}

type vsSection int

const (
	vsSectionInput  vsSection = iota
	vsSectionOutput           // informational only; output device selection not supported
	vsSectionTest
	vsSectionTransformations
	vsSectionAdvanced
	vsSectionCount
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// vsSpinnerTickMsg drives the voice-connecting spinner in RoomActionsComponent.
type vsSpinnerTickMsg struct{}

// vsTestAudioTickMsg drives the test-audio connecting spinner in VoiceSettingsScreen.
// Kept separate from vsSpinnerTickMsg to prevent the two tick loops from
// cross-scheduling each other into an exponential storm.
type vsTestAudioTickMsg struct{}

// vsStatsTickMsg fires every second to refresh send metrics while test audio is active.
type vsStatsTickMsg struct{}

// VoiceSettingsScreen is a full-screen overlay for audio device selection and test audio.
type VoiceSettingsScreen struct {
	width, height       int
	section             vsSection
	inputDevices        []voice.AudioDevice
	inputCursor         int
	outputCursor        int // index within the AUDIO OUTPUT section (0=master, 1=chatSFX toggle, 2=chatSFX slider)
	testAudioActive     bool
	testAudioConnecting bool // true while JoinSelf is in progress
	advancedCursor      int  // index within the ADVANCED section (0=jitter, 1=denoise, 2=playback toggle, 3=playback slider)
	transformCursor     int  // index within the TRANSFORMATIONS section (0=pitch, 1=vibrato, 2=vib freq, 3=vib range)
	spinnerFrame        int
	voiceActive         bool // disables test audio while voice chat is running
	sendPacketsPerSec   float64
	sendKBPerSec        float64
	pipelineLatMs       float64
	networkRTTMs        float64
	avgEncodeMs         float64
	avgDecodeMs         float64
}

func NewVoiceSettingsScreen(termW, termH int) *VoiceSettingsScreen {
	return &VoiceSettingsScreen{width: termW, height: termH}
}

func (s *VoiceSettingsScreen) SetSize(termW, termH int) {
	s.width = termW
	s.height = termH
}

// Reset refreshes the device list and resets navigation to the top. Call when showing the screen.
func (s *VoiceSettingsScreen) Reset() {
	s.inputDevices = voice.ListAudioInputs()
	// Restore cursor to currently-selected device.
	sel := voice.SelectedInputDevice()
	s.inputCursor = 0
	for i, d := range s.inputDevices {
		if d.ID == sel {
			s.inputCursor = i
			break
		}
	}
	s.section = vsSectionInput
}

func (s *VoiceSettingsScreen) Init() tea.Cmd { return nil }

func (s *VoiceSettingsScreen) spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return vsTestAudioTickMsg{} })
}

func (s *VoiceSettingsScreen) statsTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return vsStatsTickMsg{} })
}

func (s *VoiceSettingsScreen) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case roomsVoiceResultMsg:
		if msg.err == nil {
			s.voiceActive = msg.joined
		}
	case roomsTestAudioResultMsg:
		s.testAudioConnecting = false
		if msg.err == nil {
			s.testAudioActive = msg.active
			if msg.active {
				return s.statsTick()
			}
			s.sendPacketsPerSec = 0
			s.sendKBPerSec = 0
		}
	case vsTestAudioTickMsg:
		if s.testAudioConnecting {
			s.spinnerFrame = (s.spinnerFrame + 1) % len(spinnerFrames)
			return s.spinnerTick()
		}
	case vsStatsTickMsg:
		if s.testAudioActive {
			pkts, bytes := voice.DrainSendStats()
			s.sendPacketsPerSec = float64(pkts)
			s.sendKBPerSec = float64(bytes) / 1024.0
			s.pipelineLatMs = voice.GetPipelineLatMs()
			s.networkRTTMs = voice.GetNetworkRTTMs()
			s.avgEncodeMs = voice.DrainEncodeStats()
			s.avgDecodeMs = voice.DrainDecodeStats()
			return s.statsTick()
		}
	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	return nil
}

func (s *VoiceSettingsScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	defer voice.SaveAudioSettings()
	switch msg.String() {
	case "esc":
		if s.testAudioConnecting {
			return nil // blocked; wait for connect to finish
		}
		if s.testAudioActive {
			return tea.Batch(
				func() tea.Msg {
					err := commands.VoiceLeaveTestAudioDirect()
					return roomsTestAudioResultMsg{active: false, err: err}
				},
				func() tea.Msg { return HideVoiceSettingsMsg{} },
			)
		}
		return func() tea.Msg { return HideVoiceSettingsMsg{} }
	case "tab":
		s.section = (s.section + 1) % vsSectionCount
	case "up", "ctrl+p":
		if s.section == vsSectionInput && s.inputCursor > 0 {
			s.inputCursor--
		} else if s.section == vsSectionOutput && s.outputCursor > 0 {
			s.outputCursor--
		} else if s.section == vsSectionAdvanced && s.advancedCursor > 0 {
			s.advancedCursor--
		} else if s.section == vsSectionTransformations && s.transformCursor > 0 {
			s.transformCursor--
		}
	case "down", "ctrl+n":
		if s.section == vsSectionInput && s.inputCursor < len(s.inputDevices)-1 {
			s.inputCursor++
		} else if s.section == vsSectionOutput {
			if s.outputCursor < 3 { // master(0) + chatSFX(1) + playback(2) + voiceChat(3)
				s.outputCursor++
			}
		} else if s.section == vsSectionAdvanced {
			if s.advancedCursor < 2 { // jitter(0) + noise-out(1) + noise-in(2)
				s.advancedCursor++
			}
		} else if s.section == vsSectionTransformations {
			maxCursor := 1 // pitch + vibrato toggle
			if voice.VibratoEnabled() {
				maxCursor = 3 // + freq + range
			}
			if s.transformCursor < maxCursor {
				s.transformCursor++
			}
		}
	case "left":
		switch s.section {
		case vsSectionOutput:
			switch s.outputCursor {
			case 0:
				voice.SetMasterVolume(voice.MasterVolume() - 5)
				sounds.PlayPreview(float64(voice.MasterVolume()) / 100.0)
			case 1:
				if voice.ChatSFXOverride() >= 0 {
					voice.SetChatSFXOverride(voice.ChatSFXOverride() - 5)
					sounds.PlayPreview(float64(voice.ChatSFXOverride()) / 100.0)
				}
			case 2:
				if voice.PlaybackOverride() >= 0 {
					voice.SetPlaybackOverride(voice.PlaybackOverride() - 5)
					sounds.PlayPreview(float64(voice.PlaybackOverride()) / 100.0)
				}
			case 3:
				if voice.VoiceChatOverride() >= 0 {
					voice.SetVoiceChatOverride(voice.VoiceChatOverride() - 5)
					sounds.PlayPreview(float64(voice.VoiceChatOverride()) / 100.0)
				}
			}
		case vsSectionTransformations:
			switch s.transformCursor {
			case 0:
				voice.SetPitchPos(voice.PitchPos() - 1)
			case 2:
				voice.SetVibratoFreq(voice.VibratoFreq() - 1)
			case 3:
				voice.SetVibratoRange(voice.VibratoRange() - 1)
			}
		}
	case "right":
		switch s.section {
		case vsSectionOutput:
			switch s.outputCursor {
			case 0:
				voice.SetMasterVolume(voice.MasterVolume() + 5)
				sounds.PlayPreview(float64(voice.MasterVolume()) / 100.0)
			case 1:
				if voice.ChatSFXOverride() >= 0 {
					voice.SetChatSFXOverride(voice.ChatSFXOverride() + 5)
					sounds.PlayPreview(float64(voice.ChatSFXOverride()) / 100.0)
				}
			case 2:
				if voice.PlaybackOverride() >= 0 {
					voice.SetPlaybackOverride(voice.PlaybackOverride() + 5)
					sounds.PlayPreview(float64(voice.PlaybackOverride()) / 100.0)
				}
			case 3:
				if voice.VoiceChatOverride() >= 0 {
					voice.SetVoiceChatOverride(voice.VoiceChatOverride() + 5)
					sounds.PlayPreview(float64(voice.VoiceChatOverride()) / 100.0)
				}
			}
		case vsSectionTransformations:
			switch s.transformCursor {
			case 0:
				voice.SetPitchPos(voice.PitchPos() + 1)
			case 2:
				voice.SetVibratoFreq(voice.VibratoFreq() + 1)
			case 3:
				voice.SetVibratoRange(voice.VibratoRange() + 1)
			}
		}
	case "enter", " ":
		return s.activate()
	}
	return nil
}

func (s *VoiceSettingsScreen) activate() tea.Cmd {
	switch s.section {
	case vsSectionInput:
		if len(s.inputDevices) == 0 {
			return nil
		}
		dev := s.inputDevices[s.inputCursor]
		voice.SetInputDevice(dev.ID)
		label := dev.Label
		return func() tea.Msg {
			clientlog.Info("audio input set to: %s", label)
			return nil
		}
	case vsSectionTransformations:
		switch s.transformCursor {
		case 0:
			voice.SetPitchEnabled(!voice.PitchEnabled())
		case 1:
			if !voice.VibratoEnabled() {
				s.transformCursor = 1 // keep cursor on vibrato when enabling
			} else if s.transformCursor > 1 {
				s.transformCursor = 1 // clamp cursor when disabling
			}
			voice.SetVibratoEnabled(!voice.VibratoEnabled())
		}
		return nil
	case vsSectionOutput:
		switch s.outputCursor {
		case 1: // Chat SFX toggle
			if voice.ChatSFXOverride() >= 0 {
				voice.SetChatSFXOverride(-1) // revert to master
			} else {
				voice.SetChatSFXOverride(voice.MasterVolume()) // start at master value
			}
		case 2: // AudioClip Playback toggle
			if voice.PlaybackOverride() >= 0 {
				voice.SetPlaybackOverride(-1) // revert to master
			} else {
				voice.SetPlaybackOverride(voice.MasterVolume()) // start at master value
			}
		case 3: // Voice Chat toggle
			if voice.VoiceChatOverride() >= 0 {
				voice.SetVoiceChatOverride(-1) // revert to master
			} else {
				voice.SetVoiceChatOverride(voice.MasterVolume()) // start at master value
			}
		}
		return nil
	case vsSectionAdvanced:
		switch s.advancedCursor {
		case 0:
			voice.SetJitterBufferEnabled(!voice.JitterBufferEnabled())
		case 1:
			voice.SetDenoiseOutboundEnabled(!voice.DenoiseOutboundEnabled())
		case 2:
			voice.SetDenoiseInboundEnabled(!voice.DenoiseInboundEnabled())
		}
		return nil
	case vsSectionTest:
		if s.voiceActive || s.testAudioConnecting {
			return nil // mutually exclusive with voice chat, or already connecting
		}
		if !s.testAudioActive {
			s.testAudioConnecting = true
			s.spinnerFrame = 0
			return tea.Batch(
				s.spinnerTick(),
				func() tea.Msg {
					clientlog.Info("starting test audio")
					err := commands.VoiceTestAudioDirect()
					if err != nil {
						clientlog.Error("test audio start failed: %v", err)
					}
					return roomsTestAudioResultMsg{active: err == nil, err: err}
				},
			)
		}
		return func() tea.Msg {
			clientlog.Info("stopping test audio")
			err := commands.VoiceLeaveTestAudioDirect()
			if err != nil {
				clientlog.Error("test audio stop failed: %v", err)
			}
			return roomsTestAudioResultMsg{active: false, err: err}
		}
	}
	return nil
}

// buildSlider renders a 13-char slider track with a filled dot at position pos
// out of maxPos steps. Used for all slider widgets in Audio Settings.
func buildSlider(pos, maxPos int) string {
	const sliderLen = 13
	if maxPos <= 0 {
		maxPos = 1
	}
	scaled := pos * (sliderLen - 1) / maxPos
	if scaled < 0 {
		scaled = 0
	}
	if scaled >= sliderLen {
		scaled = sliderLen - 1
	}
	track := make([]rune, sliderLen)
	for i := range track {
		if i == scaled {
			track[i] = '●'
		} else {
			track[i] = '─'
		}
	}
	return "[" + string(track) + "]"
}

func (s *VoiceSettingsScreen) sectionHeader(label string, active bool, innerW int) string {
	var text string
	if active {
		text = styles.VoiceSettingsActiveSectionStyle.Render(label)
	} else {
		text = styles.DimText.Render(label)
	}
	used := lipgloss.Width(text) + 1
	fill := innerW - used
	if fill < 0 {
		fill = 0
	}
	sep := styles.DimText.Render(" " + strings.Repeat("─", fill))
	return text + sep
}

func (s *VoiceSettingsScreen) Render() string {
	// innerW / innerH are the lipgloss inner content dimensions for Width()/Height().
	// AppStyle Padding(1,2) removes 4 horizontal, 2 vertical.
	// RoundedBorder removes 2 more horizontal (left+right), 2 vertical.
	// Padding(0,1) removes 2 more horizontal (left+right).
	innerW := s.width - 8  // 4 (AppStyle) + 2 (border) + 2 (panel padding)
	innerH := s.height - 6 // 2 (AppStyle) + 2 (border) + 1 (hint line) + 1 (extra breathing room)
	if innerW < 20 {
		innerW = 20
	}
	if innerH < 10 {
		innerH = 10
	}

	var lines []string

	// ── Title row ──────────────────────────────────────────────────────────
	title := styles.VoiceSettingsBadge.Render("AUDIO SETTINGS")
	escHint := styles.DimText.Render("ESC to exit")
	gap := innerW - lipgloss.Width(title) - lipgloss.Width(escHint)
	if gap < 1 {
		gap = 1
	}
	lines = append(lines, title+strings.Repeat(" ", gap)+escHint, "")

	// ── Audio Input ─────────────────────────────────────────────────────────
	lines = append(lines, s.sectionHeader("AUDIO INPUT", s.section == vsSectionInput, innerW))
	if len(s.inputDevices) == 0 {
		lines = append(lines, styles.DimText.Render("  no devices found"))
	} else {
		sel := voice.SelectedInputDevice()
		for i, dev := range s.inputDevices {
			cursor := "  "
			if s.section == vsSectionInput && s.inputCursor == i {
				cursor = styles.CursorStyle.Render("▶ ")
			}
			label := dev.Label
			if dev.ID == sel {
				label += " " + styles.DimText.Render("✓")
			}
			var item string
			if s.section == vsSectionInput && s.inputCursor == i {
				item = cursor + styles.RoomBtnActiveStyle.Render(label)
			} else {
				item = cursor + styles.ItemStyle.Render(label)
			}
			lines = append(lines, item)
		}
	}
	lines = append(lines, "  "+styles.DimText.Render("Don't see your device? Plug it in before starting the app."))
	lines = append(lines, "")

	// ── Audio Output ────────────────────────────────────────────────────────
	lines = append(lines, s.sectionHeader("AUDIO OUTPUT", s.section == vsSectionOutput, innerW))
	{
		inSection := s.section == vsSectionOutput
		cur := func(idx int) string {
			if inSection && s.outputCursor == idx {
				return styles.CursorStyle.Render("▶ ")
			}
			return "  "
		}
		focused := func(idx int) bool { return inSection && s.outputCursor == idx }

		// Labels are padded to 19 chars so sliders align across all rows.
		const (
			lblMaster    = "Master Output      " // 19 chars
			lblChatSFX   = "Chat SFX           " // 19 chars
			lblPlayback  = "AudioClip Playback " // 19 chars
			lblVoiceChat = "Voice Chat         " // 19 chars
		)

		// Master Output Volume (always a slider)
		masterLabel := fmt.Sprintf("%s%s  %d%%", lblMaster, buildSlider(voice.MasterVolume()/5, 20), voice.MasterVolume())
		if focused(0) {
			lines = append(lines, cur(0)+styles.RoomBtnActiveStyle.Render(masterLabel))
			lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  •  applies to all audio"))
		} else {
			lines = append(lines, cur(0)+styles.ItemStyle.Render(masterLabel))
		}

		// Chat SFX (toggle + inline slider when override active)
		chatSFX := voice.ChatSFXOverride()
		if chatSFX < 0 {
			chatSFXLabel := lblChatSFX + styles.DimText.Render("(master)")
			if focused(1) {
				lines = append(lines, cur(1)+styles.RoomBtnActiveStyle.Render(chatSFXLabel))
				lines = append(lines, "  "+styles.DimText.Render("enter to set own volume"))
			} else {
				lines = append(lines, cur(1)+styles.ItemStyle.Render(chatSFXLabel))
			}
		} else {
			chatSFXLabel := fmt.Sprintf("%s%s  %d%%", lblChatSFX, buildSlider(chatSFX/5, 20), chatSFX)
			if focused(1) {
				lines = append(lines, cur(1)+styles.RoomBtnActiveStyle.Render(chatSFXLabel))
				lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  •  enter to use master"))
			} else {
				lines = append(lines, cur(1)+styles.ItemStyle.Render(chatSFXLabel))
			}
		}

		// AudioClip Playback (toggle + inline slider when override active)
		pbOverride := voice.PlaybackOverride()
		if pbOverride < 0 {
			pbLabel := lblPlayback + styles.DimText.Render("(master)")
			if focused(2) {
				lines = append(lines, cur(2)+styles.RoomBtnActiveStyle.Render(pbLabel))
				lines = append(lines, "  "+styles.DimText.Render("enter to set own volume"))
			} else {
				lines = append(lines, cur(2)+styles.ItemStyle.Render(pbLabel))
			}
		} else {
			pbLabel := fmt.Sprintf("%s%s  %d%%", lblPlayback, buildSlider(pbOverride/5, 20), pbOverride)
			if focused(2) {
				lines = append(lines, cur(2)+styles.RoomBtnActiveStyle.Render(pbLabel))
				lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  •  enter to use master"))
			} else {
				lines = append(lines, cur(2)+styles.ItemStyle.Render(pbLabel))
			}
		}

		// Voice Chat (toggle + inline slider when override active)
		vcOverride := voice.VoiceChatOverride()
		if vcOverride < 0 {
			vcLabel := lblVoiceChat + styles.DimText.Render("(master)")
			if focused(3) {
				lines = append(lines, cur(3)+styles.RoomBtnActiveStyle.Render(vcLabel))
				lines = append(lines, "  "+styles.DimText.Render("enter to set own volume"))
			} else {
				lines = append(lines, cur(3)+styles.ItemStyle.Render(vcLabel))
			}
		} else {
			vcLabel := fmt.Sprintf("%s%s  %d%%", lblVoiceChat, buildSlider(vcOverride/5, 20), vcOverride)
			if focused(3) {
				lines = append(lines, cur(3)+styles.RoomBtnActiveStyle.Render(vcLabel))
				lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  •  enter to use master"))
			} else {
				lines = append(lines, cur(3)+styles.ItemStyle.Render(vcLabel))
			}
		}
	}
	lines = append(lines, "")

	// ── Test Audio ──────────────────────────────────────────────────────────
	lines = append(lines, s.sectionHeader("TEST VOICE", s.section == vsSectionTest, innerW))
	cursor := "  "
	if s.section == vsSectionTest {
		cursor = styles.CursorStyle.Render("▶ ")
	}
	var testItem string
	if s.voiceActive {
		testItem = "  " + styles.DimText.Render("Start Test Audio (unavailable while in voice chat)")
	} else if s.testAudioConnecting {
		spinner := styles.DimText.Render(spinnerFrames[s.spinnerFrame])
		testItem = "  " + spinner + " " + styles.DimText.Render("Connecting...")
	} else if s.section == vsSectionTest {
		if s.testAudioActive {
			testItem = cursor + styles.VoiceLeaveFocusedStyle.Render("Stop Test Audio")
		} else {
			testItem = cursor + styles.RoomBtnActiveStyle.Render("Start Test Audio")
		}
	} else {
		if s.testAudioActive {
			testItem = cursor + styles.VoiceLeaveStyle.Render("Stop Test Audio")
		} else {
			testItem = cursor + styles.ItemStyle.Render("Start Test Audio")
		}
	}
	lines = append(lines, testItem)
	if s.testAudioActive {
		stats := fmt.Sprintf("  %.0f pkt/s  %.1f KB/s  net %.0f ms  lat %.0f ms  enc %.2f ms  dec %.2f ms",
			s.sendPacketsPerSec, s.sendKBPerSec, s.networkRTTMs, s.pipelineLatMs, s.avgEncodeMs, s.avgDecodeMs)
		lines = append(lines, styles.DimText.Render(stats))
	}

	// ── Transformations ─────────────────────────────────────────────────────
	lines = append(lines, "")
	lines = append(lines, s.sectionHeader("TRANSFORMATIONS", s.section == vsSectionTransformations, innerW))
	{
		inSection := s.section == vsSectionTransformations

		cur := func(idx int) string {
			if inSection && s.transformCursor == idx {
				return styles.CursorStyle.Render("▶ ")
			}
			return "  "
		}
		focused := func(idx int) bool { return inSection && s.transformCursor == idx }

		// ── Pitch ──
		if !voice.PitchEnabled() {
			lines = append(lines, cur(0)+styles.ItemStyle.Render("Pitch "+styles.DimText.Render("off")))
			if focused(0) {
				lines = append(lines, "  "+styles.DimText.Render("enter to enable"))
			}
		} else {
			pos := voice.PitchPos()
			semitones := pos - 12
			sign := ""
			if semitones > 0 {
				sign = "+"
			}
			label := fmt.Sprintf("Pitch  %s  %s%d st", buildSlider(pos, 24), sign, semitones)
			if focused(0) {
				lines = append(lines, cur(0)+styles.RoomBtnActiveStyle.Render(label))
				lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  •  enter to disable"))
			} else {
				lines = append(lines, cur(0)+styles.ItemStyle.Render(label))
			}
		}

		// ── Vibrato ──
		vibratoState := styles.DimText.Render("off")
		if voice.VibratoEnabled() {
			vibratoState = styles.DimText.Render("✓ on")
		}
		vibratoLabel := "Vibrato " + vibratoState
		if focused(1) {
			lines = append(lines, cur(1)+styles.RoomBtnActiveStyle.Render(vibratoLabel))
			lines = append(lines, "  "+styles.DimText.Render("enter to toggle  •  ↑/↓ to edit settings"))
		} else {
			lines = append(lines, cur(1)+styles.ItemStyle.Render(vibratoLabel))
		}

		if voice.VibratoEnabled() {
			// Freq
			freqLabel := fmt.Sprintf("Freq  %s  %d Hz", buildSlider(voice.VibratoFreq()-1, 7), voice.VibratoFreq())
			if focused(2) {
				lines = append(lines, cur(2)+styles.RoomBtnActiveStyle.Render(freqLabel))
				lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  (1–8 Hz)"))
			} else {
				lines = append(lines, cur(2)+styles.ItemStyle.Render(freqLabel))
			}
			// Range
			rangeSt := float64(voice.VibratoRange()) * 0.5
			rangeLabel := fmt.Sprintf("Range %s  ±%.1f st", buildSlider(voice.VibratoRange()-1, 5), rangeSt)
			if focused(3) {
				lines = append(lines, cur(3)+styles.RoomBtnActiveStyle.Render(rangeLabel))
				lines = append(lines, "  "+styles.DimText.Render("◀/▶ adjust  (±0.5–3.0 st)"))
			} else {
				lines = append(lines, cur(3)+styles.ItemStyle.Render(rangeLabel))
			}
		}
	}

	// ── Advanced ────────────────────────────────────────────────────────────
	lines = append(lines, "")
	lines = append(lines, s.sectionHeader("ADVANCED", s.section == vsSectionAdvanced, innerW))
	advItems := []struct {
		label string
		on    bool
		hint  string
	}{
		{"Jitter Buffer", voice.JitterBufferEnabled(), "Smooths packet reordering at the cost of added delay. Takes effect on next connect."},
		{"Noise Removal (outbound)", voice.DenoiseOutboundEnabled(), "RNNoise on your microphone. Disable if it distorts your voice."},
		{"Noise Removal (inbound)", voice.DenoiseInboundEnabled(), "RNNoise on received audio. Reduces noise from other participants."},
	}
	for i, item := range advItems {
		cur := "  "
		if s.section == vsSectionAdvanced && s.advancedCursor == i {
			cur = styles.CursorStyle.Render("▶ ")
		}
		state := styles.DimText.Render("off")
		if item.on {
			state = styles.DimText.Render("✓ on")
		}
		label := item.label + " " + state
		if s.section == vsSectionAdvanced && s.advancedCursor == i {
			lines = append(lines, cur+styles.RoomBtnActiveStyle.Render(label))
			lines = append(lines, "  "+styles.DimText.Render(item.hint))
		} else {
			lines = append(lines, cur+styles.ItemStyle.Render(label))
		}
	}

	content := strings.Join(lines, "\n")

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.PanelFocusedBorderColor).
		Padding(0, 1).
		Width(innerW).
		Height(innerH).
		Render(content)

	hint := styles.HelpStyle.Render("  tab  next section  •  ↑/↓  navigate  •  enter  select/toggle  •  esc  close")
	return panel + "\n" + hint
}

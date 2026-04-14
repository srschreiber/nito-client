// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/qeesung/image2ascii/convert"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/history"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

const maxCmdHistory = 20

// chatOpDef describes a chat op for autocomplete.
type chatOpDef struct {
	name    string // e.g. ".image"
	argHint string // e.g. "<filename>"
}

var chatOps = []chatOpDef{
	{name: ".createroom", argHint: "--name <room name>"},
	{name: ".invite", argHint: "--user <username>"},
	{name: ".playalias", argHint: "--alias <name> --url <url>"},
	{name: ".delplayalias", argHint: "<name>"},
	{name: ".image", argHint: "<filename> [-h <height>]"},
	{name: ".jump", argHint: "<line>"},
	{name: ".play", argHint: "--mp3-or-m3u-or-alias <url|alias> [--track <0-2>]"},
	{name: ".stoptrack", argHint: "<track 0-2>"},
	{name: ".stopall", argHint: ""},
}

// completeChatOp returns the first op whose name starts with prefix, or nil.
func completeChatOp(prefix string) *chatOpDef {
	if !strings.HasPrefix(prefix, ".") || strings.Contains(prefix, " ") {
		return nil
	}
	for i := range chatOps {
		if strings.HasPrefix(chatOps[i].name, prefix) {
			return &chatOps[i]
		}
	}
	return nil
}

type cursorBlinkMsg struct{ gen int }

// chatCreateRoomResultMsg carries the result of a .createroom operation.
type chatCreateRoomResultMsg struct {
	text string
	err  error
}

// chatDelAliasResultMsg carries the result of a .delplayalias operation.
type chatDelAliasResultMsg struct {
	text string
	err  error
}

type execResultMsg struct {
	entries []historyEntry
	signal  commands.Signal
}

const (
	placeholderCmd     = "Type a command... (/chat  /dms  /notifications  /invites  /logs  or wcid for all commands)"
	placeholderChat    = "Chat  (/ quickselect)"
	placeholderDM      = "DMs — use .dm <user> or ↑/↓ to select a conversation"
	placeholderInvites = "Invites (read-only — use ↑/↓ enter to accept)"
)

type CommandComponent struct {
	Placeholder    string
	focused        bool
	chatMode       bool
	dmMode         bool       // true when a DM conversation is active
	dmTarget       string     // username of the current DM target
	activeTab      HistoryTab // mirrors the currently visible tab
	passwordMode   bool
	textFieldValue string
	cursorPos      int
	cursorVisible  bool
	blinkGen       int
	width          int
	// command history (up/down navigation)
	cmdHistory []string
	historyIdx int    // -1 = not navigating
	draftText  string // saved input before navigating history
}

func NewCommandComponent(width int) *CommandComponent {
	return &CommandComponent{
		Placeholder:   placeholderChat,
		chatMode:      true,
		cursorVisible: true,
		historyIdx:    -1,
		width:         width,
		cmdHistory:    history.Load(),
	}
}

func (c *CommandComponent) SetWidth(width int) {
	c.width = width
}

func (l *CommandComponent) newBlinkCmd() tea.Cmd {
	gen := l.blinkGen
	return tea.Tick(time.Millisecond*530, func(time.Time) tea.Msg {
		return cursorBlinkMsg{gen: gen}
	})
}

// resetCursor makes the cursor immediately visible and restarts the blink cycle.
func (l *CommandComponent) resetCursor() tea.Cmd {
	l.cursorVisible = true
	l.blinkGen++
	return l.newBlinkCmd()
}

func (l *CommandComponent) Init() tea.Cmd {
	return l.newBlinkCmd()
}

func (l *CommandComponent) SetFocused(focused bool) {
	l.focused = focused
}

func (l *CommandComponent) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case chatDelAliasResultMsg:
		if msg.err != nil {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".delplayalias: " + msg.err.Error(), isResponse: true},
				}, Tab: TabChat}
			}
		}
		text := msg.text
		return tea.Batch(
			func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: text, isResponse: true},
				}, Tab: TabChat}
			},
			func() tea.Msg { return RefreshTrackStateMsg{} },
		)
	case chatCreateRoomResultMsg:
		if msg.err != nil {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".createroom: " + msg.err.Error(), isResponse: true},
				}, Tab: TabChat}
			}
		}
		text := msg.text
		return tea.Batch(
			func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: text, isResponse: true},
				}, Tab: TabChat}
			},
			func() tea.Msg { return types.RoomsFetchMsg{} },
		)
	case ModeChangedMsg:
		// Sync chatMode when tabs are switched externally (left/right arrows).
		l.chatMode = msg.ChatMode
		l.dmMode = false
		l.dmTarget = ""
		if msg.ChatMode {
			l.activeTab = TabChat
			l.Placeholder = placeholderChat
		} else {
			// ModeChangedMsg with ChatMode=false means we're on a non-chat, non-DM tab.
			if l.activeTab == TabDM || l.activeTab == TabChat {
				l.activeTab = TabCmd
			}
			l.Placeholder = placeholderCmd
		}
		return nil
	case SwitchTabMsg:
		l.activeTab = msg.Tab
		l.dmMode = false
		l.dmTarget = ""
		switch msg.Tab {
		case TabChat:
			l.chatMode = true
			l.Placeholder = placeholderChat
		case TabInvites:
			l.chatMode = false
			l.Placeholder = placeholderInvites
		case TabDM:
			l.chatMode = false
			l.Placeholder = placeholderDM
		default:
			l.chatMode = false
			l.Placeholder = placeholderCmd
		}
		return nil
	case DMTargetChangedMsg:
		if msg.User == "" {
			l.dmMode = false
			l.dmTarget = ""
			// Don't touch chatMode or Placeholder here — ModeChangedMsg controls them.
		} else {
			l.dmMode = true
			l.dmTarget = msg.User
			l.chatMode = false
			l.Placeholder = "Message " + msg.User + "..."
		}
		return nil
	case PreFillCommandMsg:
		l.textFieldValue = msg.Text
		if msg.CursorPos < 0 || msg.CursorPos > len(msg.Text) {
			l.cursorPos = len(msg.Text)
		} else {
			l.cursorPos = msg.CursorPos
		}
		// Chat ops (pre-filled text starting with ".") must run in chat mode.
		if strings.HasPrefix(msg.Text, ".") {
			l.chatMode = true
			l.Placeholder = placeholderChat
		}
		return nil
	case types.RoomSelectedMsg:
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: "switched to room " + msg.RoomID + " — press / to quickselect", isResponse: true},
			}}
		}
	case execResultMsg:
		userID := ""
		if s := connection.CurrentSession(); s != nil {
			userID = s.UserID
		}
		connMsg := types.ConnectionStatusMsg{
			Connected: connection.IsConnected(),
			BrokerURL: connection.BrokerURL(),
			UserID:    userID,
		}
		emitConn := func() tea.Msg { return connMsg }
		entries := msg.entries
		switch msg.signal {
		case commands.SignalClear:
			return tea.Batch(func() tea.Msg { return ClearHistoryMsg{} }, emitConn)
		case commands.SignalExit:
			_ = history.Save(l.cmdHistory)
			return tea.Quit
		case commands.SignalRefreshRooms:
			return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn, func() tea.Msg { return types.RoomsFetchMsg{} })
		case commands.SignalNeedPassword, commands.SignalNeedRegisterPassword:
			pendingPasswordSignal = msg.signal
			l.passwordMode = true
			l.Placeholder = "Password:"
			entries = append(entries, historyEntry{text: "Password:", isResponse: true})
			return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn)
		case commands.SignalConnected:
			return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn, func() tea.Msg { return types.ConnectedMsg{} })
		case commands.SignalRoomSelected:
			roomID := connection.GetSessionRoomID()
			if roomID == nil {
				break
			}
			id := *roomID
			entries = append(entries, historyEntry{text: "press / to quickselect", isResponse: true})
			return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn, func() tea.Msg { return types.RoomSelectedMsg{RoomID: id} })
		case commands.SignalVoiceLeave:
			return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn, func() tea.Msg { return StopAudioMsg{} })
		case commands.SignalStartDM:
			user := commands.DMUser
			return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn, func() tea.Msg { return StartDMMsg{User: user} })
		case commands.SignalJump:
			line := commands.JumpLine
			return func() tea.Msg { return JumpScrollMsg{Line: line} }
		}
		return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn)

	case cursorBlinkMsg:
		if msg.gen != l.blinkGen {
			return nil // stale tick from before last reset
		}
		l.cursorVisible = !l.cursorVisible
		return l.newBlinkCmd()

	case tea.PasteMsg:
		text := msg.Content
		if text != "" {
			runes := []rune(l.textFieldValue)
			l.textFieldValue = string(runes[:l.cursorPos]) + text + string(runes[l.cursorPos:])
			l.cursorPos += len([]rune(text))
			return l.resetCursor()
		}

	case tea.KeyPressMsg:
		// Every key interaction resets the cursor to visible.
		blink := l.resetCursor()

		var keyCmd tea.Cmd
		switch msg.String() {
		case "left", "ctrl+b":
			if l.cursorPos > 0 {
				l.cursorPos--
			}
		case "right", "ctrl+f":
			if l.cursorPos < len([]rune(l.textFieldValue)) {
				l.cursorPos++
			}
		case "ctrl+a":
			l.cursorPos = 0
		case "ctrl+e":
			l.cursorPos = len([]rune(l.textFieldValue))
		case "ctrl+k":
			l.textFieldValue = string([]rune(l.textFieldValue)[:l.cursorPos])
		case "ctrl+d":
			runes := []rune(l.textFieldValue)
			if l.cursorPos < len(runes) {
				l.textFieldValue = string(append(runes[:l.cursorPos], runes[l.cursorPos+1:]...))
			}
		case "up":
			if len(l.cmdHistory) > 0 {
				if l.historyIdx == -1 {
					l.draftText = l.textFieldValue
					l.historyIdx = len(l.cmdHistory) - 1
				} else if l.historyIdx > 0 {
					l.historyIdx--
				}
				l.textFieldValue = l.cmdHistory[l.historyIdx]
				l.cursorPos = len([]rune(l.textFieldValue))
			}
		case "down":
			if l.historyIdx != -1 {
				if l.historyIdx == len(l.cmdHistory)-1 {
					l.historyIdx = -1
					l.textFieldValue = l.draftText
				} else {
					l.historyIdx++
					l.textFieldValue = l.cmdHistory[l.historyIdx]
				}
				l.cursorPos = len([]rune(l.textFieldValue))
			}
		case "tab":
			if tpl := l.completionTemplate(); tpl != "" {
				l.textFieldValue = tpl
				l.cursorPos = len([]rune(tpl))
			}
		case "enter":
			if l.textFieldValue != "" {
				keyCmd = l.handleEnter()
			}
		case "backspace":
			if l.cursorPos > 0 {
				runes := []rune(l.textFieldValue)
				runes = append(runes[:l.cursorPos-1], runes[l.cursorPos:]...)
				l.textFieldValue = string(runes)
				l.cursorPos--
			}
		default:
			text := msg.Key().Text
			if text != "" {
				runes := []rune(l.textFieldValue)
				l.textFieldValue = string(runes[:l.cursorPos]) + text + string(runes[l.cursorPos:])
				l.cursorPos += len([]rune(text))
			}
		}

		if keyCmd != nil {
			return tea.Batch(keyCmd, blink)
		}
		return blink
	}
	return nil
}

// pendingPasswordSignal tracks which flow the current password prompt belongs to.
var pendingPasswordSignal commands.Signal

func (l *CommandComponent) handlePasswordSubmit(password string) tea.Cmd {
	var out string
	var signal commands.Signal
	var err error

	switch pendingPasswordSignal {
	case commands.SignalNeedRegisterPassword:
		out, signal, err = commands.CompleteRegister(context.Background(), password)
	default:
		out, signal, err = commands.CompleteLogin(context.Background(), password)
	}
	pendingPasswordSignal = commands.SignalNone

	entries := []historyEntry{{text: "> [password]"}}
	if err != nil {
		entries = append(entries, historyEntry{text: err.Error(), isResponse: true})
	} else if out != "" {
		entries = append(entries, historyEntry{text: out, isResponse: true})
	}

	userID := ""
	if s := connection.CurrentSession(); s != nil {
		userID = s.UserID
	}
	connMsg := types.ConnectionStatusMsg{
		Connected: connection.IsConnected(),
		BrokerURL: connection.BrokerURL(),
		UserID:    userID,
	}
	emitConn := func() tea.Msg { return connMsg }

	if signal == commands.SignalConnected {
		return tea.Batch(
			func() tea.Msg { return AppendHistoryMsg{Entries: entries} },
			emitConn,
			func() tea.Msg { return types.ConnectedMsg{} },
		)
	}
	return tea.Batch(func() tea.Msg { return AppendHistoryMsg{Entries: entries} }, emitConn)
}

// filterFlags removes tokens that start with "--" from a slice. They are
// cosmetic labels shown in pre-filled commands to hint at what each value is.
func filterFlags(tokens []string) []string {
	out := tokens[:0:len(tokens)]
	for _, t := range tokens {
		if !strings.HasPrefix(t, "--") {
			out = append(out, t)
		}
	}
	return out
}

func (l *CommandComponent) handleChatOp(input string) tea.Cmd {
	parts := strings.SplitN(input, " ", 2)
	op := parts[0]
	switch op {
	case ".createroom":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".createroom: usage: .createroom --name <room name>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		tokens := filterFlags(strings.Fields(parts[1]))
		if len(tokens) == 0 {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".createroom: room name required", isResponse: true},
				}, Tab: TabChat}
			}
		}
		roomName := strings.Join(tokens, " ")
		return func() tea.Msg {
			text, err := commands.CreateRoomDirect(roomName)
			return chatCreateRoomResultMsg{text: text, err: err}
		}
	case ".invite":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".invite: usage: .invite --user <username>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		tokens := filterFlags(strings.Fields(parts[1]))
		if len(tokens) == 0 {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".invite: username required", isResponse: true},
				}, Tab: TabChat}
			}
		}
		inviteUser := tokens[0]
		return func() tea.Msg {
			text, err := commands.InviteUserDirect(inviteUser)
			if err != nil {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".invite: " + err.Error(), isResponse: true},
				}, Tab: TabChat}
			}
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: text, isResponse: true},
			}, Tab: TabChat}
		}
	case ".jump":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".jump: usage: .jump <line>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".jump: line must be an integer", isResponse: true},
				}, Tab: TabChat}
			}
		}
		return func() tea.Msg { return JumpScrollMsg{Line: n} }
	case ".stopall":
		return func() tea.Msg { return StopAudioMsg{Track: -1} }
	case ".stoptrack":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".stoptrack: usage: .stoptrack <track 0-2>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || n < 0 || n > 2 {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".stoptrack: track must be 0, 1, or 2", isResponse: true},
				}, Tab: TabChat}
			}
		}
		return func() tea.Msg { return StopAudioMsg{Track: n} }
	case ".playalias":
		if len(parts) < 2 {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".playalias: usage: .playalias --alias <name> --url <url>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		tokens := filterFlags(strings.Fields(parts[1]))
		if len(tokens) < 2 {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".playalias: usage: .playalias --alias <name> --url <url>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		name := tokens[0]
		aliasURL := tokens[1]
		if err := voice.SaveAudioAlias(name, aliasURL); err != nil {
			errMsg := err.Error()
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".playalias: " + errMsg, isResponse: true},
				}, Tab: TabChat}
			}
		}
		savedName := name
		savedURL := aliasURL
		return tea.Batch(
			func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: fmt.Sprintf("saved alias %q → %s", savedName, savedURL), isResponse: true},
				}, Tab: TabChat}
			},
			func() tea.Msg { return RefreshTrackStateMsg{} },
		)
	case ".delplayalias":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".delplayalias: usage: .delplayalias <name>", isResponse: true},
				}, Tab: TabChat}
			}
		}
		aliasName := strings.TrimSpace(filterFlags(strings.Fields(parts[1]))[0])
		if aliasName == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".delplayalias: alias name required", isResponse: true},
				}, Tab: TabChat}
			}
		}
		return func() tea.Msg {
			err := voice.DeleteAudioAlias(aliasName)
			if err != nil {
				return chatDelAliasResultMsg{err: err}
			}
			return chatDelAliasResultMsg{text: fmt.Sprintf("deleted alias %q", aliasName)}
		}
	case ".play":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".play: usage: .play --mp3-or-m3u-or-alias <url|alias> [--track <0-2>]", isResponse: true},
				}, Tab: TabChat}
			}
		}
		// Strip cosmetic --flag tokens; positional: first token = url/alias, last numeric token = track.
		playArgs := filterFlags(strings.Fields(parts[1]))
		if len(playArgs) == 0 {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".play: url or alias required", isResponse: true},
				}, Tab: TabChat}
			}
		}
		arg := strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1 // strip control characters (e.g. \r from Windows clipboard)
			}
			return r
		}, playArgs[0])
		track := -1 // -1 = auto
		if len(playArgs) >= 2 {
			n, err := strconv.Atoi(playArgs[len(playArgs)-1])
			if err == nil && n >= 0 && n <= 2 {
				track = n
			}
		}
		url := arg
		if resolved, ok := voice.LookupAudioAlias(arg); ok {
			url = resolved
		}
		playURL := url
		playTrack := track
		return func() tea.Msg { return PlayAudioMsg{URL: playURL, Track: playTrack} }
	case ".image":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".image: usage: .image <filename> [-h <height>]", isResponse: true},
				}, Tab: TabChat}
			}
		}
		// Parse: .image <filename> [-h|-height <n>]
		tokens := strings.Fields(parts[1])
		filename := ""
		height := 0
		for i := 0; i < len(tokens); i++ {
			if (tokens[i] == "-h" || tokens[i] == "--height") && i+1 < len(tokens) {
				n, err := strconv.Atoi(tokens[i+1])
				if err == nil {
					height = n
				}
				i++
			} else if filename == "" {
				filename = tokens[i]
			}
		}
		if filename == "" {
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".image: usage: .image <filename> [-h <height>]", isResponse: true},
				}, Tab: TabChat}
			}
		}
		return imageOp(filename, height)
	default:
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: "unknown op: " + op, isResponse: true},
			}, Tab: TabChat}
		}
	}
}

// imageOp loads an image from ~/.nito/images/<filename>, converts it to ASCII
// art scaled to fit within maxW×maxH (preserving aspect ratio), and appends it
// to the conversation history. height overrides the default max height (capped
// at 100); pass 0 to use the default.
func imageOp(filename string, height int) tea.Cmd {
	home, err := os.UserHomeDir()
	if err != nil {
		errMsg := err.Error()
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: ".image: " + errMsg, isResponse: true},
			}, Tab: TabChat}
		}
	}

	// Resolve image path: try ~/.nito/images/<basename> first, then cwd/.nito/images/<basename>.
	base := filepath.Base(filename)
	nitoPath := filepath.Join(home, ".nito", "images", base)
	var imagePath string
	if _, statErr := os.Stat(nitoPath); statErr == nil {
		imagePath = nitoPath
	} else {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			errMsg := cwdErr.Error()
			return func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: ".image: " + errMsg, isResponse: true},
				}, Tab: TabChat}
			}
		}
		imagePath = filepath.Join(cwd, ".nito", "images", base)
	}

	// Read image dimensions to compute aspect-ratio-preserving fit within 50x50.
	f, err := os.Open(imagePath)
	if err != nil {
		errMsg := err.Error()
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: ".image: " + errMsg, isResponse: true},
			}, Tab: TabChat}
		}
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		errMsg := err.Error()
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: ".image: decode config: " + errMsg, isResponse: true},
			}, Tab: TabChat}
		}
	}

	const defaultMax = 100
	maxW := defaultMax
	maxH := defaultMax
	if height > 0 {
		if height > 100 {
			height = 100
		}
		maxH = height
	}
	w, h := fitAspect(cfg.Width, cfg.Height, maxW, maxH)

	converter := convert.NewImageConverter()
	options := convert.DefaultOptions
	options.FixedWidth = w
	options.FixedHeight = h
	options.Colored = true

	ascii := converter.ImageFile2ASCIIString(imagePath, &options)
	if strings.TrimSpace(ascii) == "" {
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: ".image: failed to convert '" + filename + "' (check it exists in ~/.nito/images/)", isResponse: true},
			}, Tab: TabChat}
		}
	}

	entries := []historyEntry{
		{text: "> .image " + filename},
		{text: ascii, isRaw: true},
	}
	if err := commands.SendRoomImage(ascii); err != nil {
		entries = append(entries, historyEntry{text: err.Error(), isResponse: true})
	}
	return func() tea.Msg { return AppendHistoryMsg{Entries: entries, Tab: TabChat} }
}

// fitAspect returns width and height that fit within maxW×maxH while preserving
// the original aspect ratio.
func fitAspect(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return maxW, maxH
	}
	scaleW := float64(maxW) / float64(srcW)
	scaleH := float64(maxH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	w := int(float64(srcW) * scale)
	h := int(float64(srcH) * scale)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func (l *CommandComponent) handleEnter() tea.Cmd {
	input := l.textFieldValue
	l.textFieldValue = ""
	l.cursorPos = 0

	if l.passwordMode {
		l.passwordMode = false
		l.Placeholder = placeholderCmd
		return l.handlePasswordSubmit(input)
	}

	l.cmdHistory = append(l.cmdHistory, input)
	if len(l.cmdHistory) > maxCmdHistory {
		l.cmdHistory = l.cmdHistory[1:]
	}
	l.historyIdx = -1

	// Mode-switch commands are intercepted before anything else.
	if input == "/chat" {
		l.chatMode = true
		l.dmMode = false
		l.dmTarget = ""
		l.Placeholder = placeholderChat
		return tea.Batch(
			func() tea.Msg {
				return AppendHistoryMsg{Entries: []historyEntry{
					{text: "> " + input},
					{text: "Switched to chat mode. Type messages and press enter to send.", isResponse: true},
				}}
			},
			func() tea.Msg { return SwitchTabMsg{Tab: TabChat} },
			func() tea.Msg { return ModeChangedMsg{ChatMode: true} },
		)
	}

	// Tab-switching slash commands.
	tabSwitchCmds := map[string]HistoryTab{
		"/dms":           TabDM,
		"/notifications": TabNotifications,
		"/invites":       TabInvites,
		"/logs":          TabLogs,
	}
	if tab, ok := tabSwitchCmds[input]; ok {
		l.activeTab = tab
		l.chatMode = false
		l.dmMode = false
		l.dmTarget = ""
		switch tab {
		case TabInvites:
			l.Placeholder = placeholderInvites
		case TabDM:
			l.Placeholder = placeholderDM
		default:
			l.Placeholder = placeholderCmd
		}
		return func() tea.Msg { return SwitchTabMsg{Tab: tab} }
	}

	// .dm <user> works in any mode to open a DM conversation.
	if strings.HasPrefix(input, ".dm ") {
		user := strings.TrimSpace(strings.TrimPrefix(input, ".dm "))
		if user != "" {
			return tea.Batch(
				func() tea.Msg {
					return AppendHistoryMsg{Entries: []historyEntry{{text: "> " + input}}}
				},
				func() tea.Msg { return StartDMMsg{User: user} },
			)
		}
	}

	// On the DM tab with no target selected, block input from reaching ExecCommand.
	if l.activeTab == TabDM && !l.dmMode {
		return func() tea.Msg {
			return AppendHistoryMsg{Entries: []historyEntry{
				{text: "No DM conversation selected. Use .dm <user> or ↑/↓ to pick one.", isResponse: true},
			}}
		}
	}

	// In DM mode, plain input is sent as a direct message to the target.
	if l.dmMode && l.dmTarget != "" {
		target := l.dmTarget
		entries := []historyEntry{{text: fmt.Sprintf("[you → %s]: %s", target, input)}}
		if err := commands.SendDirectMessage(target, input); err != nil {
			entries = append(entries, historyEntry{text: err.Error(), isResponse: true})
		}
		return func() tea.Msg { return AppendDMHistoryMsg{User: target, Entries: entries} }
	}

	// In chat mode, plain input is sent as a room message.
	if l.chatMode {
		if strings.HasPrefix(input, ".") {
			return l.handleChatOp(input)
		}
		entries := []historyEntry{{text: "[you]: " + input}}
		if err := commands.SendRoomMessage(input); err != nil {
			entries = append(entries, historyEntry{text: err.Error(), isResponse: true})
		}
		return func() tea.Msg { return AppendHistoryMsg{Entries: entries, Tab: TabChat} }
	}

	// Run ExecCommand off the UI goroutine so blocking commands (e.g. voice-join
	// ICE gathering) don't freeze the event loop.
	return func() tea.Msg {
		output, signal, err := commands.ExecCommand(input)
		entries := []historyEntry{{text: "> " + input}}
		if err != nil {
			entries = append(entries, historyEntry{text: err.Error(), isResponse: true})
		} else if output != "" {
			entries = append(entries, historyEntry{text: output, isResponse: true})
		}
		return execResultMsg{entries: entries, signal: signal}
	}
}

// ghostSuffix returns the grey inline suggestion text to display after the
// current input, or "" if there is nothing to suggest. In command mode it
// completes command names; in chat mode it completes .op names.
func (l *CommandComponent) ghostSuffix() string {
	if l.passwordMode {
		return ""
	}
	text := l.textFieldValue
	if len([]rune(text)) < 1 || strings.Contains(text, " ") {
		return ""
	}
	if l.cursorPos != len([]rune(text)) {
		return ""
	}
	// .dm works in any mode.
	const dmHint = ".dm <user>"
	if strings.HasPrefix(dmHint, text) && text != dmHint {
		return dmHint[len(text):]
	}
	// Tab-switching slash commands autocomplete.
	for _, cmd := range []string{"/chat", "/dms", "/notifications", "/invites", "/logs"} {
		if strings.HasPrefix(cmd, text) && text != cmd {
			return cmd[len(text):]
		}
	}
	if l.chatMode {
		op := completeChatOp(text)
		if op == nil {
			return ""
		}
		return op.name[len(text):] + " " + op.argHint
	}
	if strings.HasPrefix(text, "/") {
		return ""
	}
	def := commands.CompletePrefix(text)
	if def == nil {
		return ""
	}
	suffix := def.Name[len(text):]
	for _, arg := range def.Args {
		flag := arg.Long
		if flag == "" {
			flag = arg.Short
		}
		suffix += fmt.Sprintf(" --%s <%s>", flag, flag)
	}
	return suffix
}

// HasSuggestion reports whether there is an active inline autocomplete suggestion.
func (l *CommandComponent) HasSuggestion() bool {
	return l.ghostSuffix() != ""
}

// completionTemplate returns the full completed string to use on Tab, or "".
func (l *CommandComponent) completionTemplate() string {
	text := l.textFieldValue
	if len([]rune(text)) < 1 || strings.Contains(text, " ") {
		return ""
	}
	// .dm works in any mode.
	const dmHint = ".dm <user>"
	if strings.HasPrefix(dmHint, text) && text != dmHint {
		return dmHint
	}
	// Tab-switching slash commands.
	for _, cmd := range []string{"/chat", "/dms", "/notifications", "/invites", "/logs"} {
		if strings.HasPrefix(cmd, text) && text != cmd {
			return cmd
		}
	}
	if l.chatMode {
		op := completeChatOp(text)
		if op == nil {
			return ""
		}
		return op.name + " " + op.argHint
	}
	if strings.HasPrefix(text, "/") {
		return ""
	}
	def := commands.CompletePrefix(text)
	if def == nil {
		return ""
	}
	result := def.Name
	for _, arg := range def.Args {
		flag := arg.Long
		if flag == "" {
			flag = arg.Short
		}
		result += fmt.Sprintf(" --%s <%s>", flag, flag)
	}
	return result
}

func (l *CommandComponent) Render() string {
	prompt := styles.PromptStyle.Render("> ")
	runes := []rune(l.textFieldValue)
	if l.passwordMode {
		runes = []rune(strings.Repeat("*", len(runes)))
	}
	ghost := l.ghostSuffix()

	var render string
	if l.focused && l.cursorVisible {
		before := string(runes[:l.cursorPos])
		if l.cursorPos < len(runes) {
			underCursor := styles.CursorHighlightStyle.Render(string(runes[l.cursorPos]))
			render = prompt + before + underCursor + string(runes[l.cursorPos+1:])
		} else if ghost != "" {
			// Use the first ghost char as the cursor highlight so the ghost
			// text never shifts when the cursor blinks.
			gr := []rune(ghost)
			render = prompt + before + styles.CursorHighlightStyle.Render(string(gr[0])) + styles.DimText.Render(string(gr[1:]))
		} else if len(runes) > 0 {
			render = prompt + before + styles.CursorHighlightStyle.Render(" ")
		} else {
			// Empty input — highlight the first char of the placeholder in place
			pr := []rune(l.Placeholder)
			render = prompt + styles.CursorHighlightStyle.Render(string(pr[0])) + styles.DimText.Render(string(pr[1:]))
		}
	} else {
		if len(runes) > 0 {
			render = prompt + string(runes) + styles.DimText.Render(ghost)
		} else {
			render = prompt + styles.DimText.Render(l.Placeholder)
		}
	}

	style := styles.UnfocusedBorderStyle
	if l.focused {
		style = styles.FocusedBorderStyle.
			Background(styles.ComponentFocusedBg).
			BorderBackground(styles.ComponentFocusedBg)
	}
	if l.width > 0 {
		style = style.Width(l.width + 3) // +3: left border (1) + padding(0,1) (2)
	}
	return style.Render(render)
}

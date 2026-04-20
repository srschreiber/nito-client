// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"time"

	"github.com/srschreiber/nito-client/engine/commands"
	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
	"github.com/srschreiber/nito-client/engine/sounds"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/ui/clientlog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ── Message model ─────────────────────────────────────────────────────────────

type msgKind int

const (
	msgChat msgKind = iota
	msgSystem
	msgSelf
	msgDM
)

type chatMessage struct {
	kind      msgKind
	timestamp string // "15:04"
	date      string // "2006-01-02" — used for day separator injection
	from      string
	body      string
}

// isEmojiRune returns true for runes that are part of an emoji grapheme — the
// emoji codepoints themselves plus the modifier runes (ZWJ, variation selectors,
// skin tones) that join them into a single visual glyph.
func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FFFF: // emoticons, symbols & pictographs, transport, etc.
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols, dingbats
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // misc symbols & arrows
		return true
	case r >= 0x2300 && r <= 0x23FF: // misc technical (⏰ etc.)
		return true
	case r == 0x200D, r == 0xFE0F, r == 0xFE0E: // ZWJ, variation selectors
		return true
	}
	return false
}

// upsizeEmojiSegments enlarges emoji in RichText messages. Because Fyne's
// RichText baseline-aligns mixed-size segments — a bigger emoji would visually
// float above the text line — we only upsize when the *entire* message body is
// emojis and whitespace. That mirrors Discord/Slack behaviour and avoids the
// baseline misalignment caused by inline size mixing.
func upsizeEmojiSegments(rt *widget.RichText) {
	if !isEmojiOnlyMessage(rt) {
		return
	}
	for _, seg := range rt.Segments {
		if ts, ok := seg.(*widget.TextSegment); ok {
			ts.Style.SizeName = sizeNameChatEmoji
		}
	}
}

// isEmojiOnlyText returns true if s consists solely of emoji runes and
// whitespace. Used by the message renderer to route to the enlarged-emoji
// RichText path instead of the selectable-text widget.
func isEmojiOnlyText(s string) bool {
	hasEmoji := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		if !isEmojiRune(r) {
			return false
		}
		hasEmoji = true
	}
	return hasEmoji
}

// isEmojiOnlyMessage returns true if every text segment in rt contains only
// emoji runes and whitespace (so we can safely enlarge the whole message).
func isEmojiOnlyMessage(rt *widget.RichText) bool {
	hasEmoji := false
	for _, seg := range rt.Segments {
		ts, ok := seg.(*widget.TextSegment)
		if !ok {
			return false
		}
		for _, r := range ts.Text {
			if r == ' ' || r == '\t' || r == '\n' {
				continue
			}
			if !isEmojiRune(r) {
				return false
			}
			hasEmoji = true
		}
	}
	return hasEmoji
}

type emojiRun struct {
	text    string
	isEmoji bool
}

// splitByEmoji segments s into runs of consecutive emoji-grapheme runes and
// non-emoji text. A ZWJ / variation selector / skin-tone modifier keeps the
// current run going; they never start a new emoji run on their own.
func splitByEmoji(s string) []emojiRun {
	if s == "" {
		return nil
	}
	var runs []emojiRun
	var cur emojiRun
	flush := func() {
		if cur.text != "" {
			runs = append(runs, cur)
		}
		cur = emojiRun{}
	}
	for _, r := range s {
		emoji := isEmojiRune(r)
		// ZWJ / VS are only "emoji-ish" when already inside an emoji run; otherwise
		// they're inert punctuation that should stay in text runs.
		if (r == 0x200D || r == 0xFE0F || r == 0xFE0E) && !cur.isEmoji {
			emoji = false
		}
		if cur.text == "" {
			cur = emojiRun{isEmoji: emoji}
		} else if emoji != cur.isEmoji {
			flush()
			cur = emojiRun{isEmoji: emoji}
		}
		cur.text += string(r)
	}
	flush()
	return runs
}

// verifyStatusIcon returns a hover-tooltipped glyph reflecting a peer's trust
// state. ✓ green = cryptographically verified via the mutual-handshake flow;
// ⚠ amber = TOFU-pinned or unknown. Used in the member list and DM header.
func verifyStatusIcon(username string) fyne.CanvasObject {
	rec, ok := keys.LoadPeerPublicKey(username)
	var glyph string
	var col color.Color
	var tooltip string
	switch {
	case !ok:
		glyph = "⚠ "
		col = colAmber
		tooltip = "No key on record for " + username + " — will TOFU-pin on first use."
	case rec.Verified:
		glyph = "✓ "
		col = colGreen
		tooltip = username + "'s key is cryptographically verified."
	default:
		glyph = "⚠ "
		col = colAmber
		tooltip = username + "'s key is pinned (TOFU) but NOT verified. When they're online, right-click their name → Verify to confirm their identity."
	}
	// Icon bumped to 16pt (~33% bigger than surrounding text) with a subtle
	// theme-tinted backdrop so the hover zone is visually discoverable.
	// container.NewCenter keeps the glyph vertically centered inside the
	// 26×26 backdrop regardless of its native ascender height.
	icon := txt(glyph, col, 16, true, true)
	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 4
	bg.SetMinSize(fyne.NewSize(26, 26))
	return NewIconWithTooltip(container.NewStack(bg, container.NewCenter(icon)), tooltip)
}

// roomTrustIcon returns a small tooltip-icon reflecting the local trust level
// of a room's current-key rotator. Returns nil only when the rotator is
// ourselves (no useful signal to show). Legacy rooms with no manifest
// surface a distinct "unknown" glyph so users don't mistake silence for
// safety — a room that predates manifest support has NO trust information.
func roomTrustIcon(r apitypes.RoomEntry) fyne.CanvasObject {
	// Self-rotated rooms need no icon.
	if r.RotatorUsername != "" && r.RotatorUsername == connection.GetSessionUsername() {
		return nil
	}
	// Legacy (pre-manifest) room: surface the lack of trust info rather than
	// falling silent. colAlert because "no signed manifest at all" is
	// strictly weaker than TOFU and users should know.
	if r.RotatorUsername == "" {
		return NewIconWithTooltip(
			txt("!", colAlert, 12, true, true),
			"No signed key manifest for this room (legacy room). The rotator's identity cannot be verified.",
		)
	}

	var rec keys.TrustedKey
	var ok bool
	if r.RotatorDeviceID != "" {
		rec, ok = keys.LoadPeerPublicKeyByDevice(r.RotatorUsername, r.RotatorDeviceID)
	} else {
		rec, ok = keys.LoadPeerPublicKey(r.RotatorUsername)
	}
	var glyph string
	var col color.Color
	var tooltip string
	switch {
	case !ok:
		glyph = "?"
		col = liveDim
		tooltip = "Room key signed by " + r.RotatorUsername + " — no key on record yet; will TOFU-pin on join."
	case rec.Verified:
		glyph = "✓"
		col = colGreen
		tooltip = "Room key signed by verified user " + r.RotatorUsername + "."
	default:
		glyph = "⚠"
		col = colAmber
		tooltip = "Room key signed by " + r.RotatorUsername + ", who is TOFU-pinned but NOT verified. Right-click their name in members to verify."
	}
	icon := txt(glyph, col, 12, true, true)
	return NewIconWithTooltip(icon, tooltip)
}

// refreshRotatorBanner repopulates the banner under the room header with the
// current room-key rotator and their trust state. Shows nothing (zero-height
// banner) when there's no active room or no manifest rotator recorded.
// Must be called on the Fyne thread.
func (cp *ChatPanel) refreshRotatorBanner() {
	if cp.rotatorBanner == nil {
		return
	}
	rotator := connection.GetSessionRoomRotator()
	// Empty rotator means the server didn't return a manifest for this
	// room's current key — legacy room. We don't want to stay silent here;
	// users should know there's no signature backing this room.
	if rotator == "" {
		if cp.currentRoomID == "" {
			cp.rotatorBanner.Objects = nil
		} else {
			label := txt("⚠ This room has no signed key manifest — the rotator's identity cannot be verified (legacy room).", colAlert, 11, false, true)
			cp.rotatorBanner.Objects = []fyne.CanvasObject{container.NewPadded(label), hline()}
		}
		cp.rotatorBanner.Refresh()
		return
	}
	me := connection.GetSessionUsername()
	rotatorDevice := connection.GetSessionRoomRotatorDevice()
	var label *canvas.Text
	switch {
	case rotator == me:
		label = txt("🔑 Room key last rotated by you", liveDim, 11, false, true)
	default:
		// Check the specific (rotator, device) trust record since that's
		// what the manifest signature binds to.
		var rec keys.TrustedKey
		var ok bool
		if rotatorDevice != "" {
			rec, ok = keys.LoadPeerPublicKeyByDevice(rotator, rotatorDevice)
		} else {
			rec, ok = keys.LoadPeerPublicKey(rotator)
		}
		verified := ok && rec.Verified
		if verified {
			label = txt("🔑 Room key signed by verified user "+rotator+".", colGreen, 11, false, true)
		} else {
			// Be explicit: traffic IS encrypted, but with a key whose device
			// identity hasn't been verified out-of-band. colAlert is the
			// profile-independent warning color.
			label = txt("⚠ Traffic is encrypted by a key from an UNTRUSTED device of "+rotator+". Right-click "+rotator+" → Verify to confirm their identity.", colAlert, 11, false, true)
		}
	}
	cp.rotatorBanner.Objects = []fyne.CanvasObject{
		container.NewPadded(label),
		hline(),
	}
	cp.rotatorBanner.Refresh()
}

// refreshTrustIndicators rebuilds the member list, room list, rotator banner,
// and any open DM view so every ✓/⚠/? icon re-evaluates against the current
// on-disk trust state. Called after a verify flow finishes successfully — in
// particular, the room-list icon should flip for any room whose rotator is
// the user we just verified.
func (cp *ChatPanel) refreshTrustIndicators(username string) {
	cp.refreshRotatorBanner()
	// Re-fetch rooms so SetRooms re-renders per-row trust icons.
	go func() {
		if rooms, err := connection.ListRooms(); err == nil {
			fyne.Do(func() { cp.SetRooms(rooms) })
		}
	}()
	if cp.currentRoomID != "" {
		go func(roomID string) {
			members, err := connection.ListRoomMembers(roomID)
			if err != nil {
				return
			}
			fyne.Do(func() { cp.SetMembers(members) })
		}(cp.currentRoomID)
	}
	// If a DM view is already open for this user, swap it for a freshly
	// built one — that's the only way to update the header icon since the
	// header is built once in buildDMView.
	if oldView, ok := cp.dmViews[username]; ok {
		msgBox := cp.dmMsgBoxes[username]
		wasVisible := oldView.Visible()
		newView := cp.buildDMView(msgBox, username)
		if !wasVisible {
			newView.Hide()
		}
		for i, o := range cp.chatRight.Objects {
			if o == oldView {
				cp.chatRight.Objects[i] = newView
				break
			}
		}
		cp.dmViews[username] = newView
		cp.chatRight.Refresh()
	}
}

// ── Message renderer ──────────────────────────────────────────────────────────

func renderMessage(m chatMessage) fyne.CanvasObject {
	var row fyne.CanvasObject
	msgBody := func(text string) fyne.CanvasObject {
		// Emoji-only messages render at the enlarged emoji size but still go
		// through SelectableRichText so each emoji can be clicked, selected,
		// and Cmd+C-copied like any other character.
		if isEmojiOnlyText(text) {
			theme := fyne.CurrentApp().Settings().Theme()
			size := theme.Size(sizeNameChatEmoji)
			return NewSelectableRichTextWithSize(text, size)
		}
		// GIF-only messages (just a URL, nothing else) hide the URL text — the
		// embed below conveys the content.
		if isGifOnlyText(text) {
			return container.NewWithoutLayout()
		}
		// Everything else (including ``` code blocks) goes through the
		// selectable rich-text widget which handles block rendering inline.
		return NewSelectableRichText(text)
	}
	msgRow := func(ts, from string, fromCol color.Color, text string) fyne.CanvasObject {
		// Meta (timestamp + colored username) sits on the left of the row
		// with the body flowing on the same line. canvas.Text so the color
		// survives — its natural width becomes the row's horizontal minimum
		// (~120 px), which still lets the HSplit divider move; the body
		// (SelectableRichText) reports 0 min-width and wraps freely.
		meta := container.NewHBox(
			txt(ts+" ", liveDim, 12, false, true),
			txt(from+"  ", fromCol, 13, true, true),
		)
		return container.NewPadded(container.NewBorder(nil, nil, meta, nil, msgBody(text)))
	}

	switch m.kind {
	case msgSystem:
		// truncLabel instead of raw canvas.Text so the row can shrink with
		// the split divider instead of forcing its full text width as a min.
		lbl := truncLabel("  "+m.body, colMuted, false)
		return container.NewPadded(lbl)
	case msgSelf:
		row = msgRow(m.timestamp, "you", colCyan, m.body)
	case msgDM:
		row = msgRow(m.timestamp, m.from, colLavender, m.body)
	default:
		row = msgRow(m.timestamp, m.from, colLavender, m.body)
	}

	// Append embeds for YouTube, audio, and GIF URLs.
	ytURLs := findYouTubeURLs(m.body)
	audioURLs := findAudioURLs(m.body)
	gifURLs := findGifURLs(m.body)
	if len(ytURLs) == 0 && len(audioURLs) == 0 && len(gifURLs) == 0 {
		return row
	}
	parts := []fyne.CanvasObject{row}
	for _, u := range ytURLs {
		if embed := buildYouTubeEmbed(u); embed != nil {
			parts = append(parts, embed)
		}
	}
	for _, u := range audioURLs {
		if embed := buildAudioEmbed(u); embed != nil {
			parts = append(parts, embed)
		}
	}
	for _, u := range gifURLs {
		if embed := buildGifEmbed(u); embed != nil {
			parts = append(parts, embed)
		}
	}
	return container.NewVBox(parts...)
}

// renderDaySep renders a muted centred date divider for the given "2006-01-02" date.
func renderDaySep(date string) fyne.CanvasObject {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	var label string
	switch date {
	case today:
		label = "— today —"
	case yesterday:
		label = "— yesterday —"
	default:
		label = "— " + t.Format("Monday, January 2") + " —"
	}
	return container.NewPadded(truncLabel("  "+label, colMuted, false))
}

// ── ChatPanel ─────────────────────────────────────────────────────────────────

type ChatPanel struct {
	widget.BaseWidget
	w fyne.Window

	// Room area
	content       *fyne.Container
	msgBox        *fyne.Container   // VBox of room messages
	msgScroll     *container.Scroll // VScroll wrapping msgBox
	roomHeader    *canvas.Text      // "# roomname"
	rotatorBanner *fyne.Container   // shows who last rotated the room key + trust state
	roomListBox   *fyne.Container   // VBox of room sidebar rows
	memberBox     *fyne.Container   // VBox of member rows
	chatRight     *fyne.Container   // Stack: roomArea + DM views
	inputWrapper  *fyne.Container   // Bottom input bar placeholder

	// Current room
	messages        []chatMessage
	currentRoomID   string
	currentRoomName string
	roomArea        fyne.CanvasObject

	// DM views
	dmViews    map[string]fyne.CanvasObject
	dmMsgBoxes map[string]*fyne.Container
	dmMessages map[string][]chatMessage

	OnDMOpen func(username string)

	voiceBtn  *nitoBtn
	voiceTick int
}

func NewChatPanel(w fyne.Window) *ChatPanel {
	cp := &ChatPanel{
		w:          w,
		dmViews:    make(map[string]fyne.CanvasObject),
		dmMsgBoxes: make(map[string]*fyne.Container),
		dmMessages: make(map[string][]chatMessage),
	}

	// ── Room chat area ────────────────────────────────────────────────────────
	cp.roomHeader = sectionBadge("# —")
	cp.msgBox = container.NewVBox()
	cp.msgScroll = container.NewVScroll(cp.msgBox)
	cp.rotatorBanner = container.NewVBox() // populated by refreshRotatorBanner
	header := container.NewVBox(
		container.NewHBox(cp.roomHeader),
		vspace(2), hline(),
		cp.rotatorBanner,
	)
	cp.roomArea = container.NewBorder(header, nil, nil, nil, cp.msgScroll)

	// ── Stack: room area + (DM views added dynamically) ──────────────────────
	cp.chatRight = container.NewStack(cp.roomArea)

	// ── Input bar placeholder (filled by SetInputBar) ─────────────────────────
	cp.inputWrapper = container.NewVBox()

	// ── Room sidebar ──────────────────────────────────────────────────────────
	cp.roomListBox = container.NewVBox(dimTxt("loading rooms…"))
	cp.memberBox = container.NewVBox()

	sidebar := cp.buildSidebar()

	chatArea := container.NewBorder(nil, cp.inputWrapper, nil, nil, cp.chatRight)
	split := container.NewHSplit(sidebar, chatArea)
	split.SetOffset(0.18)

	cp.content = container.NewStack(split)
	cp.ExtendBaseWidget(cp)

	// Theme changes cause Fyne to Refresh the whole tree; the scroll container
	// can lose its offset during that cascade. Snapshot offset before the
	// theme-triggered refresh kicks in, restore it on the next tick.
	registerThemeListener(func() {
		offset := cp.msgScroll.Offset
		fyne.Do(func() {
			cp.msgScroll.Offset = offset
			cp.msgScroll.Refresh()
		})
	})

	// When a GIF embed finishes downloading, the message row grows. If the
	// user was at the bottom, scroll to the new bottom so they see the GIF.
	setGifLoadedHook(func() {
		if isAtBottom(cp.msgScroll) {
			cp.msgScroll.ScrollToBottom()
		}
	})

	return cp
}

// isAtBottom returns true if the scroll container is scrolled to the end
// (within 4 px of bottom). Used to avoid yanking the user's view when
// async content (like GIF embeds) arrives while they're reading history.
func isAtBottom(s *container.Scroll) bool {
	const slack float32 = 4
	return s.Offset.Y+s.Size().Height >= s.Content.Size().Height-slack
}

// buildSidebar constructs the sidebar once; room/member rows are updated via
// SetRooms / SetMembers. Buttons wire to real backend operations.
func (cp *ChatPanel) buildSidebar() fyne.CanvasObject {
	w := cp.w

	// ── Create room popup ─────────────────────────────────────────────────────
	createBtn := newBtn("+ Create", nil)
	createBtn.Importance = widget.LowImportance
	createBtn.OnTapped = func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("room name")
		confirmBtn := newBtn("Create", nil)
		cancelBtn := newBtn("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		confirmBtn.OnTapped = func() {
			name := strings.TrimSpace(nameEntry.Text)
			if pop != nil {
				pop.Hide()
			}
			if name == "" {
				return
			}
			go func() {
				_, err := commands.CreateRoomDirect(name)
				fyne.Do(func() {
					if err != nil {
						clientlog.Error("create room failed: " + err.Error())
						showToast(w, "create room: "+err.Error(), toastError)
						return
					}
					clientlog.Info("created room: " + name)
					showToast(w, "room created: "+name, toastSuccess)
					go func() {
						rooms, err := connection.ListRooms()
						if err == nil {
							fyne.Do(func() { cp.SetRooms(rooms) })
						}
					}()
				})
			}()
		}
		cancelBtn.OnTapped = func() {
			if pop != nil {
				pop.Hide()
			}
		}
		body := container.NewVBox(
			monoTxt("room name", liveDimMid), nameEntry, vspace(6),
			container.NewHBox(confirmBtn, cancelBtn),
		)
		pop = showNitoPopup("CREATE ROOM", body, w)
		w.Canvas().Focus(nameEntry)
	}

	// ── Invite popup ──────────────────────────────────────────────────────────
	inviteBtn := newBtn("+ Invite", nil)
	inviteBtn.Importance = widget.LowImportance
	inviteBtn.OnTapped = func() {
		if cp.currentRoomID == "" {
			showToast(w, "select a room first", toastWarn)
			return
		}
		userEntry := widget.NewEntry()
		userEntry.SetPlaceHolder("username")
		nextBtn := newBtn("Next", nil)
		cancelBtn := newBtn("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		nextBtn.OnTapped = func() {
			username := strings.TrimSpace(userEntry.Text)
			if username == "" {
				return
			}
			if pop != nil {
				pop.Hide()
			}
			go func() {
				rec, ok := keys.LoadPeerPublicKey(username)
				if ok && rec.Verified {
					// Cryptographically verified key — invite immediately.
					_, err := commands.InviteUserWithKey(username, rec.PublicKey)
					fyne.Do(func() {
						if err != nil {
							clientlog.Error("invite failed: " + err.Error())
							showToast(w, "invite: "+err.Error(), toastError)
							return
						}
						clientlog.Info("invited (verified) " + username)
						showToast(w, "invited "+username, toastSuccess)
					})
					return
				}
				var existing *keys.TrustedKey
				if ok {
					existing = &rec
				}
				fyne.Do(func() { showTrustPopup(username, w, existing) })
			}()
		}
		cancelBtn.OnTapped = func() {
			if pop != nil {
				pop.Hide()
			}
		}
		body := container.NewVBox(
			monoTxt("username", liveDimMid), userEntry, vspace(6),
			container.NewHBox(nextBtn, cancelBtn),
		)
		pop = showNitoPopup("INVITE TO ROOM", body, w)
		w.Canvas().Focus(userEntry)
	}

	// ── Voice join/leave button (state updated by TickVoice every 50 ms) ──────
	cp.voiceBtn = newBtn("Join Voice", func() {
		if sounds.IsActive() {
			go func() {
				var err error
				if sounds.ActiveRoomID() == sounds.SelfRoomID {
					err = commands.VoiceLeaveTestAudioDirect()
				} else {
					err = commands.VoiceLeaveDirect()
				}
				if err != nil {
					fyne.Do(func() { showToast(w, "sounds: "+err.Error(), toastError) })
				} else {
					clientlog.Info("left sounds chat")
				}
			}()
		} else if !sounds.IsConnecting() {
			go func() {
				if err := commands.VoiceJoinDirect(); err != nil {
					fyne.Do(func() { showToast(w, "sounds: "+err.Error(), toastError) })
				} else {
					clientlog.Info("joined sounds chat")
				}
			}()
		}
	})
	cp.voiceBtn.Importance = widget.LowImportance

	settingsBtn := newBtn("⚙ Voice Settings", func() { showVoiceSettingsPopup(w) })
	settingsBtn.Importance = widget.LowImportance
	footer := container.NewVBox(hline(), cp.voiceBtn, settingsBtn, vspace(4))

	// ── Room list section (dynamic) ───────────────────────────────────────────
	roomSection := container.NewVBox(
		txt("my rooms", liveDimMid, 11, false, true),
		cp.roomListBox,
		vspace(4),
		createBtn,
		inviteBtn,
	)

	// ── Member list section (dynamic) ─────────────────────────────────────────
	memberSection := container.NewVBox(
		vspace(8), hline(), vspace(4),
		cp.memberBox,
	)

	scrollBody := container.NewVScroll(container.NewVBox(roomSection, memberSection))
	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 4
	return container.NewStack(bg, container.NewPadded(
		container.NewBorder(nil, footer, nil, nil, scrollBody),
	))
}

// ── Dynamic update methods ────────────────────────────────────────────────────

// SetRooms rebuilds the room list rows. Must be called on the Fyne thread.
func (cp *ChatPanel) SetRooms(rooms []apitypes.RoomEntry) {
	var rows []fyne.CanvasObject

	var owned, joined []apitypes.RoomEntry
	for _, r := range rooms {
		if r.IsOwner {
			owned = append(owned, r)
		} else {
			joined = append(joined, r)
		}
	}

	// leftFor packs the room marker + trust icon (if any) into a single left
	// cell for container.NewBorder.
	leftFor := func(bullet *canvas.Text, r apitypes.RoomEntry) fyne.CanvasObject {
		if trust := roomTrustIcon(r); trust != nil {
			return container.NewHBox(bullet, trust)
		}
		return bullet
	}

	for _, r := range owned {
		r := r
		name := truncLabel(r.Name, colText, true)
		left := leftFor(monoTxt("◆ ", liveAccent), r)
		row := container.NewPadded(container.NewBorder(nil, nil, left, nil, name))
		rows = append(rows, NewHoverRow(row, func() { cp.selectRoom(r.ID, r.Name) }))
	}

	if len(joined) > 0 {
		rows = append(rows, vspace(4), txt("joined rooms", liveDimMid, 11, false, true))
		for _, r := range joined {
			r := r
			name := truncLabel(r.Name, colText, false)
			left := leftFor(monoTxt("• ", colText), r)
			row := container.NewPadded(container.NewBorder(nil, nil, left, nil, name))
			rows = append(rows, NewHoverRow(row, func() { cp.selectRoom(r.ID, r.Name) }))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, dimTxt("no rooms yet"))
	}

	cp.roomListBox.Objects = rows
	cp.roomListBox.Refresh()
}

// SetMembers rebuilds the member rows. Must be called on the Fyne thread.
func (cp *ChatPanel) SetMembers(members []apitypes.RoomMemberEntry) {
	myID := connection.GetSessionUsername()
	var rows []fyne.CanvasObject
	for _, m := range members {
		m := m
		var dot *canvas.Text
		if m.Online {
			dot = txt("● ", colGreen, 12, false, true)
		} else {
			dot = txt("○ ", liveDim, 12, false, true)
		}
		var nameCol color.Color = colText
		if !m.Online {
			nameCol = liveDim
		}
		nameLabel := truncLabel(m.Username, nameCol, false)
		// Self has no verification state to show. When the verify icon is
		// present it's taller than the online-dot, so wrap the dot in
		// NewCenter to keep them vertically aligned inside the HBox.
		var left fyne.CanvasObject = dot
		if m.Username != myID {
			left = container.NewHBox(container.NewCenter(dot), verifyStatusIcon(m.Username))
		}
		// Border's center gets the full row height (driven by `left`), and
		// widget.Label vertically-centers its text inside that — so the
		// username stays on the same visual baseline as the icon.
		row := container.NewPadded(container.NewBorder(nil, nil, left, nil, nameLabel))
		if m.Online && m.Username != myID {
			username := m.Username
			hr := NewHoverRow(row, func() { cp.openDM(username) })
			hr.OnSecondaryTap = func(pos fyne.Position) {
				menuItems := []*fyne.MenuItem{
					fyne.NewMenuItem("Message", func() { cp.openDM(username) }),
				}
				// Only offer Verify if the user isn't already cryptographically
				// verified — running the flow again would be pointless.
				if rec, ok := keys.LoadPeerPublicKey(username); !ok || !rec.Verified {
					menuItems = append(menuItems, fyne.NewMenuItem("Verify", func() {
						showVerifyPopup(username, cp.w, func(keyPEM string) {
							existing, _ := keys.LoadPeerPublicKey(username)
							_ = keys.SavePeerPublicKey(username, keys.TrustedKey{
								PublicKey: keyPEM,
								Verified:  true,
								Method:    keys.TrustMethodVerified,
								AddedAt:   existing.AddedAt,
							})
							fyne.Do(func() {
								showToast(cp.w, username+" verified", toastSuccess)
								cp.refreshTrustIndicators(username)
							})
						})
					}))
				}
				menu := fyne.NewMenu("", menuItems...)
				widget.ShowPopUpMenuAtPosition(menu, cp.w.Canvas(), pos)
			}
			rows = append(rows, hr)
		} else {
			rows = append(rows, row)
		}
	}
	cp.memberBox.Objects = rows
	cp.memberBox.Refresh()
}

// selectRoom is called when the user clicks a room row.
func (cp *ChatPanel) selectRoom(roomID, roomName string) {
	go func() {
		// Notify broker we are leaving the previous room (if any).
		var prevRoomID string
		fyne.DoAndWait(func() { prevRoomID = cp.currentRoomID })
		if prevRoomID != "" && prevRoomID != roomID {
			commands.SendRoomLeave(prevRoomID)
		}

		if err := connection.SetSessionRoom(roomID); err != nil {
			// Unverified rotator: the manifest signature checked out, but the
			// rotator's identity is only TOFU-pinned. Prompt the user to
			// continue at their own risk or verify first.
			var unverified *connection.UnverifiedRotatorError
			if errors.As(err, &unverified) {
				rotator := unverified.Rotator
				fyne.Do(func() {
					cp.promptUnverifiedRotator(roomID, roomName, rotator)
				})
				return
			}
			clientlog.Error("join room failed: " + err.Error())
			fyne.Do(func() { showToast(cp.w, "room: "+err.Error(), toastError) })
			return
		}
		// Notify broker we are now viewing this room so it routes messages here.
		commands.SendRoomEnter(roomID)
		clientlog.Info("joined room: " + roomName)

		members, _ := connection.ListRoomMembers(roomID)

		// Load historical messages
		var histMsgs []chatMessage
		resp, err := connection.GetRoomMessages(roomID, 50)
		if err == nil {
			histMsgs = decryptHistoricalMessages(resp)
		}

		joinMsg := chatMessage{
			kind:      msgSystem,
			timestamp: time.Now().Format("15:04"),
			date:      time.Now().Format("2006-01-02"),
			body:      "— joined room: " + roomName + " —",
		}
		allMsgs := append(histMsgs, joinMsg)

		fyne.Do(func() {
			cp.roomHeader.Text = "# " + roomName
			cp.roomHeader.Refresh()
			cp.currentRoomID = roomID
			cp.currentRoomName = roomName
			cp.messages = allMsgs
			cp.rebuildMsgBox()
			cp.refreshRotatorBanner()
			cp.showRoomArea()
			if members != nil {
				cp.SetMembers(members)
			}
			cp.msgScroll.ScrollToBottom()
		})
	}()
}

// promptUnverifiedRotator shows a blocking popup warning that the active
// room-key rotator is not verified (only TOFU-pinned). The user can either
// verify the rotator out-of-band first or continue at their own risk; the
// latter retries the join via SetSessionRoomTrustingUnverified.
func (cp *ChatPanel) promptUnverifiedRotator(roomID, roomName, rotator string) {
	var pop *widget.PopUp

	title := "UNVERIFIED ROOM KEY — " + strings.ToUpper(roomName)
	headline := "⚠  KEY ROTATED BY UNVERIFIED USER: " + strings.ToUpper(rotator)
	desc := "This room's encryption key was signed by " + rotator + ", whose identity you have not cryptographically verified. " +
		"The signature proves the broker didn't tamper with the key en route, but without verification you can't rule out that the broker created a fake \"" + rotator + "\" account whose key signs a substituted room key. " +
		"Either verify " + rotator + " out-of-band first (recommended) or continue at your own risk."

	continueBtn := newBtn("Continue unverified (risky)", nil)
	verifyBtn := newBtn("Start verification", nil)
	cancelBtn := newBtn("Cancel", nil)
	cancelBtn.Importance = widget.LowImportance

	continueBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
		go func() {
			if err := connection.SetSessionRoomTrustingUnverified(roomID); err != nil {
				clientlog.Error("join room (trusting unverified) failed: " + err.Error())
				fyne.Do(func() { showToast(cp.w, "room: "+err.Error(), toastError) })
				return
			}
			commands.SendRoomEnter(roomID)
			clientlog.Info("joined room %s (rotator %s unverified — user consented)", roomID, rotator)
			members, _ := connection.ListRoomMembers(roomID)
			var histMsgs []chatMessage
			if resp, err := connection.GetRoomMessages(roomID, 50); err == nil {
				histMsgs = decryptHistoricalMessages(resp)
			}
			joinMsg := chatMessage{
				kind:      msgSystem,
				timestamp: time.Now().Format("15:04"),
				date:      time.Now().Format("2006-01-02"),
				body:      "— joined room: " + roomName + " (rotator unverified) —",
			}
			allMsgs := append(histMsgs, joinMsg)
			fyne.Do(func() {
				cp.roomHeader.Text = "# " + roomName
				cp.roomHeader.Refresh()
				cp.currentRoomID = roomID
				cp.currentRoomName = roomName
				cp.messages = allMsgs
				cp.rebuildMsgBox()
				cp.refreshRotatorBanner()
				cp.showRoomArea()
				if members != nil {
					cp.SetMembers(members)
				}
				cp.msgScroll.ScrollToBottom()
			})
		}()
	}

	verifyBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
		// Kicks off the mutual-handshake flow for the rotator. On success
		// the peer key store marks them verified; a follow-up room-select
		// will pass the trust check without prompting.
		showVerifyPopup(rotator, cp.w, func(_ string) {
			showToast(cp.w, "verified "+rotator+" — try joining the room again", toastSuccess)
		})
	}

	cancelBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
	}

	body := container.NewVBox(
		alertRichText(headline), vspace(6),
		popupDescLabel(desc), vspace(10),
		verifyBtn, continueBtn, cancelBtn,
	)
	pop = showNitoPopupSized(title, body, cp.w, 0.33)
}

// AppendMessage adds a message to the current room view. Fyne thread.
func (cp *ChatPanel) AppendMessage(m chatMessage) {
	if m.date != "" {
		n := len(cp.messages)
		if n == 0 || cp.messages[n-1].date != m.date {
			if sep := renderDaySep(m.date); sep != nil {
				cp.msgBox.Objects = append(cp.msgBox.Objects, sep)
			}
		}
	}
	cp.messages = append(cp.messages, m)
	cp.msgBox.Objects = append(cp.msgBox.Objects, renderMessage(m))
	cp.msgBox.Refresh()
	cp.msgScroll.ScrollToBottom()
}

func (cp *ChatPanel) rebuildMsgBox() {
	var objs []fyne.CanvasObject
	lastDate := ""
	for _, m := range cp.messages {
		if m.date != "" && m.date != lastDate {
			if sep := renderDaySep(m.date); sep != nil {
				objs = append(objs, sep)
			}
			lastDate = m.date
		}
		objs = append(objs, renderMessage(m))
	}
	cp.msgBox.Objects = objs
	cp.msgBox.Refresh()
	cp.msgScroll.ScrollToBottom()
}

// AppendDMMessage adds or creates a DM view for fromUser. Fyne thread.
func (cp *ChatPanel) AppendDMMessage(fromUser string, m chatMessage) {
	msgs := cp.dmMessages[fromUser]
	if m.date != "" {
		n := len(msgs)
		if n == 0 || msgs[n-1].date != m.date {
			cp.dmMessages[fromUser] = append(msgs, m)
			if _, ok := cp.dmViews[fromUser]; !ok {
				cp.createDMView(fromUser)
			}
			box := cp.dmMsgBoxes[fromUser]
			if sep := renderDaySep(m.date); sep != nil {
				box.Objects = append(box.Objects, sep)
			}
			box.Objects = append(box.Objects, renderMessage(m))
			box.Refresh()
			return
		}
	}
	cp.dmMessages[fromUser] = append(msgs, m)
	if _, ok := cp.dmViews[fromUser]; !ok {
		cp.createDMView(fromUser)
	}
	box := cp.dmMsgBoxes[fromUser]
	box.Objects = append(box.Objects, renderMessage(m))
	box.Refresh()
}

func (cp *ChatPanel) createDMView(username string) {
	msgBox := container.NewVBox()
	// Populate with any already-received messages, injecting day separators.
	lastDate := ""
	for _, m := range cp.dmMessages[username] {
		if m.date != "" && m.date != lastDate {
			if sep := renderDaySep(m.date); sep != nil {
				msgBox.Objects = append(msgBox.Objects, sep)
			}
			lastDate = m.date
		}
		msgBox.Objects = append(msgBox.Objects, renderMessage(m))
	}
	cp.dmMsgBoxes[username] = msgBox

	view := cp.buildDMView(msgBox, username)
	view.Hide()
	cp.dmViews[username] = view
	cp.chatRight.Objects = append(cp.chatRight.Objects, view)
	cp.chatRight.Refresh()
}

func (cp *ChatPanel) buildDMView(msgBox *fyne.Container, username string) fyne.CanvasObject {
	backLabel := NewTappable(
		container.NewHBox(
			txt("← ", liveAccent, 13, false, true),
			txt("#"+cp.currentRoomName, liveDimMid, 12, false, true),
		),
		func() { cp.showRoomArea() },
	)
	header := container.NewVBox(
		container.NewHBox(backLabel, vspace(8), sectionBadge("@ "+username), verifyStatusIcon(username)),
		vspace(2), hline(),
	)

	var topBanner fyne.CanvasObject
	rec, ok := keys.LoadPeerPublicKey(username)
	if !ok || !rec.Verified {
		warning := monoTxt("⚠ Key not verified — messages are encrypted but identity unconfirmed. Right-click to verify.", colAmber)
		topBanner = container.NewVBox(container.NewPadded(warning), hline())
	}

	content := container.NewBorder(header, nil, nil, nil,
		container.NewVScroll(msgBox))

	if topBanner != nil {
		return container.NewBorder(topBanner, nil, nil, nil, content)
	}
	return content
}

func (cp *ChatPanel) openDM(username string) {
	if _, ok := cp.dmViews[username]; !ok {
		cp.createDMView(username)
	}
	cp.roomArea.Hide()
	for u, v := range cp.dmViews {
		if u == username {
			v.Show()
		} else {
			v.Hide()
		}
	}
	if cp.OnDMOpen != nil {
		cp.OnDMOpen(username)
	}
}

func (cp *ChatPanel) showRoomArea() {
	cp.roomArea.Show()
	for _, v := range cp.dmViews {
		v.Hide()
	}
	if cp.OnDMOpen != nil {
		cp.OnDMOpen("")
	}
}

// SetInputBar places bar at the bottom of the conversation area.
// Call once after the ChatPanel is created.
func (cp *ChatPanel) SetInputBar(bar fyne.CanvasObject) {
	cp.inputWrapper.Objects = []fyne.CanvasObject{hline(), container.NewPadded(bar)}
	cp.inputWrapper.Refresh()
}

// Refresh rebuilds the message box from cp.messages (called after external appends).
func (cp *ChatPanel) Refresh() {
	cp.rebuildMsgBox()
	cp.BaseWidget.Refresh()
}

func (cp *ChatPanel) CreateRenderer() fyne.WidgetRenderer {
	_, panel := panelStack(false, cp.content)
	return widget.NewSimpleRenderer(panel)
}

// TickVoice updates the join/leave button to reflect current sounds state.
// Called every ~50 ms from the StatusPanel animation ticker on the Fyne thread.
func (cp *ChatPanel) TickVoice() {
	if cp.voiceBtn == nil {
		return
	}
	active := sounds.IsActive()
	connecting := sounds.IsConnecting()
	inRoom := cp.currentRoomID != ""

	cp.voiceTick++

	var label string
	var imp widget.Importance
	switch {
	case connecting:
		frames := [4]string{"|", "/", "-", "\\"}
		label = frames[(cp.voiceTick/2)%4] + " Connecting…"
		imp = widget.LowImportance
	case active:
		label = "Leave Voice"
		imp = widget.DangerImportance
	default:
		label = "Join Voice"
		imp = widget.LowImportance
	}

	if cp.voiceBtn.Text != label || cp.voiceBtn.Importance != imp {
		cp.voiceBtn.Text = label
		cp.voiceBtn.Importance = imp
		cp.voiceBtn.Refresh()
	}

	shouldDisable := connecting || (!inRoom && !active)
	if shouldDisable && !cp.voiceBtn.Disabled() {
		cp.voiceBtn.Disable()
	} else if !shouldDisable && cp.voiceBtn.Disabled() {
		cp.voiceBtn.Enable()
	}
}

// ── TOFU / verification popups ────────────────────────────────────────────────

// showTrustPopup is shown before inviting when the key is absent or unverified.
// existing is non-nil when a TOFU record is already on disk (known but unverified).
func showTrustPopup(username string, w fyne.Window, existing *keys.TrustedKey) {
	var pop *widget.PopUp

	// Tailor messaging to whether we've seen this key before.
	var title, headline, desc, continueLabel string
	if existing != nil {
		title = "UNVERIFIED KEY — " + strings.ToUpper(username)
		headline = "⚠  YOU HAVE NOT VERIFIED " + strings.ToUpper(username) + "'S KEY"
		desc = "A key for " + username + " is on disk but was never verified out-of-band. If the broker was compromised the very first time you saw this user, the stored key may belong to an attacker — not " + username + ". Continuing will expose every message and room key you send them to whoever controls that key. Verify out-of-band (voice, in person, trusted channel) before trusting this identity."
		continueLabel = "Continue unverified (risky)"
	} else {
		title = "UNKNOWN KEY — " + strings.ToUpper(username)
		headline = "⚠  " + strings.ToUpper(username) + " IS NOT VERIFIED"
		desc = "No trusted key is on record for " + username + ". The broker will provide one if the user exists — but a malicious broker can silently substitute its own key, decrypt everything you send, re-encrypt it, and forward it to the real user without either of you noticing. Without out-of-band verification, you cannot tell the difference. This is the attack E2EE is designed to prevent."
		continueLabel = "Trust without verification (risky)"
	}

	continueBtn := newBtn(continueLabel, nil)
	verifyBtn := newBtn("Start verification", nil)
	cancelBtn := newBtn("Cancel", nil)
	cancelBtn.Importance = widget.LowImportance

	continueBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
		go func() {
			var keyPEM string
			if existing != nil {
				keyPEM = existing.PublicKey
			} else {
				fetched, err := connection.GetOrStoreUserPublicKey(username)
				if err != nil {
					fyne.Do(func() { showToast(w, "get key: "+err.Error(), toastError) })
					return
				}
				_ = keys.SavePeerPublicKey(username, keys.TrustedKey{
					PublicKey: fetched,
					Verified:  false,
					Method:    keys.TrustMethodTOFU,
				})
				keyPEM = fetched
			}
			_, err := commands.InviteUserWithKey(username, keyPEM)
			fyne.Do(func() {
				if err != nil {
					showToast(w, "invite: "+err.Error(), toastError)
					return
				}
				showToast(w, "invited "+username, toastSuccess)
			})
		}()
	}

	verifyBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
		showVerifyPopup(username, w, func(keyPEM string) {
			_, err := commands.InviteUserWithKey(username, keyPEM)
			fyne.Do(func() {
				if err != nil {
					showToast(w, "invite: "+err.Error(), toastError)
					return
				}
				showToast(w, "verified and invited "+username, toastSuccess)
			})
		})
	}

	cancelBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
	}

	body := container.NewVBox(
		alertRichText(headline), vspace(6),
		popupDescLabel(desc), vspace(10),
		verifyBtn, continueBtn, cancelBtn,
	)
	pop = showNitoPopupSized(title, body, w, 0.33)
}

// showVerifyPopup starts the cryptographic verification flow.
// onSuccess is called with the verified key PEM after the flow completes.
func showVerifyPopup(username string, w fyne.Window, onSuccess func(keyPEM string)) {
	myUsername := connection.GetSessionUsername()
	initiatorPubPEM, err := keys.LoadPublicKeyPEM(myUsername)
	if err != nil {
		showToast(w, "load public key: "+err.Error(), toastError)
		return
	}

	code, sessionID, err := keys.GenerateVerificationChallenge()
	if err != nil {
		showToast(w, "generate verification: "+err.Error(), toastError)
		return
	}

	const verifyTTL = 5 * time.Minute
	expiresAt := time.Now().Add(verifyTTL)
	ctx, cancel := context.WithDeadline(context.Background(), expiresAt)

	clientlog.Info("verify: starting flow for %s (session %s, expires in %s)", username, sessionID, verifyTTL)

	var pop *widget.PopUp
	cancelled := false
	cancelBtn := newBtn("Cancel", nil)
	cancelBtn.Importance = widget.LowImportance
	cancelBtn.OnTapped = func() {
		cancelled = true
		clientlog.Info("verify: user cancelled flow for %s (session %s)", username, sessionID)
		cancel()
		if pop != nil {
			pop.Hide()
		}
	}

	body := container.NewVBox(
		monoTxt("Share this code with "+username+" out-of-band:", liveDimMid), vspace(4),
		monoTxt(code, liveAccent), vspace(8),
		monoTxt("Waiting for their response…", liveDim), vspace(6),
		cancelBtn,
	)
	pop = showNitoPopup("VERIFY "+strings.ToUpper(username), body, w)

	go func() {
		defer cancel()
		if err := connection.SendKeyVerifyChallenge(username, sessionID, initiatorPubPEM, expiresAt.Unix()); err != nil {
			clientlog.Error("verify: send challenge failed for %s: %v", username, err)
			fyne.Do(func() {
				if pop != nil {
					pop.Hide()
				}
				showToast(w, "send challenge: "+err.Error(), toastError)
			})
			return
		}
		resp, err := connection.WaitForKeyVerifyResponse(ctx, sessionID)
		if err != nil {
			if cancelled {
				return // user already saw the popup close; no extra toast needed
			}
			clientlog.Warn("verify: wait failed for %s: %v", username, err)
			fyne.Do(func() {
				if pop != nil {
					pop.Hide()
				}
				showToast(w, "verification: "+err.Error(), toastWarn)
			})
			return
		}
		// A checks that B's signature really covers (code, session, pk_A, pk_B)
		// under pk_B — proves B holds sk_B and saw the out-of-band code.
		if err := keys.VerifyResponseSignature(code, sessionID, initiatorPubPEM, resp.PublicKeyPEM, resp.Signature); err != nil {
			clientlog.Error("verify: response signature check failed for %s: %v", username, err)
			fyne.Do(func() {
				if pop != nil {
					pop.Hide()
				}
				showToast(w, "verification failed: "+err.Error(), toastError)
			})
			return
		}
		// The "A" byte inside the hash tags this signature as the initiator's,
		// keeping it distinct from any other signature sk_A produces.
		confirmSig, err := keys.SignVerificationConfirm(code, sessionID, initiatorPubPEM, resp.PublicKeyPEM, myUsername)
		if err != nil {
			clientlog.Error("verify: sign confirm failed for %s: %v", username, err)
			fyne.Do(func() {
				if pop != nil {
					pop.Hide()
				}
				showToast(w, "sign confirm: "+err.Error(), toastError)
			})
			return
		}
		if err := connection.SendKeyVerifyConfirm(username, sessionID, confirmSig); err != nil {
			clientlog.Error("verify: send confirm failed for %s: %v", username, err)
			fyne.Do(func() {
				if pop != nil {
					pop.Hide()
				}
				showToast(w, "send confirm: "+err.Error(), toastError)
			})
			return
		}
		// A pins B's key only after sending the confirm.
		_ = keys.SavePeerPublicKey(username, keys.TrustedKey{
			PublicKey: resp.PublicKeyPEM,
			Verified:  true,
			Method:    keys.TrustMethodVerified,
		})
		clientlog.Info("verify: successfully verified and pinned key for %s", username)
		fyne.Do(func() {
			if pop != nil {
				pop.Hide()
			}
		})
		onSuccess(resp.PublicKeyPEM)
	}()
}

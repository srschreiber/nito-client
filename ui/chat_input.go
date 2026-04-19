// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ── Emoji catalog ─────────────────────────────────────────────────────────────

type emojiEntry struct {
	Code string
	Char string
}

var chatEmojis = []emojiEntry{
	{"smile", "😀"}, {"joy", "😂"}, {"sweat", "😅"}, {"cool", "😎"},
	{"cry", "😢"}, {"wow", "😮"}, {"angry", "😡"}, {"think", "🤔"},
	{"heart", "❤️"}, {"like", "👍"}, {"dislike", "👎"}, {"party", "🎉"},
	{"fire", "🔥"}, {"100", "💯"}, {"check", "✅"}, {"x", "❌"},
	{"wave", "👋"}, {"muscle", "💪"}, {"pray", "🙏"}, {"laugh", "🤣"},
	{"skull", "💀"}, {"rocket", "🚀"}, {"star", "⭐"},
	{"smirk", "😏"}, {"plead", "🥺"}, {"huff", "😤"},
	{"music", "🎵"}, {"notes", "🎶"}, {"sleep", "😴"},
	{"potato", "🥔"},
}

var emojiByCode = func() map[string]string {
	m := make(map[string]string, len(chatEmojis))
	for _, e := range chatEmojis {
		m[e.Code] = e.Char
	}
	return m
}()

// findActiveShortcode inspects text and returns the byte offset and prefix of
// an in-progress `:prefix` shortcode at the end of the string (no closing `:`,
// no whitespace). Returns ok=false if nothing is active.
func findActiveShortcode(text string) (start int, prefix string, ok bool) {
	lastColon := strings.LastIndex(text, ":")
	if lastColon < 0 {
		return 0, "", false
	}
	if lastColon > 0 {
		ch := text[lastColon-1]
		if ch != ' ' && ch != '\n' && ch != '\t' {
			return 0, "", false
		}
	}
	remainder := text[lastColon+1:]
	if strings.ContainsAny(remainder, ": \n\t") {
		return 0, "", false
	}
	return lastColon, remainder, true
}

// expandFinishedShortcode checks whether text ends in `:code:` or `:code: ` and,
// if code is known, returns text with the shortcode replaced by its emoji.
// Returns the original text if nothing matched.
func expandFinishedShortcode(text string) string {
	trimmed := text
	trailingSpace := ""
	if strings.HasSuffix(trimmed, " ") {
		trimmed = trimmed[:len(trimmed)-1]
		trailingSpace = " "
	}
	if !strings.HasSuffix(trimmed, ":") {
		return text
	}
	// need at least ":x:"
	body := trimmed[:len(trimmed)-1]
	openColon := strings.LastIndex(body, ":")
	if openColon < 0 {
		return text
	}
	if openColon > 0 {
		ch := body[openColon-1]
		if ch != ' ' && ch != '\n' && ch != '\t' {
			return text
		}
	}
	code := body[openColon+1:]
	emoji, ok := emojiByCode[code]
	if !ok {
		return text
	}
	return body[:openColon] + emoji + trailingSpace
}

// ── multilineEntry ────────────────────────────────────────────────────────────

// suggestion is one row in the autocomplete list. Accept is the callback that
// edits the entry text when the user selects the row (via click, Tab, or Enter
// on a highlighted row); different suggestion kinds (emoji inline-replace vs
// chatop full-replace) supply different Accept implementations.
type suggestion struct {
	Label  string
	Accept func()
}

// multilineEntry submits on plain Enter and inserts a newline on Shift+Enter.
// Renders emoji shortcode autocomplete while the user types `:xxx`, expands
// completed `:code:` (or `:code: `) into the emoji, and shows chatop
// autocomplete while the user types a leading `/`.
type multilineEntry struct {
	widget.Entry
	shiftHeld bool
	onSubmit  func(string)

	// Autocomplete state. SuggestCard is the visual wrapper (card + border);
	// the enclosing ChatInput places it inline above the entry. When there
	// are no matches we Hide() the whole card so nothing is visible.
	SuggestCard  *fyne.Container
	suggestList  *fyne.Container
	suggestions  []suggestion
	suggestIndex int
	suppressHook bool // true while programmatically mutating SetText to avoid recursion
}

var _ desktop.Keyable = (*multilineEntry)(nil)

func newMultilineEntry(onSubmit func(string)) *multilineEntry {
	e := &multilineEntry{onSubmit: onSubmit}
	e.MultiLine = true
	e.SetPlaceHolder("type a message")

	e.suggestList = container.NewVBox()
	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 6
	bg.StrokeColor = liveBorder
	bg.StrokeWidth = 1
	e.SuggestCard = container.NewStack(bg, container.NewPadded(e.suggestList))
	e.SuggestCard.Hide()

	e.ExtendBaseWidget(e)
	e.OnChanged = e.onTextChanged
	return e
}

func (e *multilineEntry) KeyDown(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		e.shiftHeld = true
	}
	e.Entry.KeyDown(key)
}

func (e *multilineEntry) KeyUp(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		e.shiftHeld = false
	}
	e.Entry.KeyUp(key)
}

func (e *multilineEntry) TypedKey(key *fyne.KeyEvent) {
	// Autocomplete key handling takes precedence.
	if len(e.suggestions) > 0 {
		switch key.Name {
		case fyne.KeyTab:
			e.acceptSuggestion(e.suggestIndex)
			return
		case fyne.KeyDown:
			e.suggestIndex = (e.suggestIndex + 1) % len(e.suggestions)
			e.renderSuggestions()
			return
		case fyne.KeyUp:
			e.suggestIndex = (e.suggestIndex - 1 + len(e.suggestions)) % len(e.suggestions)
			e.renderSuggestions()
			return
		case fyne.KeyEscape:
			e.hideSuggestions()
			return
		}
	}

	if key.Name == fyne.KeyReturn {
		if e.shiftHeld {
			e.Entry.TypedKey(key)
			return
		}
		text := strings.TrimSpace(e.Text)
		if text != "" && e.onSubmit != nil {
			e.onSubmit(text)
		}
		e.hideSuggestions()
		e.suppressHook = true
		e.SetText("")
		e.suppressHook = false
		return
	}
	e.Entry.TypedKey(key)
}

// appendText appends s to the current entry text (places cursor at end).
func (e *multilineEntry) appendText(s string) {
	e.suppressHook = true
	e.SetText(e.Text + s)
	e.suppressHook = false
}

// onTextChanged is attached to Entry.OnChanged. Handles completed-shortcode
// expansion and routes autocomplete between emoji (`:xxx`) and chatop (`/xxx`)
// modes based on the current text.
func (e *multilineEntry) onTextChanged(s string) {
	if e.suppressHook {
		return
	}
	// 1. Expand `:code:` or `:code: ` if present at the tail.
	if expanded := expandFinishedShortcode(s); expanded != s {
		e.suppressHook = true
		e.SetText(expanded)
		e.suppressHook = false
		e.hideSuggestions()
		return
	}
	// 2. Chatop autocomplete — only active while the user is typing the
	// command name (i.e. text starts with '/' and has no space yet).
	trim := strings.TrimLeft(s, " ")
	if strings.HasPrefix(trim, "/") && !strings.Contains(trim, " ") {
		e.refreshChatOpSuggestions(trim)
		return
	}
	// 3. Emoji autocomplete.
	e.refreshEmojiSuggestions(s)
}

func (e *multilineEntry) refreshEmojiSuggestions(s string) {
	start, prefix, ok := findActiveShortcode(s)
	if !ok || prefix == "" {
		e.hideSuggestions()
		return
	}
	matches := make([]emojiEntry, 0, 8)
	lowerPrefix := strings.ToLower(prefix)
	for _, em := range chatEmojis {
		if strings.HasPrefix(em.Code, lowerPrefix) {
			matches = append(matches, em)
			if len(matches) == 8 {
				break
			}
		}
	}
	if len(matches) == 0 {
		e.hideSuggestions()
		return
	}
	suggestions := make([]suggestion, 0, len(matches))
	for _, m := range matches {
		mm := m
		startRef := start
		suggestions = append(suggestions, suggestion{
			Label: mm.Char + "  :" + mm.Code + ":",
			Accept: func() {
				replaced := e.Text[:startRef] + mm.Char
				e.suppressHook = true
				e.SetText(replaced)
				e.CursorColumn = len(replaced)
				e.suppressHook = false
				e.hideSuggestions()
			},
		})
	}
	e.setSuggestions(suggestions)
}

func (e *multilineEntry) refreshChatOpSuggestions(trimmed string) {
	prefix := strings.TrimPrefix(trimmed, "/")
	ops := ChatOpCompletions(prefix)
	if len(ops) == 0 {
		e.hideSuggestions()
		return
	}
	suggestions := make([]suggestion, 0, len(ops))
	for _, op := range ops {
		opRef := op
		label := "/" + opRef.Name
		if opRef.Desc != "" {
			label += "  —  " + opRef.Desc
		}
		suggestions = append(suggestions, suggestion{
			Label: label,
			Accept: func() {
				// Replace the whole input with `/<name> ` so the user types
				// args next. Cursor moves to end.
				replaced := "/" + opRef.Name + " "
				e.suppressHook = true
				e.SetText(replaced)
				e.CursorColumn = len(replaced)
				e.suppressHook = false
				e.hideSuggestions()
			},
		})
	}
	e.setSuggestions(suggestions)
}

func (e *multilineEntry) setSuggestions(list []suggestion) {
	e.suggestions = list
	if e.suggestIndex >= len(list) {
		e.suggestIndex = 0
	}
	e.showSuggestions()
}

func (e *multilineEntry) showSuggestions() {
	if e.suggestList == nil {
		return
	}
	rows := make([]fyne.CanvasObject, 0, len(e.suggestions))
	for i := range e.suggestions {
		idx := i
		label := monoTxt(e.suggestions[idx].Label, colText)
		row := NewHoverRow(container.NewPadded(label), func() {
			e.acceptSuggestion(idx)
		})
		if idx == e.suggestIndex {
			row.bg.FillColor = liveHover
		}
		rows = append(rows, row)
	}
	e.suggestList.Objects = rows
	e.suggestList.Refresh()
	if e.SuggestCard != nil {
		e.SuggestCard.Show()
	}
}

func (e *multilineEntry) renderSuggestions() { e.showSuggestions() }

func (e *multilineEntry) hideSuggestions() {
	if e.suggestList != nil {
		e.suggestList.Objects = nil
		e.suggestList.Refresh()
	}
	if e.SuggestCard != nil {
		e.SuggestCard.Hide()
	}
	e.suggestions = nil
	e.suggestIndex = 0
}

func (e *multilineEntry) acceptSuggestion(idx int) {
	if idx < 0 || idx >= len(e.suggestions) {
		return
	}
	e.suggestions[idx].Accept()
}

// ── Emoji picker (smiley-button popup) ────────────────────────────────────────

func showEmojiPicker(w fyne.Window, anchor fyne.CanvasObject, entry *multilineEntry) {
	var pop *widget.PopUp

	const cols = 8
	footer := monoTxt(" ", liveDimMid) // updated on hover to show :code:
	cells := make([]fyne.CanvasObject, len(chatEmojis))
	for i, em := range chatEmojis {
		entryRef := em
		lbl := widget.NewLabel(entryRef.Char)
		row := NewHoverRow(container.NewCenter(lbl), func() {
			entry.appendText(entryRef.Char)
			if pop != nil {
				pop.Hide()
			}
			w.Canvas().Focus(entry)
		})
		row.OnHover = func(entered bool) {
			if entered {
				footer.Text = ":" + entryRef.Code + ":"
			} else {
				footer.Text = " "
			}
			footer.Refresh()
		}
		cells[i] = row
	}

	grid := container.NewGridWithColumns(cols, cells...)
	body := container.NewVBox(grid, vspace(2), container.NewPadded(footer))

	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 8
	bg.StrokeColor = liveBorder
	bg.StrokeWidth = 1

	card := container.NewStack(bg, container.NewPadded(body))
	pop = widget.NewPopUp(card, w.Canvas())
	pop.Show()

	ms := pop.MinSize()

	// Anchor the popup's bottom-right near the smiley button's top-right so it
	// appears to float up from the button instead of pinning to the corner.
	var x, y float32
	if anchor != nil {
		abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
		sz := anchor.Size()
		x = abs.X + sz.Width - ms.Width
		y = abs.Y - ms.Height - 4
	} else {
		cs := w.Canvas().Size()
		x = cs.Width - ms.Width - 8
		y = cs.Height - ms.Height - 64
	}
	// Keep it on screen.
	cs := w.Canvas().Size()
	if x < 4 {
		x = 4
	}
	if x+ms.Width > cs.Width-4 {
		x = cs.Width - ms.Width - 4
	}
	if y < 4 {
		y = 4
	}
	pop.Move(fyne.NewPos(x, y))
}

// ── ChatInput ─────────────────────────────────────────────────────────────────

type ChatInput struct {
	widget.BaseWidget
	Entry     *multilineEntry
	modeLabel *canvas.Text
	emojiBtn  *Tappable
	w         fyne.Window
	onSubmit  func(string)
}

func NewChatInput(w fyne.Window, onSubmit func(string)) *ChatInput {
	ci := &ChatInput{w: w, onSubmit: onSubmit}
	ci.modeLabel = txt("> ", liveAccent, 13, false, true)

	ci.Entry = newMultilineEntry(func(text string) {
		// Slash commands route through ExecChatOp instead of being sent as
		// regular text. The host marshals back to the Fyne thread for toasts
		// / SendMessage and reuses ci.onSubmit to publish results to chat.
		if IsChatOp(text) {
			ExecChatOp(text, &chatOpHost{ci: ci})
			return
		}
		if ci.onSubmit != nil {
			ci.onSubmit(text)
		}
	})
	ci.emojiBtn = NewTappable(
		container.NewCenter(txt("☺", liveDimMid, 16, false, false)),
		nil, // tap handler wired after construction (needs ci.emojiBtn self-ref)
	)
	ci.emojiBtn.OnTapped = func() { showEmojiPicker(ci.w, ci.emojiBtn, ci.Entry) }
	ci.ExtendBaseWidget(ci)
	return ci
}

func (ci *ChatInput) SetChatMode(on bool) {
	if on {
		ci.modeLabel.Text = "chat › "
		ci.modeLabel.Color = colLavender
	} else {
		ci.modeLabel.Text = "> "
		ci.modeLabel.Color = liveAccent
	}
	ci.modeLabel.Refresh()
}

func (ci *ChatInput) SetDMMode(username string) {
	if username == "" {
		ci.modeLabel.Text = "> "
		ci.modeLabel.Color = liveAccent
	} else {
		ci.modeLabel.Text = "dm " + username + " › "
		ci.modeLabel.Color = colCyan
	}
	ci.modeLabel.Refresh()
}

// DMTarget returns the username when in DM mode, otherwise "".
func (ci *ChatInput) DMTarget() string {
	text := ci.modeLabel.Text
	if !strings.HasPrefix(text, "dm ") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(text, "dm "), " › ")
}

// chatOpHost implements ChatOpHost for the active chat input. All callbacks
// marshal to the Fyne thread so ops can invoke them from goroutines.
type chatOpHost struct{ ci *ChatInput }

func (h *chatOpHost) SendMessage(text string) {
	fyne.Do(func() {
		if h.ci.onSubmit != nil {
			h.ci.onSubmit(text)
		}
	})
}

func (h *chatOpHost) ShowError(msg string) {
	fyne.Do(func() { showToast(h.ci.w, msg, toastError) })
}

func (ci *ChatInput) CreateRenderer() fyne.WidgetRenderer {
	inputRow := container.NewBorder(nil, nil, container.NewPadded(ci.modeLabel), container.NewPadded(ci.emojiBtn), ci.Entry)

	inputBg := canvas.NewRectangle(liveInputBg)
	inputBg.CornerRadius = 6
	inputBg.StrokeColor = liveBorder
	inputBg.StrokeWidth = 1
	inputArea := container.NewStack(inputBg, container.NewPadded(inputRow))

	// Suggestion list is inline above the input, hidden when empty. No popup
	// or overlay — the entry keeps focus without any workarounds.
	stack := container.NewVBox(ci.Entry.SuggestCard, inputArea)
	return widget.NewSimpleRenderer(stack)
}

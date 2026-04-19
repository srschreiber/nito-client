// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

// SelectableRichText is a minimal styled-text widget that supports mouse
// selection + Cmd/Ctrl+A/C clipboard copy while rendering bold, italic, and
// underline runs. It exists because Fyne's widget.RichText has no selection
// and widget.Entry has no per-run styling.
//
// Markup (deliberately minimal so selection math stays simple):
//
//	**bold**          — fyne.TextStyle{Bold: true}
//	*italic*          — fyne.TextStyle{Italic: true}
//	__under__         — drawn with a canvas.Line under the run
//	~~strike~~        — drawn with a canvas.Line through the run
//	`inline code`     — Slack-style inline monospace pill with dark bg
//	```code block```  — block-level monospace with darker background
//
// Single underscores are treated as literal characters (usernames,
// filenames, and var_names are common in chat and shouldn't italicise).
// Unknown or unmatched markers are also left as literal characters.

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ── Parser ────────────────────────────────────────────────────────────────────

type styledRun struct {
	Text      string
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Code      bool // inline `x` — render as monospace pill with dark bg
	Block     bool // ```x``` — block-level code, own line(s), full-width dark bg
}

// parseStyledText walks text and emits styled runs. Longest-match markers:
// ** → bold, __ → underline, * or _ → italic. Markers toggle; unclosed markers
// stay as literal characters in the last partial run.
func parseStyledText(text string) []styledRun {
	var runs []styledRun
	var buf strings.Builder
	bold, italic, underline, strike := false, false, false, false

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		runs = append(runs, styledRun{
			Text:      buf.String(),
			Bold:      bold,
			Italic:    italic,
			Underline: underline,
			Strike:    strike,
		})
		buf.Reset()
	}

	i := 0
	for i < len(text) {
		// Triple-backtick block code. Greedy: consume through the next ```
		// (no nested inline markers honored inside).
		if i+2 < len(text) && text[i] == '`' && text[i+1] == '`' && text[i+2] == '`' {
			if end := strings.Index(text[i+3:], "```"); end >= 0 {
				flush()
				body := text[i+3 : i+3+end]
				body = strings.Trim(body, "\n")
				runs = append(runs, styledRun{Text: body, Block: true})
				i += 3 + end + 3
				continue
			}
			// Unclosed — fall through to literal byte.
		}
		// Single-backtick inline code.
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				flush()
				body := text[i+1 : i+1+end]
				runs = append(runs, styledRun{Text: body, Code: true})
				i += 1 + end + 1
				continue
			}
			// Unclosed — fall through to literal backtick.
		}
		if i+1 < len(text) && text[i] == '*' && text[i+1] == '*' {
			flush()
			bold = !bold
			i += 2
			continue
		}
		if i+1 < len(text) && text[i] == '_' && text[i+1] == '_' {
			flush()
			underline = !underline
			i += 2
			continue
		}
		if i+1 < len(text) && text[i] == '~' && text[i+1] == '~' {
			flush()
			strike = !strike
			i += 2
			continue
		}
		if text[i] == '*' {
			flush()
			italic = !italic
			i++
			continue
		}
		buf.WriteByte(text[i])
		i++
	}
	flush()
	return runs
}

// ── Layout primitives ─────────────────────────────────────────────────────────

// srtToken is a laid-out run of text at a known (x, y) position within a line.
// w is its measured pixel width; runeStart/runeCount locate it in the widget's
// plaintext (no markers) for selection bookkeeping.
type srtToken struct {
	x, w      float32
	text      string
	style     fyne.TextStyle
	underline bool
	strike    bool
	code      bool // inline code: draw small bg pill behind this token
	runeStart int
	runeCount int
}

type srtLine struct {
	y      float32
	tokens []srtToken
	block  bool // block code: draw full-width dark bg behind the whole line
}

// tokenizeForWrap splits s into alternating non-whitespace/whitespace tokens
// preserving order. Whitespace tokens are collapsed at line wrap; non-whitespace
// tokens flow and wrap to the next line when they don't fit.
func tokenizeForWrap(s string) []string {
	var out []string
	var cur strings.Builder
	var inSpace bool
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		space := c == ' ' || c == '\t'
		if cur.Len() > 0 && space != inSpace {
			flush()
		}
		inSpace = space
		cur.WriteByte(c)
	}
	flush()
	return out
}

func isAllWhitespace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

// ── Widget ────────────────────────────────────────────────────────────────────

type SelectableRichText struct {
	widget.BaseWidget

	raw       string // original source text with markers
	plaintext string // marker-stripped text; selection indices refer to this
	runs      []styledRun

	// fontSize overrides theme.TextSize() when > 0. Used for emoji-only
	// messages (≈36 pt) so they render large but stay selectable/copyable.
	fontSize float32

	lines     []srtLine
	lastWidth float32

	// Selection range in plaintext rune indices [selStart, selEnd). -1 = none.
	selStart int
	selEnd   int
	dragging bool

	focused bool
}

var (
	_ fyne.Focusable      = (*SelectableRichText)(nil)
	_ fyne.Shortcutable   = (*SelectableRichText)(nil)
	_ fyne.Tappable       = (*SelectableRichText)(nil)
	_ fyne.DoubleTappable = (*SelectableRichText)(nil)
	_ desktop.Mouseable   = (*SelectableRichText)(nil)
	_ desktop.Hoverable   = (*SelectableRichText)(nil)
)

func NewSelectableRichText(text string) *SelectableRichText {
	s := &SelectableRichText{raw: text, selStart: -1, selEnd: -1}
	s.runs = parseStyledText(text)
	var pt strings.Builder
	for _, r := range s.runs {
		pt.WriteString(r.Text)
	}
	s.plaintext = pt.String()
	s.ExtendBaseWidget(s)
	return s
}

// NewSelectableRichTextWithSize is like NewSelectableRichText but renders at a
// custom font size. Used for emoji-only messages (large display, still selectable).
func NewSelectableRichTextWithSize(text string, size float32) *SelectableRichText {
	s := NewSelectableRichText(text)
	s.fontSize = size
	return s
}

// ── Layout / wrapping ─────────────────────────────────────────────────────────

// textSize returns the font size to render at: the override if set, else theme.
func (s *SelectableRichText) textSize() float32 {
	if s.fontSize > 0 {
		return s.fontSize
	}
	return theme.TextSize()
}

func (s *SelectableRichText) lineHeight() float32 {
	return s.textSize() * 1.4
}

// rebuildLayout flows runs into lines given the available width.
func (s *SelectableRichText) rebuildLayout(width float32) {
	s.lastWidth = width
	s.lines = nil

	textSize := s.textSize()
	curLine := srtLine{y: 0}
	x := float32(0)
	runeIdx := 0

	finishLine := func() {
		// Strip trailing whitespace so wrap doesn't leave awkward gaps.
		for len(curLine.tokens) > 0 && isAllWhitespace(curLine.tokens[len(curLine.tokens)-1].text) {
			curLine.tokens = curLine.tokens[:len(curLine.tokens)-1]
		}
		s.lines = append(s.lines, curLine)
	}

	for _, run := range s.runs {
		if run.Block {
			// Finish current inline line, emit the code block on its own lines.
			if len(curLine.tokens) > 0 {
				finishLine()
				curLine = srtLine{y: float32(len(s.lines)) * s.lineHeight()}
				x = 0
			}
			// NOTE: intentionally not Monospace — Fyne's default monospace
			// font renders underscores as near-invisible whitespace. The dark
			// background already conveys "this is code"; regular font keeps
			// var_names, snake_case, etc. legible.
			codeStyle := fyne.TextStyle{}
			const codePad = 8
			for li, physLine := range strings.Split(run.Text, "\n") {
				if li > 0 {
					// Plaintext includes the \n separator between physical
					// lines; skip one rune here so token indices stay aligned
					// with plaintext indices for selection/copy.
					runeIdx++
				}
				blockLine := srtLine{y: float32(len(s.lines)) * s.lineHeight(), block: true}
				bx := float32(codePad)
				lineRunes := []rune(physLine)
				lw := fyne.MeasureText(physLine, textSize, codeStyle).Width
				blockLine.tokens = append(blockLine.tokens, srtToken{
					x: bx, w: lw, text: physLine, style: codeStyle,
					code:      false, // whole line has bg via block flag
					runeStart: runeIdx, runeCount: len(lineRunes),
				})
				runeIdx += len(lineRunes)
				s.lines = append(s.lines, blockLine)
			}
			// Start fresh line after the block.
			curLine = srtLine{y: float32(len(s.lines)) * s.lineHeight()}
			x = 0
			continue
		}
		style := fyne.TextStyle{Bold: run.Bold, Italic: run.Italic}
		// Inline code keeps the default font (not Monospace) for the same
		// underscore-rendering reason as block code; the pill background is
		// enough visual distinction.
		for _, tok := range tokenizeForWrap(run.Text) {
			tokRunes := []rune(tok)
			w := fyne.MeasureText(tok, textSize, style).Width
			// Force wrap on explicit \n by splitting tokens further.
			if strings.Contains(tok, "\n") {
				parts := strings.Split(tok, "\n")
				for pi, part := range parts {
					if pi > 0 {
						finishLine()
						curLine = srtLine{y: float32(len(s.lines)) * s.lineHeight()}
						x = 0
					}
					partRunes := []rune(part)
					pw := fyne.MeasureText(part, textSize, style).Width
					if part == "" {
						continue
					}
					if x > 0 && x+pw > width && !isAllWhitespace(part) {
						finishLine()
						curLine = srtLine{y: float32(len(s.lines)) * s.lineHeight()}
						x = 0
					}
					if x == 0 && isAllWhitespace(part) {
						runeIdx += len(partRunes)
						continue
					}
					curLine.tokens = append(curLine.tokens, srtToken{
						x: x, w: pw, text: part, style: style,
						underline: run.Underline, strike: run.Strike, code: run.Code,
						runeStart: runeIdx, runeCount: len(partRunes),
					})
					runeIdx += len(partRunes)
					x += pw
				}
				continue
			}
			if x > 0 && x+w > width && !isAllWhitespace(tok) {
				finishLine()
				curLine = srtLine{y: float32(len(s.lines)) * s.lineHeight()}
				x = 0
			}
			if x == 0 && isAllWhitespace(tok) {
				// Drop leading whitespace of a fresh line.
				runeIdx += len(tokRunes)
				continue
			}
			curLine.tokens = append(curLine.tokens, srtToken{
				x: x, w: w, text: tok, style: style,
				underline: run.Underline, strike: run.Strike, code: run.Code,
				runeStart: runeIdx, runeCount: len(tokRunes),
			})
			runeIdx += len(tokRunes)
			x += w
		}
	}
	if len(curLine.tokens) > 0 {
		finishLine()
	}
}

// ── Hit testing ───────────────────────────────────────────────────────────────

// indexAt returns the plaintext rune index nearest the pixel position (px, py).
// Clamped to [0, len(plaintext-in-runes)].
func (s *SelectableRichText) indexAt(px, py float32) int {
	plainRunes := []rune(s.plaintext)
	if len(s.lines) == 0 {
		return 0
	}
	lh := s.lineHeight()
	li := int(py / lh)
	if li < 0 {
		li = 0
	}
	if li >= len(s.lines) {
		li = len(s.lines) - 1
	}
	line := s.lines[li]
	if len(line.tokens) == 0 {
		if li == 0 {
			return 0
		}
		// End of previous content at this line.
		prev := s.lines[li-1]
		last := prev.tokens[len(prev.tokens)-1]
		return last.runeStart + last.runeCount
	}
	first := line.tokens[0]
	last := line.tokens[len(line.tokens)-1]
	if px <= first.x {
		return first.runeStart
	}
	if px >= last.x+last.w {
		return last.runeStart + last.runeCount
	}
	for _, tok := range line.tokens {
		if px >= tok.x && px < tok.x+tok.w {
			// Bisect into the token to find the exact rune.
			rel := px - tok.x
			runes := []rune(tok.text)
			acc := float32(0)
			for i := 0; i < len(runes); i++ {
				rw := fyne.MeasureText(string(runes[i]), s.textSize(), tok.style).Width
				if rel < acc+rw/2 {
					return tok.runeStart + i
				}
				acc += rw
			}
			return tok.runeStart + len(runes)
		}
	}
	_ = plainRunes
	return last.runeStart + last.runeCount
}

// ── Selection helpers ─────────────────────────────────────────────────────────

func (s *SelectableRichText) hasSelection() bool {
	return s.selStart >= 0 && s.selEnd >= 0 && s.selStart != s.selEnd
}

func (s *SelectableRichText) selectedText() string {
	if !s.hasSelection() {
		return ""
	}
	a, b := s.selStart, s.selEnd
	if a > b {
		a, b = b, a
	}
	runes := []rune(s.plaintext)
	if a < 0 {
		a = 0
	}
	if b > len(runes) {
		b = len(runes)
	}
	return string(runes[a:b])
}

func (s *SelectableRichText) selectAll() {
	s.selStart = 0
	s.selEnd = len([]rune(s.plaintext))
	s.Refresh()
}

func (s *SelectableRichText) clearSelection() {
	s.selStart = -1
	s.selEnd = -1
	s.Refresh()
}

func (s *SelectableRichText) copyToClipboard() {
	if !s.hasSelection() {
		return
	}
	if app := fyne.CurrentApp(); app != nil {
		app.Clipboard().SetContent(s.selectedText())
	}
}

// ── fyne.Focusable ────────────────────────────────────────────────────────────

func (s *SelectableRichText) FocusGained() { s.focused = true }
func (s *SelectableRichText) FocusLost() {
	s.focused = false
	s.dragging = false
	s.clearSelection()
}
func (s *SelectableRichText) TypedRune(r rune)        {}
func (s *SelectableRichText) TypedKey(*fyne.KeyEvent) {}

// Tapped is a no-op handler; Fyne's framework routes focus to Tappable objects
// on click, so implementing this interface is what enables the widget to be
// focused (and thus receive Cmd/Ctrl+C shortcuts). MouseDown does the actual
// selection work.
func (s *SelectableRichText) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s)
	}
}

// DoubleTapped selects the entire line under the cursor. Matches the common
// terminal/IDE convention where double-click picks the current line out of a
// block of text so Cmd+C grabs it as a unit.
func (s *SelectableRichText) DoubleTapped(e *fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s)
	}
	if len(s.lines) == 0 {
		return
	}
	lh := s.lineHeight()
	li := int(e.Position.Y / lh)
	if li < 0 {
		li = 0
	}
	if li >= len(s.lines) {
		li = len(s.lines) - 1
	}
	line := s.lines[li]
	if len(line.tokens) == 0 {
		return
	}
	first := line.tokens[0]
	last := line.tokens[len(line.tokens)-1]
	s.selStart = first.runeStart
	s.selEnd = last.runeStart + last.runeCount
	s.Refresh()
}

// TypedShortcut handles Cmd/Ctrl+A and Cmd/Ctrl+C.
func (s *SelectableRichText) TypedShortcut(sc fyne.Shortcut) {
	switch sc.(type) {
	case *fyne.ShortcutSelectAll:
		s.selectAll()
	case *fyne.ShortcutCopy:
		s.copyToClipboard()
	}
}

// ── desktop.Mouseable ─────────────────────────────────────────────────────────

func (s *SelectableRichText) MouseDown(e *desktop.MouseEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s)
	}
	idx := s.indexAt(e.Position.X, e.Position.Y)
	s.selStart = idx
	s.selEnd = idx
	s.dragging = true
	s.Refresh()
}

func (s *SelectableRichText) MouseUp(*desktop.MouseEvent) {
	s.dragging = false
}

// ── desktop.Hoverable (for drag to extend selection) ──────────────────────────

func (s *SelectableRichText) MouseIn(*desktop.MouseEvent) {}
func (s *SelectableRichText) MouseOut()                   {}
func (s *SelectableRichText) MouseMoved(e *desktop.MouseEvent) {
	if !s.dragging {
		return
	}
	// If the user released the mouse outside our bounds we never get MouseUp,
	// so dragging would stay true forever. Fyne populates e.Button with the
	// currently-held button during a move; 0 means nothing is held.
	if e.Button == 0 {
		s.dragging = false
		return
	}
	s.selEnd = s.indexAt(e.Position.X, e.Position.Y)
	s.Refresh()
}

func (s *SelectableRichText) Cursor() desktop.Cursor { return desktop.TextCursor }

// ── Renderer ──────────────────────────────────────────────────────────────────

func (s *SelectableRichText) CreateRenderer() fyne.WidgetRenderer {
	r := &srtRenderer{widget: s}
	return r
}

type srtRenderer struct {
	widget  *SelectableRichText
	objects []fyne.CanvasObject
}

func (r *srtRenderer) Destroy() {}

func (r *srtRenderer) Layout(size fyne.Size) {
	w := size.Width
	if w <= 0 {
		w = r.widget.lastWidth
	}
	if w != r.widget.lastWidth || len(r.widget.lines) == 0 {
		r.widget.rebuildLayout(w)
	}
	r.rebuildObjects()
}

func (r *srtRenderer) MinSize() fyne.Size {
	if len(r.widget.lines) == 0 {
		r.widget.rebuildLayout(r.widget.lastWidth)
	}
	h := float32(len(r.widget.lines)) * r.widget.lineHeight()
	if h == 0 {
		h = r.widget.lineHeight()
	}
	return fyne.NewSize(r.widget.lastWidth, h)
}

func (r *srtRenderer) Refresh() {
	r.rebuildObjects()
	canvas.Refresh(r.widget)
}

func (r *srtRenderer) Objects() []fyne.CanvasObject { return r.objects }

// rebuildObjects regenerates canvas objects from the computed layout and
// current selection. Called on Layout and Refresh.
func (r *srtRenderer) rebuildObjects() {
	s := r.widget
	textSize := s.textSize()
	lh := s.lineHeight()

	var objs []fyne.CanvasObject

	// Code-block backgrounds (painted first, behind selection + text).
	// Consecutive block lines are merged into a single rectangle so the
	// rounded corners don't create visible gaps between adjacent lines.
	// colInputBg recolours with the active theme profile.
	for i := 0; i < len(s.lines); {
		if !s.lines[i].block {
			i++
			continue
		}
		start := i
		for i < len(s.lines) && s.lines[i].block {
			i++
		}
		end := i - 1
		top := s.lines[start].y
		bottom := s.lines[end].y + s.lineHeight()
		bg := canvas.NewRectangle(colInputBg)
		bg.CornerRadius = 4
		bg.Move(fyne.NewPos(0, top))
		bg.Resize(fyne.NewSize(s.lastWidth, bottom-top))
		objs = append(objs, bg)
	}

	// Inline-code pills (one per token).
	for _, line := range s.lines {
		if line.block {
			continue
		}
		for _, tok := range line.tokens {
			if !tok.code {
				continue
			}
			bg := canvas.NewRectangle(colInputBg)
			bg.CornerRadius = 3
			// Slight horizontal padding so the pill doesn't clip the glyphs.
			const pad = 2
			bg.Move(fyne.NewPos(tok.x-pad, line.y+2))
			bg.Resize(fyne.NewSize(tok.w+2*pad, s.lineHeight()-4))
			objs = append(objs, bg)
		}
	}

	// Selection highlight rectangles (painted under text).
	if s.hasSelection() {
		a, b := s.selStart, s.selEnd
		if a > b {
			a, b = b, a
		}
		highlight := color.NRGBA{R: colAccentDark.R, G: colAccentDark.G, B: colAccentDark.B, A: 0x80}
		for _, line := range s.lines {
			for _, tok := range line.tokens {
				tokStart := tok.runeStart
				tokEnd := tok.runeStart + tok.runeCount
				if tokEnd <= a || tokStart >= b {
					continue
				}
				lo := tokStart
				hi := tokEnd
				if lo < a {
					lo = a
				}
				if hi > b {
					hi = b
				}
				runes := []rune(tok.text)
				relStart := lo - tokStart
				relEnd := hi - tokStart
				if relStart < 0 {
					relStart = 0
				}
				if relEnd > len(runes) {
					relEnd = len(runes)
				}
				prefixW := fyne.MeasureText(string(runes[:relStart]), textSize, tok.style).Width
				selW := fyne.MeasureText(string(runes[relStart:relEnd]), textSize, tok.style).Width
				rect := canvas.NewRectangle(highlight)
				rect.Move(fyne.NewPos(tok.x+prefixW, line.y))
				rect.Resize(fyne.NewSize(selW, lh))
				objs = append(objs, rect)
			}
		}
	}

	// Text + underline passes.
	for _, line := range s.lines {
		for _, tok := range line.tokens {
			t := canvas.NewText(tok.text, colText)
			t.TextSize = textSize
			t.TextStyle = tok.style
			t.Move(fyne.NewPos(tok.x, line.y))
			t.Resize(fyne.NewSize(tok.w, lh))
			objs = append(objs, t)

			if tok.underline {
				ul := canvas.NewLine(colText)
				ul.StrokeWidth = 1
				underlineY := line.y + textSize*1.15
				ul.Position1 = fyne.NewPos(tok.x, underlineY)
				ul.Position2 = fyne.NewPos(tok.x+tok.w, underlineY)
				objs = append(objs, ul)
			}
			if tok.strike {
				sl := canvas.NewLine(colText)
				sl.StrokeWidth = 1
				strikeY := line.y + textSize*0.65
				sl.Position1 = fyne.NewPos(tok.x, strikeY)
				sl.Position2 = fyne.NewPos(tok.x+tok.w, strikeY)
				objs = append(objs, sl)
			}
		}
	}

	r.objects = objs
}

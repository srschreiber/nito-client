// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ── Primitive helpers ─────────────────────────────────────────────────────────

func txt(s string, col color.Color, size float32, bold, mono bool) *canvas.Text {
	t := canvas.NewText(s, col)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold, Monospace: mono}
	return t
}

func monoTxt(s string, col color.Color) *canvas.Text { return txt(s, col, 13, false, true) }
func dimTxt(s string) *canvas.Text                   { return monoTxt(s, liveDim) }
func sectionBadge(s string) *canvas.Text             { return txt(s, liveAccent, 11, true, true) }

// truncLabel returns a Label that clips to "…" when the allocated width is
// too narrow to show the full text.  Use inside container.NewBorder (not HBox)
// so the label actually receives the remaining width to fill.
func truncLabel(s string, _ color.Color, bold bool) *widget.Label {
	l := widget.NewLabel(s)
	l.Truncation = fyne.TextTruncateEllipsis
	l.TextStyle = fyne.TextStyle{Monospace: true, Bold: bold}
	return l
}

func hline() *canvas.Rectangle {
	r := canvas.NewRectangle(liveSep)
	r.SetMinSize(fyne.NewSize(0, 1))
	return r
}

func vspace(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(colTransparent)
	r.SetMinSize(fyne.NewSize(0, h))
	return r
}

// panelStack wraps content in a stack with a rounded bordered panel background.
func panelStack(focused bool, content fyne.CanvasObject) (*canvas.Rectangle, fyne.CanvasObject) {
	bg := canvas.NewRectangle(liveSurface)
	bg.CornerRadius = 6
	if focused {
		bg.StrokeColor = liveBorderFocus
		bg.StrokeWidth = 1.5
	} else {
		bg.StrokeColor = liveBorder
		bg.StrokeWidth = 1
	}
	return bg, container.NewStack(bg, container.NewPadded(content))
}

// ── Tappable ──────────────────────────────────────────────────────────────────

type Tappable struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	OnTapped func()
}

func NewTappable(content fyne.CanvasObject, onTap func()) *Tappable {
	t := &Tappable{content: content, OnTapped: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *Tappable) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}
func (t *Tappable) Tapped(_ *fyne.PointEvent) {
	if t.OnTapped != nil {
		t.OnTapped()
	}
}
func (t *Tappable) TappedSecondary(_ *fyne.PointEvent) {}
func (t *Tappable) Cursor() desktop.Cursor             { return desktop.PointerCursor }

// ── withPointerCursor ─────────────────────────────────────────────────────────

// cursorLayer is a transparent overlay that makes the mouse cursor a pointer.
// It implements only desktop.Cursorable so taps, drags, and hover events all
// fall through to the widget underneath.
type cursorLayer struct{ widget.BaseWidget }

func (c *cursorLayer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(colTransparent))
}
func (c *cursorLayer) Cursor() desktop.Cursor { return desktop.PointerCursor }

// withPointerCursor wraps any widget so hovering over it shows the pointer
// cursor. Safe for buttons, selects, checks, and sliders — events pass through.
func withPointerCursor(obj fyne.CanvasObject) fyne.CanvasObject {
	layer := &cursorLayer{}
	layer.ExtendBaseWidget(layer)
	return container.NewStack(obj, layer)
}

// hoverAbsorber is a transparent overlay that consumes MouseIn/MouseOut/
// MouseMoved events so the underlying widget never enters its hover state.
// It still shows a pointer cursor and lets drags, taps, and scrolls fall
// through to the widget underneath.
type hoverAbsorber struct{ widget.BaseWidget }

func (h *hoverAbsorber) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(colTransparent))
}
func (h *hoverAbsorber) Cursor() desktop.Cursor           { return desktop.PointerCursor }
func (h *hoverAbsorber) MouseIn(_ *desktop.MouseEvent)    {}
func (h *hoverAbsorber) MouseMoved(_ *desktop.MouseEvent) {}
func (h *hoverAbsorber) MouseOut()                        {}

// withPointerCursorNoHover wraps a widget so the pointer cursor shows on
// hover but the widget's built-in hover highlight (e.g. the slider thumb
// turning purple) is suppressed.
func withPointerCursorNoHover(obj fyne.CanvasObject) fyne.CanvasObject {
	layer := &hoverAbsorber{}
	layer.ExtendBaseWidget(layer)
	return container.NewStack(obj, layer)
}

// ── nitoBtn ───────────────────────────────────────────────────────────────────

// nitoBtn extends widget.Button to respond to both Space and Enter/Return, and
// to show the pointer cursor on hover without needing a withPointerCursor wrapper.
// Use newBtn() everywhere instead of widget.NewButton().
type nitoBtn struct {
	widget.Button
}

func newBtn(label string, tapped func()) *nitoBtn {
	b := &nitoBtn{}
	b.Text = label
	b.OnTapped = tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *nitoBtn) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *nitoBtn) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeySpace || ev.Name == fyne.KeyReturn {
		b.Tapped(nil)
	}
}

// ── PillPlayBtn ───────────────────────────────────────────────────────────────

// PillPlayBtn mirrors CircleStopBtn exactly — same 18×18 circle, same colors —
// but shows "▶" instead of "■". Call SetIcon to toggle between ▶ and ■.
type PillPlayBtn struct {
	widget.BaseWidget
	circ    *canvas.Rectangle
	label   *canvas.Text
	isThumb bool // true for 48×48 thumb variant (keeps white label)
	onTap   func()
}

func newPillPlayBtn(icon string, onTap func()) *PillPlayBtn {
	b := &PillPlayBtn{onTap: onTap}
	b.circ = canvas.NewRectangle(liveAccentDark)
	b.circ.CornerRadius = 9
	b.circ.SetMinSize(fyne.NewSize(18, 18))
	b.label = txt(icon, liveAccent, 9, false, false)
	b.ExtendBaseWidget(b)
	return b
}

func (b *PillPlayBtn) SetIcon(icon string) {
	b.label.Text = icon
	b.label.Refresh()
}

func (b *PillPlayBtn) Refresh() {
	b.circ.FillColor = liveAccentDark
	b.circ.Refresh()
	if !b.isThumb {
		b.label.Color = liveAccent
		b.label.Refresh()
	}
	b.BaseWidget.Refresh()
}

// newThumbPlayBtn creates a 48×48 fully-rounded play button — used as the
// album-art thumb in audio embeds. Shows ♪ when idle, ■ when playing.
func newThumbPlayBtn(icon string, onTap func()) *PillPlayBtn {
	b := &PillPlayBtn{onTap: onTap, isThumb: true}
	b.circ = canvas.NewRectangle(liveAccentDark)
	b.circ.CornerRadius = 24
	b.circ.SetMinSize(fyne.NewSize(48, 48))
	b.label = txt(icon, color.White, 22, true, false)
	b.ExtendBaseWidget(b)
	return b
}

func (b *PillPlayBtn) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(
		container.NewStack(b.circ, container.NewCenter(b.label)),
	)
}

func (b *PillPlayBtn) Tapped(_ *fyne.PointEvent) {
	b.circ.FillColor = liveAccent
	b.circ.Refresh()
	go func() {
		time.Sleep(120 * time.Millisecond)
		fyne.Do(func() {
			b.circ.FillColor = liveAccentDark
			b.circ.Refresh()
		})
		if b.onTap != nil {
			b.onTap()
		}
	}()
}

func (b *PillPlayBtn) TappedSecondary(_ *fyne.PointEvent) {}

func (b *PillPlayBtn) MouseIn(_ *desktop.MouseEvent) {
	b.circ.FillColor = liveBorderFocus
	b.circ.Refresh()
}
func (b *PillPlayBtn) MouseMoved(_ *desktop.MouseEvent) {}
func (b *PillPlayBtn) MouseOut() {
	b.circ.FillColor = liveAccentDark
	b.circ.Refresh()
}
func (b *PillPlayBtn) Cursor() desktop.Cursor { return desktop.PointerCursor }

// ── HoverRow ──────────────────────────────────────────────────────────────────

// HoverRow is a tappable row that highlights its background on hover.
type HoverRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	bg      *canvas.Rectangle
	OnTap   func()
}

func NewHoverRow(content fyne.CanvasObject, onTap func()) *HoverRow {
	h := &HoverRow{content: content, OnTap: onTap}
	h.bg = canvas.NewRectangle(colTransparent)
	h.bg.CornerRadius = 4
	h.ExtendBaseWidget(h)
	return h
}

func (h *HoverRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(h.bg, h.content))
}

func (h *HoverRow) Tapped(_ *fyne.PointEvent) {
	if h.OnTap != nil {
		h.OnTap()
	}
}

func (h *HoverRow) TappedSecondary(_ *fyne.PointEvent) {}

func (h *HoverRow) MouseIn(_ *desktop.MouseEvent) {
	h.bg.FillColor = liveHover
	h.bg.Refresh()
}

func (h *HoverRow) MouseMoved(_ *desktop.MouseEvent) {}

func (h *HoverRow) MouseOut() {
	h.bg.FillColor = colTransparent
	h.bg.Refresh()
}

func (h *HoverRow) Cursor() desktop.Cursor { return desktop.PointerCursor }

// ── TabBar ────────────────────────────────────────────────────────────────────

type TabBar struct {
	widget.BaseWidget
	tabs     []string
	Active   int
	OnChange func(int)
}

func NewTabBar(tabs []string, active int, onChange func(int)) *TabBar {
	tb := &TabBar{tabs: tabs, Active: active, OnChange: onChange}
	tb.ExtendBaseWidget(tb)
	return tb
}

func (tb *TabBar) SetActive(i int) {
	tb.Active = i
	tb.Refresh()
}

func (tb *TabBar) Prev() {
	if tb.Active > 0 {
		tb.Active--
		if tb.OnChange != nil {
			tb.OnChange(tb.Active)
		}
		tb.Refresh()
	}
}

func (tb *TabBar) Next() {
	if tb.Active < len(tb.tabs)-1 {
		tb.Active++
		if tb.OnChange != nil {
			tb.OnChange(tb.Active)
		}
		tb.Refresh()
	}
}

func (tb *TabBar) CreateRenderer() fyne.WidgetRenderer {
	var labels []*widget.Label
	var bgs []*canvas.Rectangle
	var items []fyne.CanvasObject

	for i, name := range tb.tabs {
		idx := i
		label := widget.NewLabel(name)
		label.TextStyle = fyne.TextStyle{Monospace: true}
		label.Truncation = fyne.TextTruncateEllipsis
		label.Importance = widget.LowImportance
		bg := canvas.NewRectangle(colTransparent)
		bg.CornerRadius = 4
		if i == tb.Active {
			bg.FillColor = liveHover
		}
		hoverRow := NewHoverRow(
			container.NewStack(bg, container.NewPadded(label)),
			func() {
				tb.Active = idx
				if tb.OnChange != nil {
					tb.OnChange(idx)
				}
				tb.Refresh()
			},
		)
		labels = append(labels, label)
		bgs = append(bgs, bg)
		items = append(items, hoverRow)
	}

	row := container.New(&tabBarLayout{n: len(items)}, items...)

	return &tabBarRenderer{bar: tb, row: row, labels: labels, bgs: bgs}
}

// tabBarLayout distributes available width equally across all tabs so the bar
// can be narrowed freely — labels truncate with "…" instead of pushing wider.
type tabBarLayout struct{ n int }

func (l *tabBarLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if l.n == 0 {
		return
	}
	w := size.Width / float32(l.n)
	for i, o := range objs {
		o.Move(fyne.NewPos(float32(i)*w, 0))
		o.Resize(fyne.NewSize(w, size.Height))
	}
}

func (l *tabBarLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(float32(l.n)*28, 28)
}

type tabBarRenderer struct {
	bar    *TabBar
	row    *fyne.Container
	labels []*widget.Label
	bgs    []*canvas.Rectangle
}

func (r *tabBarRenderer) Layout(size fyne.Size)        { r.row.Resize(size) }
func (r *tabBarRenderer) MinSize() fyne.Size           { return r.row.MinSize() }
func (r *tabBarRenderer) Destroy()                     {}
func (r *tabBarRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.row} }
func (r *tabBarRenderer) Refresh() {
	for i, bg := range r.bgs {
		if i == r.bar.Active {
			bg.FillColor = liveHover
		} else {
			bg.FillColor = colTransparent
		}
		bg.Refresh()
	}
	r.row.Refresh()
}

func (tb *TabBar) Refresh() { tb.BaseWidget.Refresh() }

// ── responsiveGrid ────────────────────────────────────────────────────────────

// responsiveGrid is a fyne.Layout that wraps items into rows whose column count
// adapts to the container width. Maximum column count is capped at maxCols.
// minCellW is the target minimum width per cell used to calculate columns.
//
// MinSize always reflects the current column count (stored from the last Layout
// call) so the parent container allocates the right height as the panel resizes.
type responsiveGrid struct {
	maxCols   int
	minCellW  float32
	lastWidth float32 // last observed width; used in MinSize
}

func newResponsiveGrid(maxCols int, minCellW float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(&responsiveGrid{maxCols: maxCols, minCellW: minCellW}, objs...)
}

func (l *responsiveGrid) cols(width float32, n int) int {
	c := int(width / l.minCellW)
	if c < 1 {
		c = 1
	}
	if c > l.maxCols {
		c = l.maxCols
	}
	if c > n {
		c = n
	}
	return c
}

func (l *responsiveGrid) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	n := len(objs)
	if n == 0 {
		return
	}
	l.lastWidth = size.Width
	cols := l.cols(size.Width, n)
	cellW := size.Width / float32(cols)
	rows := (n + cols - 1) / cols

	// Per-row heights: tallest min-height in each row.
	rowH := make([]float32, rows)
	for i, o := range objs {
		if h := o.MinSize().Height; h > rowH[i/cols] {
			rowH[i/cols] = h
		}
	}

	var y float32
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= n {
				break
			}
			objs[i].Move(fyne.NewPos(float32(c)*cellW, y))
			objs[i].Resize(fyne.NewSize(cellW, rowH[r]))
		}
		y += rowH[r]
	}
}

func (l *responsiveGrid) MinSize(objs []fyne.CanvasObject) fyne.Size {
	n := len(objs)
	if n == 0 {
		return fyne.Size{}
	}
	w := l.lastWidth
	if w < l.minCellW {
		w = l.minCellW
	}
	cols := l.cols(w, n)
	rows := (n + cols - 1) / cols

	var maxCellW, maxCellH float32
	for _, o := range objs {
		ms := o.MinSize()
		if ms.Width > maxCellW {
			maxCellW = ms.Width
		}
		if ms.Height > maxCellH {
			maxCellH = ms.Height
		}
	}
	return fyne.NewSize(maxCellW, maxCellH*float32(rows))
}

// ── CollapseSection ───────────────────────────────────────────────────────────

// CollapseSection is a tappable header that shows ▶ when collapsed and ▼ when
// expanded, with the body shown/hidden below.
type CollapseSection struct {
	widget.BaseWidget
	title     string
	open      bool
	body      fyne.CanvasObject
	arrow     *canvas.Text
	titleText *canvas.Text // stored so Refresh() can update colour on theme change
}

func NewCollapseSection(title string, body fyne.CanvasObject, startOpen bool) *CollapseSection {
	arrowText := "▶ "
	if startOpen {
		arrowText = "▼ "
	}
	c := &CollapseSection{
		title:     title,
		open:      startOpen,
		body:      body,
		arrow:     txt(arrowText, liveAccent, 11, true, true),
		titleText: txt(title, liveAccent, 11, true, true),
	}
	if !startOpen {
		body.Hide()
	}
	c.ExtendBaseWidget(c)
	return c
}

func (c *CollapseSection) CreateRenderer() fyne.WidgetRenderer {
	toggle := func() {
		c.open = !c.open
		if c.open {
			c.arrow.Text = "▼ "
			c.body.Show()
		} else {
			c.arrow.Text = "▶ "
			c.body.Hide()
		}
		c.arrow.Refresh()
		c.Refresh()
	}
	header := NewHoverRow(container.NewHBox(c.arrow, c.titleText), toggle)
	layout := container.NewVBox(header, c.body)
	return widget.NewSimpleRenderer(layout)
}

func (c *CollapseSection) Refresh() {
	c.arrow.Color = liveAccent
	c.arrow.Refresh()
	c.titleText.Color = liveAccent
	c.titleText.Refresh()
	c.BaseWidget.Refresh()
}

// ── Popup helper ──────────────────────────────────────────────────────────────

// showNitoPopup shows a styled floating popup that auto-dismisses when the user
// clicks outside it (uses widget.PopUp, not a modal dialog).
func showNitoPopup(title string, body fyne.CanvasObject, w fyne.Window) *widget.PopUp {
	bg := canvas.NewRectangle(liveSurface)
	bg.CornerRadius = 16
	bg.StrokeColor = liveBorderFocus
	bg.StrokeWidth = 1.5

	content := container.NewVBox(
		sectionBadge(title),
		vspace(8),
		body,
	)
	inner := container.NewPadded(container.NewPadded(container.NewPadded(content)))
	card := container.NewStack(bg, inner)

	pop := widget.NewPopUp(card, w.Canvas())
	pop.Show()
	cs := w.Canvas().Size()
	ms := pop.MinSize()
	pop.Move(fyne.NewPos((cs.Width-ms.Width)/2, (cs.Height-ms.Height)/2))
	return pop
}

// ── Toast ─────────────────────────────────────────────────────────────────────

type toastKind int

const (
	toastInfo toastKind = iota
	toastSuccess
	toastError
	toastWarn
)

// showToast displays a brief auto-dismissing notification at the bottom of the window.
func showToast(w fyne.Window, message string, kind toastKind) {
	var accentCol color.Color
	var icon string
	switch kind {
	case toastSuccess:
		accentCol = colGreen
		icon = "✓ "
	case toastError:
		accentCol = color.NRGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff}
		icon = "✕ "
	case toastWarn:
		accentCol = colAmber
		icon = "⚠ "
	default:
		accentCol = liveAccent
		icon = "· "
	}

	iconLabel := txt(icon, accentCol, 13, true, true)
	msgLabel := monoTxt(message, colText)

	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 8
	bg.StrokeColor = accentCol
	bg.StrokeWidth = 1.5

	card := container.NewStack(bg,
		container.NewPadded(container.NewPadded(
			container.NewHBox(iconLabel, msgLabel),
		)),
	)

	pop := widget.NewPopUp(card, w.Canvas())
	pop.Show()

	cs := w.Canvas().Size()
	ms := pop.MinSize()
	pop.Move(fyne.NewPos(cs.Width-ms.Width-16, cs.Height-ms.Height-32))

	go func() {
		time.Sleep(3 * time.Second)
		fyne.Do(pop.Hide)
	}()
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

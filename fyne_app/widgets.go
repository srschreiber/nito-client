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
func dimTxt(s string) *canvas.Text                   { return monoTxt(s, colDim) }
func sectionBadge(s string) *canvas.Text             { return txt(s, colAccent, 11, true, true) }

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
	r := canvas.NewRectangle(colSep)
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
	bg := canvas.NewRectangle(colSurface)
	bg.CornerRadius = 6
	if focused {
		bg.StrokeColor = colBorderFocus
		bg.StrokeWidth = 1.5
	} else {
		bg.StrokeColor = colBorder
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
	h.bg.FillColor = colHover
	h.bg.Refresh()
}

func (h *HoverRow) MouseMoved(_ *desktop.MouseEvent) {}

func (h *HoverRow) MouseOut() {
	h.bg.FillColor = colTransparent
	h.bg.Refresh()
}

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
	var labels []*canvas.Text
	var bgs []*canvas.Rectangle
	var items []fyne.CanvasObject

	for i, name := range tb.tabs {
		idx := i
		isActive := i == tb.Active
		labelCol := colDimMid
		labelBold := false
		if isActive {
			labelCol = colAccent
			labelBold = true
		}
		label := txt(name, labelCol, 12, labelBold, true)
		bg := canvas.NewRectangle(colTransparent)
		bg.CornerRadius = 4
		if isActive {
			bg.FillColor = colTabActive
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

	row := container.NewHBox(items...)

	return &tabBarRenderer{bar: tb, row: row, labels: labels, bgs: bgs}
}

type tabBarRenderer struct {
	bar    *TabBar
	row    *fyne.Container
	labels []*canvas.Text
	bgs    []*canvas.Rectangle
}

func (r *tabBarRenderer) Layout(size fyne.Size)        { r.row.Resize(size) }
func (r *tabBarRenderer) MinSize() fyne.Size           { return r.row.MinSize() }
func (r *tabBarRenderer) Destroy()                     {}
func (r *tabBarRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.row} }
func (r *tabBarRenderer) Refresh() {
	for i, label := range r.labels {
		if i == r.bar.Active {
			label.Color = colAccent
			label.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
			r.bgs[i].FillColor = colTabActive
		} else {
			label.Color = colDimMid
			label.TextStyle = fyne.TextStyle{Monospace: true}
			r.bgs[i].FillColor = colTransparent
		}
		label.Refresh()
		r.bgs[i].Refresh()
	}
}

func (tb *TabBar) Refresh() { tb.BaseWidget.Refresh() }

// ── CollapseSection ───────────────────────────────────────────────────────────

// CollapseSection is a tappable header that shows ▶ when collapsed and ▼ when
// expanded, with the body shown/hidden below.
type CollapseSection struct {
	widget.BaseWidget
	title string
	open  bool
	body  fyne.CanvasObject
	arrow *canvas.Text
}

func NewCollapseSection(title string, body fyne.CanvasObject, startOpen bool) *CollapseSection {
	arrowText := "▶ "
	if startOpen {
		arrowText = "▼ "
	}
	c := &CollapseSection{
		title: title,
		open:  startOpen,
		body:  body,
		arrow: txt(arrowText, colAccent, 11, true, true),
	}
	if !startOpen {
		body.Hide()
	}
	c.ExtendBaseWidget(c)
	return c
}

func (c *CollapseSection) CreateRenderer() fyne.WidgetRenderer {
	titleLabel := txt(c.title, colText, 11, true, true)
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
	header := NewHoverRow(container.NewHBox(c.arrow, titleLabel), toggle)
	layout := container.NewVBox(header, c.body)
	return widget.NewSimpleRenderer(layout)
}

// ── Popup helper ──────────────────────────────────────────────────────────────

// showNitoPopup shows a styled floating popup that auto-dismisses when the user
// clicks outside it (uses widget.PopUp, not a modal dialog).
func showNitoPopup(title string, body fyne.CanvasObject, w fyne.Window) *widget.PopUp {
	bg := canvas.NewRectangle(colSurface)
	bg.CornerRadius = 16
	bg.StrokeColor = colBorderFocus
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
		accentCol = colAccent
		icon = "· "
	}

	iconLabel := txt(icon, accentCol, 13, true, true)
	msgLabel := monoTxt(message, colText)

	bg := canvas.NewRectangle(colSurface2)
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

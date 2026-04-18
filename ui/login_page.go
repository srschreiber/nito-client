// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"context"
	"image/color"
	"net"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/srschreiber/nito-client/engine/commands"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

// normalizeBrokerURL ensures public hosts always use https; local/private hosts
// default to http.
func normalizeBrokerURL(raw string) string {
	s := raw
	if !strings.Contains(s, "://") {
		s = "placeholder://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return raw
	}
	host := u.Hostname()
	private := host == "localhost" || !strings.Contains(host, ".")
	if !private {
		ip := net.ParseIP(host)
		if ip != nil {
			private = ip.IsLoopback() || ip.IsPrivate()
		}
	}
	if private {
		if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
			u.Scheme = "http"
			return u.String()
		}
		return raw
	}
	u.Scheme = "https"
	return u.String()
}

// showLoginView replaces the window content with the login/register form.
// onSuccess is called on the Fyne main thread after successful auth.
func showLoginView(a fyne.App, w fyne.Window, onSuccess func()) {
	prefs := loadLoginPrefs()

	isLogin := true

	// ── Fields ────────────────────────────────────────────────────────────────
	brokerEntry := widget.NewEntry()
	brokerEntry.SetText(prefs.Broker)
	brokerEntry.SetPlaceHolder("broker URL")

	usernameEntry := widget.NewEntry()
	usernameEntry.SetText(prefs.Username)
	usernameEntry.SetPlaceHolder("username")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("password")

	rememberCheck := widget.NewCheck("remember me", nil)
	rememberCheck.SetChecked(prefs.Broker != "" || prefs.Username != "")

	// ── Error / loading state ─────────────────────────────────────────────────
	errLabel := canvas.NewText("", color.NRGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff})
	errLabel.TextSize = 12
	errLabel.TextStyle = fyne.TextStyle{Monospace: true}
	errLabel.Hide()

	loadingLabel := txt("connecting…", liveDimMid, 12, false, true)
	loadingLabel.Hide()

	var submitBtn *nitoBtn

	submitFn := func() {
		broker := normalizeBrokerURL(strings.TrimSpace(brokerEntry.Text))
		username := strings.TrimSpace(usernameEntry.Text)
		password := passwordEntry.Text

		showErr := func(msg string) {
			errLabel.Text = msg
			errLabel.Show()
			errLabel.Refresh()
		}

		if broker == "" {
			showErr("broker URL is required")
			return
		}
		if username == "" {
			showErr("username is required")
			return
		}
		if password == "" {
			showErr("password is required")
			return
		}

		submitBtn.Disable()
		errLabel.Hide()
		loadingLabel.Show()
		loadingLabel.Refresh()

		isLoginCopy := isLogin
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var err error
			if isLoginCopy {
				_, _, err = commands.LoginDirect(ctx, broker, username, password)
			} else {
				_, _, err = commands.RegisterDirect(ctx, broker, username, password)
			}
			fyne.Do(func() {
				loadingLabel.Hide()
				submitBtn.Enable()
				if err != nil {
					action := "login"
					if !isLoginCopy {
						action = "register"
					}
					clientlog.Error(action + " failed for " + username + ": " + err.Error())
					errLabel.Text = err.Error()
					errLabel.Show()
					errLabel.Refresh()
					return
				}
				action := "logged in"
				if !isLoginCopy {
					action = "registered"
				}
				clientlog.Info(action + " as " + username + " @ " + broker)
				if rememberCheck.Checked {
					saveLoginPrefs(loginPrefs{Broker: broker, Username: username})
				} else {
					clearLoginPrefs()
				}
				onSuccess()
			})
		}()
	}

	submitBtn = newBtn("Continue", submitFn)
	submitBtn.Importance = widget.HighImportance

	// Enter to advance / submit
	brokerEntry.OnSubmitted = func(string) { w.Canvas().Focus(usernameEntry) }
	usernameEntry.OnSubmitted = func(string) { w.Canvas().Focus(passwordEntry) }
	passwordEntry.OnSubmitted = func(string) { submitFn() }

	// ── Key-storage notice (shown only on REGISTER tab) ──────────────────────
	amberCol := color.NRGBA{R: 0xfb, G: 0xbf, B: 0x24, A: 0xff}

	noticeBg := canvas.NewRectangle(color.NRGBA{R: 0x1e, G: 0x18, B: 0x08, A: 0xff})
	noticeBg.CornerRadius = 6
	noticeBg.StrokeColor = amberCol
	noticeBg.StrokeWidth = 1

	noticeHeader := txt("⚠  your keys", amberCol, 12, true, false)
	noticeLine := func(s string) *canvas.Text { return monoTxt(s, colText) }
	noticeDim := func(s string) *canvas.Text { return monoTxt(s, liveDimMid) }

	noticeBody := container.NewVBox(
		noticeHeader,
		vspace(6),
		noticeLine("nito is end-to-end encrypted. registering"),
		noticeLine("generates an RSA key pair stored at:"),
		vspace(4),
		noticeDim("  ~/.nito/<broker>/users/<username>/keys/"),
		vspace(4),
		noticeLine("the private key never leaves your device."),
		noticeLine("if you lose it, your account cannot be"),
		noticeLine("recovered — there is no reset."),
		vspace(4),
		monoTxt("back up that directory.", amberCol),
	)

	registerNotice := container.NewStack(
		noticeBg,
		container.NewPadded(container.NewPadded(noticeBody)),
	)
	registerNotice.Hide()

	// ── Mode toggle ───────────────────────────────────────────────────────────
	modeTabBar := NewTabBar([]string{"LOGIN", "REGISTER"}, 0, func(i int) {
		isLogin = i == 0
		if i == 0 {
			registerNotice.Hide()
		} else {
			registerNotice.Show()
		}
	})

	// ── Brand area ────────────────────────────────────────────────────────────
	titleText := txt("nito", liveAccent, 30, true, true)
	subtitleText := txt("encrypted sounds & chat", liveDim, 11, false, true)
	brandArea := container.NewVBox(
		container.NewCenter(titleText),
		vspace(2),
		container.NewCenter(subtitleText),
	)

	// ── About link ────────────────────────────────────────────────────────────
	aboutBtn := newBtn("about & licenses", func() { showAboutWindow(a) })
	aboutBtn.Importance = widget.LowImportance

	// ── Min-width spacer ──────────────────────────────────────────────────────
	minW := canvas.NewRectangle(colTransparent)
	minW.SetMinSize(fyne.NewSize(340, 0))

	// ── Assemble ──────────────────────────────────────────────────────────────
	form := container.NewVBox(
		minW,
		brandArea,
		vspace(16),
		hline(),
		vspace(10),
		modeTabBar,
		vspace(10),
		registerNotice,
		vspace(4),
		monoTxt("broker", liveDimMid), brokerEntry,
		vspace(8),
		monoTxt("username", liveDimMid), usernameEntry,
		vspace(8),
		monoTxt("password", liveDimMid), passwordEntry,
		vspace(12),
		withPointerCursor(rememberCheck),
		vspace(12),
		submitBtn,
		loadingLabel,
		errLabel,
		vspace(14),
		hline(),
		vspace(8),
		container.NewCenter(aboutBtn),
	)

	bg := canvas.NewRectangle(colBg)
	_, card := panelStack(false, container.NewPadded(form))

	w.SetContent(container.NewStack(bg, container.NewCenter(card)))

	// Focus first empty field.
	if brokerEntry.Text == "" {
		w.Canvas().Focus(brokerEntry)
	} else if usernameEntry.Text == "" {
		w.Canvas().Focus(usernameEntry)
	} else {
		w.Canvas().Focus(passwordEntry)
	}
}

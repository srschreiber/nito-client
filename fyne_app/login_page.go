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
	"github.com/srschreiber/nito-client/shellapp/commands"
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

	brokerEntry := widget.NewEntry()
	brokerEntry.SetText(prefs.Broker)
	brokerEntry.SetPlaceHolder("broker URL (e.g. localhost:8080)")

	usernameEntry := widget.NewEntry()
	usernameEntry.SetText(prefs.Username)
	usernameEntry.SetPlaceHolder("username")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("password")

	rememberCheck := widget.NewCheck("Remember me", nil)
	rememberCheck.SetChecked(prefs.Broker != "" || prefs.Username != "")

	errLabel := txt("", color.NRGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff}, 12, false, true)
	errLabel.Hide()

	var submitBtn *widget.Button
	var loadingLabel *canvas.Text

	submitFn := func() {
		broker := normalizeBrokerURL(strings.TrimSpace(brokerEntry.Text))
		username := strings.TrimSpace(usernameEntry.Text)
		password := passwordEntry.Text

		if broker == "" {
			errLabel.Text = "broker URL is required"
			errLabel.Show()
			errLabel.Refresh()
			return
		}
		if username == "" {
			errLabel.Text = "username is required"
			errLabel.Show()
			errLabel.Refresh()
			return
		}
		if password == "" {
			errLabel.Text = "password is required"
			errLabel.Show()
			errLabel.Refresh()
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
					errLabel.Text = err.Error()
					errLabel.Show()
					errLabel.Refresh()
					return
				}
				if rememberCheck.Checked {
					saveLoginPrefs(loginPrefs{Broker: broker, Username: username})
				} else {
					clearLoginPrefs()
				}
				onSuccess()
			})
		}()
	}

	submitBtn = widget.NewButton("Submit", submitFn)
	submitBtn.Importance = widget.HighImportance

	loadingLabel = txt("connecting...", colDimMid, 12, false, true)
	loadingLabel.Hide()

	modeTabBar := NewTabBar([]string{"LOGIN", "REGISTER"}, 0, func(i int) {
		isLogin = i == 0
	})

	aboutBtn := widget.NewButton("About / Licenses", func() { showAboutWindow(a) })
	aboutBtn.Importance = widget.LowImportance

	// Allow submitting with Enter in the password field.
	passwordEntry.OnSubmitted = func(string) { submitFn() }
	usernameEntry.OnSubmitted = func(string) { w.Canvas().Focus(passwordEntry) }
	brokerEntry.OnSubmitted = func(string) { w.Canvas().Focus(usernameEntry) }

	minWidth := canvas.NewRectangle(colTransparent)
	minWidth.SetMinSize(fyne.NewSize(380, 0))

	form := container.NewVBox(
		modeTabBar,
		vspace(12),
		monoTxt("Broker URL", colDimMid), brokerEntry,
		vspace(6),
		monoTxt("Username", colDimMid), usernameEntry,
		vspace(6),
		monoTxt("Password", colDimMid), passwordEntry,
		vspace(8),
		rememberCheck,
		vspace(10),
		submitBtn,
		loadingLabel,
		errLabel,
		vspace(16),
		hline(),
		vspace(6),
		aboutBtn,
	)

	bg := canvas.NewRectangle(colBg)
	_, card := panelStack(false, container.NewVBox(
		sectionBadge("NITO"),
		vspace(10),
		minWidth,
		form,
	))

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

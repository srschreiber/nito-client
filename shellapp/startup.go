// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"context"
	_ "embed"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"gopkg.in/yaml.v3"
)

//go:embed tag.txt
var appVersion string

// ── login prefs persistence (~/.nito/defaults.yml) ────────────────────────────

type loginPrefs struct {
	Broker     string `yaml:"broker"`
	Username   string `yaml:"username"`
	ShowCmdTab bool   `yaml:"show_cmd_tab"`
}

func loginPrefsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nito", "defaults.yml"), nil
}

func loadLoginPrefs() loginPrefs {
	p, err := loginPrefsPath()
	if err != nil {
		return loginPrefs{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return loginPrefs{}
	}
	var prefs loginPrefs
	_ = yaml.Unmarshal(data, &prefs)
	return prefs
}

func saveLoginPrefs(prefs loginPrefs) {
	p, err := loginPrefsPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	data, err := yaml.Marshal(prefs)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}

func clearLoginPrefs() {
	p, err := loginPrefsPath()
	if err != nil {
		return
	}
	_ = os.Remove(p)
}

// ── startup form ─────────────────────────────────────────────────────────────

type startupState int

const (
	sStateSelect startupState = iota // choose LOGIN / REGISTER
	sStateForm                       // fill in fields
	sStateAbout                      // about / licenses
)

const (
	sfBroker     = 0
	sfUsername   = 1
	sfPassword   = 2
	sfRememberMe = 3 // checkbox
	sfSubmit     = 4 // virtual "button" after the last field
)

type startupAuthMsg struct {
	msg    string
	signal commands.Signal
	err    error
}

type startupModel struct {
	state  startupState
	login  bool // true=LOGIN, false=REGISTER
	btnSel int  // 0=LOGIN, 1=REGISTER in sStateSelect

	vals       [3]string // broker, username, password
	curs       [3]int    // cursor positions in each field
	focus      int       // sfBroker/sfUsername/sfPassword/sfRememberMe/sfSubmit
	rememberMe bool

	termW, termH int
	loading      bool
	errMsg       string
	successMsg   string
	done         bool

	aboutCursor int // selected license index
	aboutScroll int // scroll offset in license text
	aboutFocus  int // 0=list pane, 1=text pane
}

func newStartupModel() startupModel {
	m := startupModel{termW: 80, termH: 24}
	prefs := loadLoginPrefs()
	if prefs.Broker != "" || prefs.Username != "" {
		m.vals[sfBroker] = prefs.Broker
		m.curs[sfBroker] = len([]rune(prefs.Broker))
		m.vals[sfUsername] = prefs.Username
		m.curs[sfUsername] = len([]rune(prefs.Username))
		m.rememberMe = true
	}
	return m
}

// ── startup styles ────────────────────────────────────────────────────────────

var (
	sDialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 3)

	sTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F25D94")).
			Bold(true).
			MarginBottom(1)

	sSubtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888B7E")).
			MarginBottom(1)

	sLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	sFieldStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444")).
			Padding(0, 1).
			Width(44)

	sFieldFocusedStyle = sFieldStyle.
				BorderForeground(lipgloss.Color("#874BFD"))

	sButtonStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFF7DB")).
			Background(lipgloss.Color("#888B7E")).
			Padding(0, 3).
			MarginRight(2)

	sButtonActiveStyle = sButtonStyle.
				Background(lipgloss.Color("#F25D94")).
				Underline(true).
				MarginRight(2)

	sErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87")).
			MarginTop(1)

	sNoteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888B7E")).
			Italic(true).
			MarginTop(1)

	sHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555"))
)

// ── Init / Update / View ──────────────────────────────────────────────────────

func (m startupModel) Init() tea.Cmd { return nil }

func (m startupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height

	case startupAuthMsg:
		m.loading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.successMsg = msg.msg
		m.done = true
		return m, tea.Quit

	case tea.PasteMsg:
		if !m.loading && m.state == sStateForm && m.focus < sfRememberMe {
			text := msg.Content
			if text != "" {
				i := m.focus
				runes := []rune(m.vals[i])
				m.vals[i] = string(runes[:m.curs[i]]) + text + string(runes[m.curs[i]:])
				m.curs[i] += len([]rune(text))
			}
		}

	case tea.KeyPressMsg:
		if m.loading {
			return m, nil
		}
		switch m.state {
		case sStateSelect:
			return m.updateSelect(msg)
		case sStateForm:
			return m.updateForm(msg)
		case sStateAbout:
			return m.updateAbout(msg)
		}
	}
	return m, nil
}

func (m startupModel) updateSelect(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "left", "h", "shift+tab":
		m.btnSel = 0
	case "right", "l", "tab":
		m.btnSel = (m.btnSel + 1) % 3
	case "a":
		m.aboutCursor = 0
		m.aboutScroll = 0
		m.state = sStateAbout
	case "enter", " ":
		switch m.btnSel {
		case 0:
			m.login = true
			m.focus = sfBroker
			m.state = sStateForm
		case 1:
			m.login = false
			m.focus = sfBroker
			m.state = sStateForm
		case 2:
			m.aboutCursor = 0
			m.aboutScroll = 0
			m.state = sStateAbout
		}
	}
	return m, nil
}

func (m startupModel) updateAbout(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	maxCursor := len(allLicenses) - 1
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = sStateSelect
	case "tab", "shift+tab":
		if m.aboutFocus == 0 {
			m.aboutFocus = 1
		} else {
			m.aboutFocus = 0
		}
	case "up", "ctrl+p", "k":
		if m.aboutFocus == 0 {
			if m.aboutCursor > 0 {
				m.aboutCursor--
				m.aboutScroll = 0
			}
		} else {
			if m.aboutScroll > 0 {
				m.aboutScroll--
			}
		}
	case "down", "ctrl+n", "j":
		if m.aboutFocus == 0 {
			if m.aboutCursor < maxCursor {
				m.aboutCursor++
				m.aboutScroll = 0
			}
		} else {
			m.aboutScroll++
		}
	}
	return m, nil
}

func (m startupModel) updateForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = sStateSelect
		m.errMsg = ""
		return m, nil

	case "tab", "down", "ctrl+n":
		m.errMsg = ""
		m.focus = (m.focus + 1) % (sfSubmit + 1)
		return m, nil

	case "shift+tab", "up", "ctrl+p":
		m.errMsg = ""
		if m.focus == 0 {
			m.focus = sfSubmit
		} else {
			m.focus--
		}
		return m, nil

	case "enter", " ":
		if m.focus == sfSubmit {
			return m.submit()
		}
		if m.focus == sfRememberMe {
			m.rememberMe = !m.rememberMe
			return m, nil
		}
		if msg.String() == "enter" {
			// move to next field on enter only
			m.focus = (m.focus + 1) % (sfSubmit + 1)
		}
		return m, nil

	case "backspace":
		if m.focus >= sfRememberMe {
			return m, nil
		}
		i := m.focus
		runes := []rune(m.vals[i])
		if m.curs[i] > 0 {
			m.vals[i] = string(append(runes[:m.curs[i]-1], runes[m.curs[i]:]...))
			m.curs[i]--
		}

	case "left", "ctrl+b":
		if m.focus < sfRememberMe && m.curs[m.focus] > 0 {
			m.curs[m.focus]--
		}
	case "right", "ctrl+f":
		if m.focus < sfRememberMe {
			i := m.focus
			if m.curs[i] < len([]rune(m.vals[i])) {
				m.curs[i]++
			}
		}
	case "ctrl+a":
		if m.focus < sfRememberMe {
			m.curs[m.focus] = 0
		}
	case "ctrl+e":
		if m.focus < sfRememberMe {
			m.curs[m.focus] = len([]rune(m.vals[m.focus]))
		}

	default:
		if m.focus >= sfRememberMe {
			return m, nil
		}
		text := msg.Key().Text
		if text != "" {
			i := m.focus
			runes := []rune(m.vals[i])
			m.vals[i] = string(runes[:m.curs[i]]) + text + string(runes[m.curs[i]:])
			m.curs[i] += len([]rune(text))
		}
	}
	return m, nil
}

// normalizeBrokerURL ensures public hosts always use https.
// If no scheme is provided, https is added for public hosts and http for private/local ones.
// An explicit http:// on a public host is upgraded to https://.
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
	// Public host — always https.
	u.Scheme = "https"
	return u.String()
}

func (m startupModel) submit() (tea.Model, tea.Cmd) {
	broker := normalizeBrokerURL(strings.TrimSpace(m.vals[sfBroker]))
	username := strings.TrimSpace(m.vals[sfUsername])
	password := m.vals[sfPassword]

	if broker == "" {
		m.errMsg = "Broker URL is required"
		m.focus = sfBroker
		return m, nil
	}
	if username == "" {
		m.errMsg = "Username is required"
		m.focus = sfUsername
		return m, nil
	}
	if password == "" {
		m.errMsg = "Password is required"
		m.focus = sfPassword
		return m, nil
	}

	if m.rememberMe {
		saveLoginPrefs(loginPrefs{Broker: broker, Username: username})
	} else {
		clearLoginPrefs()
	}

	m.loading = true
	m.errMsg = ""
	isLogin := m.login
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var msg string
		var sig commands.Signal
		var err error
		if isLogin {
			msg, sig, err = commands.LoginDirect(ctx, broker, username, password)
		} else {
			msg, sig, err = commands.RegisterDirect(ctx, broker, username, password)
		}
		return startupAuthMsg{msg: msg, signal: sig, err: err}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m startupModel) View() tea.View {
	var content string
	switch m.state {
	case sStateSelect:
		content = m.renderSelect()
	case sStateForm:
		content = m.renderForm()
	case sStateAbout:
		placed := renderAbout(m.aboutCursor, m.aboutScroll, m.aboutFocus, m.termW, m.termH)
		v := tea.NewView(placed)
		v.AltScreen = true
		return v
	}
	placed := lipgloss.Place(m.termW, m.termH, lipgloss.Center, lipgloss.Center, content)
	v := tea.NewView(placed)
	v.AltScreen = true
	return v
}

func (m startupModel) renderSelect() string {
	title := sTitleStyle.Render("nito")
	subtitle := sSubtitleStyle.Render("Choose an option to get started")

	loginBtn := sButtonStyle.Render("  LOGIN  ")
	registerBtn := sButtonStyle.Render("  REGISTER  ")
	aboutBtn := sButtonStyle.Render("  About / Licenses  ")
	switch m.btnSel {
	case 0:
		loginBtn = sButtonActiveStyle.Render("  LOGIN  ")
	case 1:
		registerBtn = sButtonActiveStyle.Render("  REGISTER  ")
	case 2:
		aboutBtn = sButtonActiveStyle.Render("  About / Licenses  ")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, loginBtn, registerBtn)

	body := lipgloss.JoinVertical(lipgloss.Center, title, subtitle, buttons,
		lipgloss.NewStyle().MarginTop(1).Render(aboutBtn))
	body = lipgloss.NewStyle().Width(56).Align(lipgloss.Center).Render(body)
	dialog := sDialogStyle.Render(body)

	dialogW := lipgloss.Width(dialog)
	hint := lipgloss.NewStyle().Width(dialogW).Align(lipgloss.Center).
		Foreground(lipgloss.Color("#555")).
		Render("←/→  select   enter  confirm   a  about   ctrl+c  quit")
	version := lipgloss.NewStyle().Width(dialogW).Align(lipgloss.Right).
		Foreground(lipgloss.Color("#444")).
		Render(strings.TrimSpace(appVersion))

	return lipgloss.JoinVertical(lipgloss.Left, dialog, hint, version)
}

func (m startupModel) renderForm() string {
	mode := "Login"
	if !m.login {
		mode = "Register"
	}
	title := sTitleStyle.Render(mode)

	labels := []string{"Broker URL", "Username", "Password"}
	var rows []string
	rows = append(rows, title)

	for i := 0; i < 3; i++ {
		rows = append(rows, sLabelStyle.Render(labels[i]))

		val := m.vals[i]
		display := val
		if i == sfPassword {
			display = strings.Repeat("•", len([]rune(val)))
		}

		// render with cursor when focused
		var fieldText string
		if m.focus == i {
			runes := []rune(display)
			pos := m.curs[i]
			if pos >= len(runes) {
				fieldText = display + lipgloss.NewStyle().
					Background(lipgloss.Color("#874BFD")).
					Render(" ")
			} else {
				fieldText = string(runes[:pos]) +
					lipgloss.NewStyle().
						Background(lipgloss.Color("#874BFD")).
						Render(string(runes[pos])) +
					string(runes[pos+1:])
			}
			rows = append(rows, sFieldFocusedStyle.Render(fieldText))
		} else {
			if display == "" {
				display = " " // prevent empty box from collapsing
			}
			rows = append(rows, sFieldStyle.Render(display))
		}
	}

	// register note
	if !m.login {
		rows = append(rows, sNoteStyle.Render("Your keypair will be generated and stored in ~/.nito/"))
	}

	// remember me checkbox
	checkMark := "[ ]"
	if m.rememberMe {
		checkMark = "[x]"
	}
	rememberLabel := sLabelStyle.Render(checkMark + " Remember me")
	if m.focus == sfRememberMe {
		rememberLabel = sButtonActiveStyle.Render(checkMark + " Remember me")
	}
	rows = append(rows, lipgloss.NewStyle().MarginTop(1).Render(rememberLabel))

	// submit button
	submitBtn := sButtonStyle.MarginTop(1).Render("  Submit  ")
	if m.focus == sfSubmit {
		submitBtn = sButtonActiveStyle.MarginTop(1).Render("  Submit  ")
	}
	rows = append(rows, lipgloss.NewStyle().Width(46).Align(lipgloss.Center).Render(submitBtn))

	if m.loading {
		rows = append(rows, sSubtitleStyle.Render("Connecting..."))
	} else if m.errMsg != "" {
		rows = append(rows, sErrorStyle.Render("✗ "+m.errMsg))
	}

	hint := sHintStyle.Render("tab  next field   esc  back   ctrl+c  quit")
	rows = append(rows, lipgloss.NewStyle().MarginTop(1).Render(hint))

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return sDialogStyle.Render(body)
}

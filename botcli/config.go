// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// BotConfig is the on-disk schema loaded from -f bot.yml. Defaults apply to
// any command that doesn't override them; commands without a script are a
// load-time error so a bad config is caught at startup, not at first
// invocation.
type BotConfig struct {
	Worker   BotWorker              `yaml:"worker"`
	Defaults BotDefaults            `yaml:"defaults"`
	Commands map[string]*BotCommand `yaml:"commands"`

	// sourceDir is the absolute path passed via -s. All script paths must
	// resolve under it; we store it on the config so individual command
	// validators can check.
	sourceDir string `yaml:"-"`
	// runner is what BotConfig.Execute hands the script + env to. Set at
	// LoadConfig time: dockerRunner if Worker.Image is configured, otherwise
	// directRunner with a startup warning. Tests bypass this by constructing
	// their own runner.
	runner runner `yaml:"-"`
}

// BotWorker configures the sandbox container used to execute scripts. When
// Image is set, every script runs inside a long-lived container started
// from that image at bot launch — the container only sees the bind-mounted
// (read-only) source dir, never the bot's keys or state. Operators are
// strongly encouraged to set this for production.
type BotWorker struct {
	Image string `yaml:"image"`
	// Network controls whether the worker container has internet access.
	// Defaults to true so scripts can hit external APIs (ChatGPT, etc.).
	// Set false for offline-only scripts (less attack surface).
	Network *bool `yaml:"network,omitempty"`
}

// NetworkEnabled returns true iff the worker should have a network. Pointer
// + nil-default lets `network: false` opt out without a magic sentinel.
func (w BotWorker) NetworkEnabled() bool {
	if w.Network == nil {
		return true
	}
	return *w.Network
}

// BotDefaults are applied to every command that doesn't override them.
type BotDefaults struct {
	RateLimitRPS float64 `yaml:"rate_limit_rps"`
	TimeoutMs    int     `yaml:"timeout_ms"`
}

// BotCommand is one entry under `commands:` in bot.yml.
//
// Fields that can be omitted fall back to BotDefaults. The unexported
// fields are computed at load time so the hot path (Dispatch) doesn't
// re-parse the regex or re-resolve the script path on every message.
type BotCommand struct {
	Script       string   `yaml:"script"`
	Usage        string   `yaml:"usage,omitempty"`
	ArgsRegex    string   `yaml:"args_regex,omitempty"`
	ArgNames     []string `yaml:"arg_names,omitempty"`
	RateLimitRPS float64  `yaml:"rate_limit_rps,omitempty"`
	TimeoutMs    int      `yaml:"timeout_ms,omitempty"`

	// Resolved at load time.
	name       string         `yaml:"-"`
	scriptPath string         `yaml:"-"` // absolute path on host (used by directRunner)
	scriptRel  string         `yaml:"-"` // path relative to sourceDir (used by dockerRunner: /scripts/<rel>)
	regex      *regexp.Regexp `yaml:"-"`
	timeout    time.Duration  `yaml:"-"`
	window     time.Duration  `yaml:"-"` // 1 / RateLimitRPS, rounded up
}

const (
	defaultRateLimitRPS = 1.0
	defaultTimeoutMs    = 5000
	maxTimeoutMs        = 60_000
)

// LoadConfig parses bot.yml and validates every command. The source dir is
// the root against which relative script paths are resolved; no `..` is
// allowed and only files ending in `.sh` may be executed (the script can
// then call python/node/whatever — that's a script-level decision the bot
// doesn't need to know about).
//
// Returns a fully-resolved *BotConfig; the dispatcher and executor read
// the precomputed scriptPath / regex / timeout / window so they never
// touch disk or recompile during message handling.
func LoadConfig(path, sourceDir string) (*BotConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bot config: %w", err)
	}
	var cfg BotConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse bot config: %w", err)
	}

	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve source dir: %w", err)
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return nil, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source %q is not a directory", absSource)
	}
	cfg.sourceDir = absSource

	if cfg.Defaults.RateLimitRPS <= 0 {
		cfg.Defaults.RateLimitRPS = defaultRateLimitRPS
	}
	if cfg.Defaults.TimeoutMs <= 0 {
		cfg.Defaults.TimeoutMs = defaultTimeoutMs
	}

	if len(cfg.Commands) == 0 {
		return nil, fmt.Errorf("bot config has no commands")
	}
	for name, cmd := range cfg.Commands {
		if cmd == nil {
			return nil, fmt.Errorf("command %q is empty", name)
		}
		if err := validateCommand(name, cmd, &cfg); err != nil {
			return nil, fmt.Errorf("command %q: %w", name, err)
		}
	}

	// Pick a runner. With worker.image, scripts run inside a sandbox
	// container that never sees the bot's keys or state. Without it,
	// scripts run in the bot's own process namespace — same fs access
	// as the bot itself, so a malicious script could read keys. Loud
	// warning so operators don't ship that to production by accident.
	if cfg.Worker.Image != "" {
		r, err := newDockerRunner(cfg.Worker, cfg.sourceDir)
		if err != nil {
			return nil, fmt.Errorf("worker: %w", err)
		}
		cfg.runner = r
	} else {
		cfg.runner = directRunner{}
	}
	return &cfg, nil
}

// HasSandbox reports whether the loaded config will execute scripts inside
// an isolated worker container. Used by Main to surface a one-line warning
// when the operator hasn't configured one.
func (cfg *BotConfig) HasSandbox() bool {
	_, ok := cfg.runner.(*dockerRunner)
	return ok
}

// Close releases any runner-held resources (e.g. removes the worker
// container). Safe to call on a nil-runner config.
func (cfg *BotConfig) Close() error {
	if c, ok := cfg.runner.(closer); ok {
		return c.Close()
	}
	return nil
}

// closer is satisfied by runners that hold OS resources beyond the lifetime
// of a single Execute call (the Docker worker container is the obvious one).
type closer interface{ Close() error }

// validateCommand resolves and verifies a single command. Each check
// fails closed: an invalid command name, a script path that escapes
// sourceDir, or a regex that doesn't compile aborts the whole load.
func validateCommand(name string, cmd *BotCommand, cfg *BotConfig) error {
	if !validCommandName(name) {
		return fmt.Errorf("invalid command name (must be [a-z0-9_-]+)")
	}
	cmd.name = name

	if cmd.Script == "" {
		return fmt.Errorf("missing 'script'")
	}
	if strings.Contains(cmd.Script, "..") {
		return fmt.Errorf("script path %q contains '..'", cmd.Script)
	}
	if filepath.IsAbs(cmd.Script) {
		return fmt.Errorf("script path %q must be relative to source dir", cmd.Script)
	}
	cleaned := filepath.Clean(cmd.Script)
	if cleaned != cmd.Script || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("script path %q is not cleanly relative", cmd.Script)
	}
	if !strings.HasSuffix(cleaned, ".sh") {
		return fmt.Errorf("script %q must have .sh extension (call python/node/etc from inside)", cmd.Script)
	}
	abs := filepath.Join(cfg.sourceDir, cleaned)
	// Defense-in-depth: even after Clean+Join, ensure the result is
	// inside sourceDir. Catches symlink shenanigans the cheap-string
	// checks above miss.
	if !strings.HasPrefix(abs, cfg.sourceDir+string(filepath.Separator)) && abs != cfg.sourceDir {
		return fmt.Errorf("script %q resolves outside source dir", cmd.Script)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("script %q: %w", cmd.Script, err)
	}
	if st.IsDir() {
		return fmt.Errorf("script %q is a directory", cmd.Script)
	}
	cmd.scriptPath = abs
	cmd.scriptRel = filepath.ToSlash(cleaned) // forward slashes for in-container path

	if cmd.ArgsRegex != "" {
		re, err := regexp.Compile(cmd.ArgsRegex)
		if err != nil {
			return fmt.Errorf("invalid args_regex: %w", err)
		}
		if len(cmd.ArgNames) > 0 && len(cmd.ArgNames) != re.NumSubexp() {
			return fmt.Errorf("arg_names has %d entries but args_regex has %d capture groups",
				len(cmd.ArgNames), re.NumSubexp())
		}
		for _, an := range cmd.ArgNames {
			if !validArgName(an) {
				return fmt.Errorf("invalid arg_name %q (must be [a-zA-Z0-9_]+)", an)
			}
		}
		cmd.regex = re
	} else if len(cmd.ArgNames) > 0 {
		return fmt.Errorf("arg_names provided without args_regex")
	}

	rps := cmd.RateLimitRPS
	if rps <= 0 {
		rps = cfg.Defaults.RateLimitRPS
	}
	cmd.window = time.Duration(float64(time.Second) / rps)

	tms := cmd.TimeoutMs
	if tms <= 0 {
		tms = cfg.Defaults.TimeoutMs
	}
	if tms > maxTimeoutMs {
		tms = maxTimeoutMs
	}
	cmd.timeout = time.Duration(tms) * time.Millisecond
	return nil
}

func validCommandName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func validArgName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

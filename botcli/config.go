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
	Defaults BotDefaults            `yaml:"defaults"`
	Commands map[string]*BotCommand `yaml:"commands"`

	// sourceDir is the absolute path passed via -s. All script paths must
	// resolve under it; we store it on the config so individual command
	// validators can check.
	sourceDir string `yaml:"-"`
	// dockerRunners are reused across commands that share an image, so
	// startup validation (image inspect) only happens once per image and
	// Close() can iterate every spawned worker. Keyed by image string.
	dockerRunners map[string]*dockerRunner `yaml:"-"`
}

// BotDefaults are applied to every command that doesn't override them.
// Image / Network here propagate down so a uniform-bot config doesn't
// have to repeat them per command; any command can still override.
type BotDefaults struct {
	Image        string  `yaml:"image,omitempty"`
	Network      *bool   `yaml:"network,omitempty"`
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
	// Image overrides worker.image for just this command. Useful when one
	// command needs a different runtime (e.g. !ask wants python:3.12-slim
	// for the openai SDK while !hello is fine on alpine). Falls back to
	// worker.image if absent; if both are absent, the command runs
	// unsandboxed (after the global y/N confirmation prompt).
	Image string `yaml:"image,omitempty"`
	// Env is the allow-list of host env-var names the bot passes through to
	// this command's container at start. Each named var must be set in the
	// bot's process env (otherwise the empty string is passed). Variables
	// listed here are exposed ONLY to this command's container — every
	// command gets its own worker, so a secret routed to `motto` is
	// invisible to scripts under any other command. Use this for per-
	// command secrets (API keys, signing keys) without exposing them to
	// the whole script library.
	Env []string `yaml:"env,omitempty"`
	// Network controls whether this command's container can reach the
	// internet. Defaults to false (--network none) so commands have to
	// opt in to outbound traffic — `!hello` and `!motto` stay offline,
	// `!ask` (which calls ChatGPT) can flip it on. Pointer + nil-default
	// lets the validator distinguish "explicit false" from "absent" if
	// we ever want to change the default.
	Network *bool `yaml:"network,omitempty"`

	// Resolved at load time.
	name       string         `yaml:"-"`
	scriptPath string         `yaml:"-"` // absolute path on host (used by directRunner)
	scriptRel  string         `yaml:"-"` // path relative to sourceDir (used by dockerRunner: /scripts/<rel>)
	regex      *regexp.Regexp `yaml:"-"`
	timeout    time.Duration  `yaml:"-"`
	window     time.Duration  `yaml:"-"` // 1 / RateLimitRPS, rounded up
	image      string         `yaml:"-"` // resolved: cmd.Image || worker.Image; "" → unsandboxed
	runner     runner         `yaml:"-"` // dockerRunner if image != "", else directRunner
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

	// Pick a runner per command. Each command resolves to either a
	// docker-sandboxed runner (image set, by command or default) or
	// directRunner (no image — runs in the bot's own process namespace,
	// triggers the y/N startup prompt). dockerRunner instances are
	// shared across commands with the same image so we only image-
	// inspect each one once.
	cfg.dockerRunners = map[string]*dockerRunner{}
	for _, cmd := range cfg.Commands {
		if cmd.image == "" {
			cmd.runner = directRunner{}
			continue
		}
		dr, ok := cfg.dockerRunners[cmd.image]
		if !ok {
			r, err := newDockerRunner(cmd.image, cfg.sourceDir)
			if err != nil {
				return nil, fmt.Errorf("worker for command %q: %w", cmd.name, err)
			}
			cfg.dockerRunners[cmd.image] = r
			dr = r
		}
		cmd.runner = dr
	}
	return &cfg, nil
}

// HasSandbox reports whether every command in the loaded config will run
// inside an isolated worker container. Returns false if any command has
// no resolved image — Main uses that to decide whether to fire the
// y/N "continue without sandbox" prompt.
func (cfg *BotConfig) HasSandbox() bool {
	for _, cmd := range cfg.Commands {
		if cmd.image == "" {
			return false
		}
	}
	return true
}

// UnsandboxedCommands returns the names of commands that resolved to
// no image — used in the startup warning so the operator knows exactly
// which commands they're approving to run in-process.
func (cfg *BotConfig) UnsandboxedCommands() []string {
	var out []string
	for name, cmd := range cfg.Commands {
		if cmd.image == "" {
			out = append(out, name)
		}
	}
	return out
}

// Close removes every spawned worker container across every image. Safe
// to call on configs with no docker runners.
func (cfg *BotConfig) Close() error {
	var firstErr error
	for _, dr := range cfg.dockerRunners {
		if err := dr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

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

	for _, ev := range cmd.Env {
		if !validEnvName(ev) {
			return fmt.Errorf("invalid env var name %q in env list", ev)
		}
	}

	// Resolve image: per-command override wins, otherwise inherit
	// defaults.image. Empty after both is allowed — that command runs
	// unsandboxed and the startup prompt covers consent.
	cmd.image = cmd.Image
	if cmd.image == "" {
		cmd.image = cfg.Defaults.Image
	}

	// Resolve network: per-command Network wins; else defaults.Network;
	// else off (safer baseline). Pointer-vs-default chain lets an
	// explicit `network: false` on the command shadow a true default.
	if cmd.Network == nil {
		cmd.Network = cfg.Defaults.Network
	}
	return nil
}

// NetworkEnabled reports whether this command's worker container should
// have a network. Default is OFF: the safer posture is "no internet
// unless you ask for it", since most bot commands (greetings, lookups,
// in-script-only logic) don't need it.
func (c *BotCommand) NetworkEnabled() bool {
	if c.Network == nil {
		return false
	}
	return *c.Network
}

// validEnvName mirrors POSIX shell-variable naming: leading letter or
// underscore, then alnum or underscore. We also reject the NITO_*
// prefix because the bot reserves it for per-invocation vars
// (NITO_COMMAND, NITO_ARGS, NITO_ARG_*) — letting an operator
// configure NITO_ARGS as a passthrough would silently override the
// per-call value and break the args parser.
func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "NITO_") || name == "REQUESTER" || name == "PATH" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
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

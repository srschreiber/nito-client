// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import "os"

// stdinIsTTY reports whether standard input is connected to a character
// device. Used to refuse interactive prompts under `docker run -d` (where
// stdin is /dev/null) rather than hang silently.
//
// We avoid pulling in golang.org/x/term to keep the bot image small: the
// stat-mode check matches what isatty does for the POSIX case and is good
// enough for the two branches we care about (interactive terminal vs
// detached container stdin).
func stdinIsTTY() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

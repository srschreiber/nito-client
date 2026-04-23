// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Command nito-bot is a headless nito user. Run it from the generated Docker
// image, or build + run directly as any Go binary. See BOTS.md.
package main

import (
	"os"

	"github.com/srschreiber/nito-client/botcli"
)

func main() {
	os.Exit(botcli.Main())
}

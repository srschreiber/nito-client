// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Command nito-bot is a headless nito user. Run it as `nito-bot -f bot.yml
// -s <source-dir>`. See BOTS.md for the config schema and the bootstrap
// flow (register → verify → invite → serve).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/srschreiber/nito-client/botcli"
)

func main() {
	configPath := flag.String("f", "", "path to bot.yml command config (required)")
	sourceDir := flag.String("s", "", "top-level directory containing command scripts (required)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: nito-bot -f <bot.yml> -s <source-dir>")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *configPath == "" || *sourceDir == "" {
		flag.Usage()
		os.Exit(2)
	}

	os.Exit(botcli.Main(*configPath, *sourceDir))
}

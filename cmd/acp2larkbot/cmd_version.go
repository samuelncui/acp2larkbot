package main

import (
	"fmt"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func versionCmd(args []string) {
	fmt.Printf("acp2larkbot %s (commit: %s, built: %s)\n", version, commit, date)
}

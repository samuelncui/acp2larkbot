package main

import (
	"fmt"
	"os"

	"github.com/samuelncui/acp2larkbot/config"
)

func testCmd(args []string) {
	fs := newFlagSet("test")
	configPath := fs.StringP("config", "c", "config.yaml", "path to YAML config")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	_, err := config.LoadFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("config ok")
}

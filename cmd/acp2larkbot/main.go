package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var usage = `acp2larkbot - Lark bot to ACP agent bridge

USAGE:
    acp2larkbot <command> [options]

COMMANDS:
    run      Start the bot service (default)
    test     Validate configuration
    init     Interactive config wizard
    help     Show detailed help
    version  Show version

OPTIONS:
    -c, --config   Path to config file (default: config.yaml)
    -d, --debug    Enable debug logging
    -h, --help     Show this help

EXAMPLES:
    acp2larkbot run                    Start with config.yaml
    acp2larkbot run -c my.yaml         Start with custom config
    acp2larkbot test -c my.yaml        Validate config
    acp2larkbot init -o config.yaml    Generate config interactively
    acp2larkbot help lark              Lark config help
`

func main() {
	args := os.Args[1:]

	// Handle -h/--help at top level
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return
	}

	// Backward compatibility: detect old-style flags
	if len(args) > 0 && !isSubcommand(args[0]) {
		if isOldFlag(args[0]) {
			// Check if it's a validate-only invocation
			for _, a := range args {
				if a == "-validate-only" || a == "--validate-only" {
					logrus.Warn("-validate-only is deprecated, use 'acp2larkbot test' instead")
					testCmd(filterOldValidateFlags(args))
					return
				}
			}
			logrus.Warn("flag-based usage is deprecated, use 'acp2larkbot run' instead")
			runCmd(args)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		fmt.Print(usage)
		os.Exit(1)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "run":
		runCmd(cmdArgs)
	case "test":
		testCmd(cmdArgs)
	case "init":
		initCmd(cmdArgs)
	case "help":
		helpCmd(cmdArgs)
	case "version":
		versionCmd(cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}
}

func isSubcommand(s string) bool {
	switch s {
	case "run", "test", "init", "help", "version":
		return true
	default:
		return false
	}
}

func isOldFlag(s string) bool {
	return strings.HasPrefix(s, "-")
}

// filterOldValidateFlags converts old-style flags to new-style flags for testCmd.
func filterOldValidateFlags(args []string) []string {
	var result []string
	for _, a := range args {
		if a == "-validate-only" || a == "--validate-only" {
			continue
		}
		// -config foo → -c foo
		if a == "-config" || a == "--config" {
			result = append(result, "-c")
			continue
		}
		result = append(result, a)
	}
	return result
}

// flagSet wraps flag.FlagSet with short alias support.
type flagSet struct {
	*flag.FlagSet
}

func newFlagSet(name string) *flagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return &flagSet{fs}
}

func (f *flagSet) StringP(long, short, def, usage string) *string {
	val := f.FlagSet.String(long, def, usage)
	f.FlagSet.StringVar(val, short, def, usage)
	return val
}

func (f *flagSet) BoolP(long, short string, def bool, usage string) *bool {
	val := f.FlagSet.Bool(long, def, usage)
	f.FlagSet.BoolVar(val, short, def, usage)
	return val
}

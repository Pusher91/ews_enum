package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

func run(args []string, stdout, stderr io.Writer) int {
	cfg, warnings, usage, err := parseConfig(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		fmt.Fprintf(stderr, "[!] %v\n\n%s", err, usage)
		return 1
	}

	for _, warning := range warnings {
		fmt.Fprintln(stderr, warning)
	}

	switch cfg.Mode() {
	case modeSpray:
		return runSpray(cfg, stdout, stderr)
	case modeEnum:
		return runEnum(cfg, stdout, stderr)
	default:
		return runAuthCheck(cfg, stderr)
	}
}

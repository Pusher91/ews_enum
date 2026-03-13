package main

import (
	"bytes"
	"flag"
	"fmt"
)

type appMode int

const (
	modeAuthCheck appMode = iota
	modeSpray
	modeGuess
	modeEnum
	maxEnumDepth = 5
)

type appConfig struct {
	URL        string
	User       string
	UserFile   string
	CredFile   string
	Pass       string
	EnumGAL    bool
	DebugDupes bool
	Format     string
	OutFile    string
	NTLM       bool
	Timeout    int
	Depth      int
	DelayMS    int
	Workers    int
	Conns      int
}

func (c appConfig) Mode() appMode {
	switch {
	case c.CredFile != "":
		return modeGuess
	case c.UserFile != "":
		return modeSpray
	case c.EnumGAL:
		return modeEnum
	default:
		return modeAuthCheck
	}
}

func (c appConfig) Validate() error {
	credSources := 0
	if c.User != "" {
		credSources++
	}
	if c.UserFile != "" {
		credSources++
	}
	if c.CredFile != "" {
		credSources++
	}

	switch {
	case c.URL == "":
		return fmt.Errorf("missing required -url")
	case credSources > 1:
		return fmt.Errorf("must specify exactly one of -user, -userfile, or -credfile")
	case credSources == 0:
		return fmt.Errorf("must specify one of -user, -userfile, or -credfile")
	case c.Workers < 1:
		return fmt.Errorf("-workers must be at least 1")
	case c.Conns < 1:
		return fmt.Errorf("-conns must be at least 1")
	case c.Timeout < 1:
		return fmt.Errorf("-timeout must be at least 1")
	case c.DelayMS < 0:
		return fmt.Errorf("-delay must be at least 0")
	}

	switch c.Mode() {
	case modeSpray:
		if c.Pass == "" {
			return fmt.Errorf("missing required -pass in spray mode")
		}
		if c.Format != "csv" && c.Format != "json" {
			return fmt.Errorf("invalid -format %q for spray mode (allowed: csv, json)", c.Format)
		}
	case modeGuess:
		if c.Format != "csv" && c.Format != "json" {
			return fmt.Errorf("invalid -format %q for guessing mode (allowed: csv, json)", c.Format)
		}
	case modeEnum:
		if c.Pass == "" {
			return fmt.Errorf("missing required -pass in enum mode")
		}
		if c.Depth < 1 {
			return fmt.Errorf("-depth must be at least 1 in enum mode")
		}
		if c.Format != "csv" && c.Format != "json" && c.Format != "emails" {
			return fmt.Errorf("invalid -format %q for enum mode (allowed: csv, json, emails)", c.Format)
		}
	default:
		if c.Pass == "" {
			return fmt.Errorf("missing required -pass in auth-check mode")
		}
	}

	return nil
}

func parseConfig(args []string) (appConfig, []string, string, error) {
	var cfg appConfig
	fs := flag.NewFlagSet("ews_enum", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})

	fs.StringVar(&cfg.URL, "url", "", "EWS endpoint URL (e.g. https://mail.target.com/EWS/Exchange.asmx)")
	fs.StringVar(&cfg.User, "user", "", "Username (DOMAIN\\user or user@domain.com)")
	fs.StringVar(&cfg.UserFile, "userfile", "", "File of usernames for password spraying (one per line)")
	fs.StringVar(&cfg.CredFile, "credfile", "", "File of username:password guesses (one per line, first username wins)")
	fs.StringVar(&cfg.Pass, "pass", "", "Password (required unless using -credfile)")
	fs.BoolVar(&cfg.EnumGAL, "enum", false, "Enumerate the Global Address List after authenticating")
	fs.BoolVar(&cfg.DebugDupes, "debug-dupes", false, "In spray mode, print duplicate usernames after trim/lowercase normalization")
	fs.StringVar(&cfg.Format, "format", "csv", "Output format: csv, json, emails (enum) or csv, json (spray)")
	fs.StringVar(&cfg.OutFile, "o", "", "Output file (default: stdout)")
	fs.BoolVar(&cfg.NTLM, "ntlm", true, "Use NTLM authentication (default true, set -ntlm=false for basic)")
	fs.IntVar(&cfg.Timeout, "timeout", 30, "HTTP timeout in seconds (must be at least 1)")
	fs.IntVar(&cfg.Depth, "depth", 3, "Max prefix depth for enumeration (2=aa, 3=aaa; values above 5 are capped)")
	fs.IntVar(&cfg.DelayMS, "delay", 0, "Minimum delay between request starts in milliseconds, shared across workers (must be at least 0)")
	fs.IntVar(&cfg.Workers, "workers", 10, "Number of concurrent workers")
	fs.IntVar(&cfg.Conns, "conns", 20, "Max concurrent connections to server")

	usage := renderUsage(fs)
	if err := fs.Parse(args); err != nil {
		return cfg, nil, usage, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, nil, usage, err
	}

	var warnings []string
	if cfg.Mode() == modeSpray && cfg.EnumGAL {
		warnings = append(warnings, "[!] -enum is ignored in spray mode")
	}
	if cfg.Mode() == modeGuess && cfg.EnumGAL {
		warnings = append(warnings, "[!] -enum is ignored in guessing mode")
	}
	if cfg.Mode() == modeGuess && cfg.Pass != "" {
		warnings = append(warnings, "[!] -pass is ignored in guessing mode")
	}
	if cfg.Mode() == modeEnum && cfg.Depth > maxEnumDepth {
		warnings = append(warnings, fmt.Sprintf("[!] -depth %d is capped to %d", cfg.Depth, maxEnumDepth))
		cfg.Depth = maxEnumDepth
	}

	return cfg, warnings, usage, nil
}

func renderUsage(fs *flag.FlagSet) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Usage: ews_enum -url <EWS_URL> (-user <USER> -pass <PASS> | -userfile <FILE> -pass <PASS> | -credfile <FILE>) [options]\n\n")
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	return buf.String()
}

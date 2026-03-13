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
	modeEnum
	maxEnumDepth = 5
)

type appConfig struct {
	URL      string
	User     string
	UserFile string
	Pass     string
	EnumGAL  bool
	Format   string
	OutFile  string
	NTLM     bool
	Timeout  int
	Depth    int
	DelayMS  int
	Workers  int
	Conns    int
}

func (c appConfig) Mode() appMode {
	switch {
	case c.UserFile != "":
		return modeSpray
	case c.EnumGAL:
		return modeEnum
	default:
		return modeAuthCheck
	}
}

func (c appConfig) Validate() error {
	switch {
	case c.URL == "" || c.Pass == "":
		return fmt.Errorf("missing required -url or -pass")
	case c.User != "" && c.UserFile != "":
		return fmt.Errorf("cannot use both -user and -userfile")
	case c.User == "" && c.UserFile == "":
		return fmt.Errorf("must specify either -user or -userfile")
	case c.Workers < 1:
		return fmt.Errorf("-workers must be at least 1")
	case c.Conns < 1:
		return fmt.Errorf("-conns must be at least 1")
	case c.Depth < 1:
		return fmt.Errorf("-depth must be at least 1")
	default:
		return nil
	}
}

func parseConfig(args []string) (appConfig, []string, string, error) {
	var cfg appConfig
	fs := flag.NewFlagSet("ews_enum", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})

	fs.StringVar(&cfg.URL, "url", "", "EWS endpoint URL (e.g. https://mail.target.com/EWS/Exchange.asmx)")
	fs.StringVar(&cfg.User, "user", "", "Username (DOMAIN\\user or user@domain.com)")
	fs.StringVar(&cfg.UserFile, "userfile", "", "File of usernames for password spraying (one per line)")
	fs.StringVar(&cfg.Pass, "pass", "", "Password")
	fs.BoolVar(&cfg.EnumGAL, "enum", false, "Enumerate the Global Address List after authenticating")
	fs.StringVar(&cfg.Format, "format", "csv", "Output format: csv, json, emails (enum) or csv, json (spray)")
	fs.StringVar(&cfg.OutFile, "o", "", "Output file (default: stdout)")
	fs.BoolVar(&cfg.NTLM, "ntlm", true, "Use NTLM authentication (default true, set -ntlm=false for basic)")
	fs.IntVar(&cfg.Timeout, "timeout", 30, "HTTP timeout in seconds")
	fs.IntVar(&cfg.Depth, "depth", 3, "Max prefix depth for enumeration (2=aa, 3=aaa; values above 5 are capped)")
	fs.IntVar(&cfg.DelayMS, "delay", 0, "Delay in milliseconds between requests")
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
	if cfg.Mode() == modeEnum && cfg.Depth > maxEnumDepth {
		warnings = append(warnings, fmt.Sprintf("[!] -depth %d is capped to %d", cfg.Depth, maxEnumDepth))
		cfg.Depth = maxEnumDepth
	}

	return cfg, warnings, usage, nil
}

func renderUsage(fs *flag.FlagSet) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Usage: ews_enum -url <EWS_URL> (-user <USER> | -userfile <FILE>) -pass <PASS> [options]\n\n")
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	return buf.String()
}

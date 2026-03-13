package main

import "testing"

func TestParseConfigModesAndWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantMode    appMode
		wantWarning string
	}{
		{
			name:     "auth check default",
			args:     []string{"-url", "https://mail.example.com/EWS/Exchange.asmx", "-user", "alice", "-pass", "secret"},
			wantMode: modeAuthCheck,
		},
		{
			name:     "enum mode",
			args:     []string{"-url", "https://mail.example.com/EWS/Exchange.asmx", "-user", "alice", "-pass", "secret", "-enum"},
			wantMode: modeEnum,
		},
		{
			name:     "guess mode",
			args:     []string{"-url", "https://mail.example.com/EWS/Exchange.asmx", "-credfile", "creds.txt"},
			wantMode: modeGuess,
		},
		{
			name:        "spray mode warns on enum",
			args:        []string{"-url", "https://mail.example.com/EWS/Exchange.asmx", "-userfile", "users.txt", "-pass", "secret", "-enum"},
			wantMode:    modeSpray,
			wantWarning: "[!] -enum is ignored in spray mode",
		},
		{
			name:        "guess mode warns on ignored flags",
			args:        []string{"-url", "https://mail.example.com/EWS/Exchange.asmx", "-credfile", "creds.txt", "-pass", "secret", "-enum"},
			wantMode:    modeGuess,
			wantWarning: "[!] -enum is ignored in guessing mode",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, warnings, _, err := parseConfig(tc.args)
			if err != nil {
				t.Fatalf("parseConfig returned error: %v", err)
			}
			if got := cfg.Mode(); got != tc.wantMode {
				t.Fatalf("mode = %v, want %v", got, tc.wantMode)
			}

			if tc.wantWarning == "" {
				if len(warnings) != 0 {
					t.Fatalf("warnings = %v, want none", warnings)
				}
				return
			}

			if len(warnings) == 0 || warnings[0] != tc.wantWarning {
				t.Fatalf("warnings = %v, want first warning %q", warnings, tc.wantWarning)
			}
		})
	}
}

func TestParseConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "workers must be positive",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-pass", "secret",
				"-workers", "0",
			},
		},
		{
			name: "timeout must be positive",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-pass", "secret",
				"-timeout", "0",
			},
		},
		{
			name: "delay must be non-negative",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-pass", "secret",
				"-delay", "-1000",
			},
		},
		{
			name: "spray rejects emails format",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-userfile", "users.txt",
				"-pass", "secret",
				"-format", "emails",
			},
		},
		{
			name: "enum rejects typo format",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-pass", "secret",
				"-enum",
				"-format", "jsno",
			},
		},
		{
			name: "enum rejects non-positive depth",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-pass", "secret",
				"-enum",
				"-depth", "0",
			},
		},
		{
			name: "cannot combine user and credfile",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-credfile", "creds.txt",
				"-pass", "secret",
			},
		},
		{
			name: "auth mode still requires pass",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := parseConfig(tc.args)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParseConfigCapsEnumDepth(t *testing.T) {
	t.Parallel()

	cfg, warnings, _, err := parseConfig([]string{
		"-url", "https://mail.example.com/EWS/Exchange.asmx",
		"-user", "alice",
		"-pass", "secret",
		"-enum",
		"-depth", "9",
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cfg.Depth != maxEnumDepth {
		t.Fatalf("depth = %d, want %d", cfg.Depth, maxEnumDepth)
	}
	if len(warnings) != 1 || warnings[0] != "[!] -depth 9 is capped to 5" {
		t.Fatalf("warnings = %v, want depth cap warning", warnings)
	}
}

func TestParseConfigAllowsNonPositiveDepthOutsideEnumMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "auth check ignores depth",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-user", "alice",
				"-pass", "secret",
				"-depth", "0",
			},
		},
		{
			name: "spray ignores depth",
			args: []string{
				"-url", "https://mail.example.com/EWS/Exchange.asmx",
				"-userfile", "users.txt",
				"-pass", "secret",
				"-depth", "0",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, _, err := parseConfig(tc.args)
			if err != nil {
				t.Fatalf("parseConfig returned error: %v", err)
			}
			if cfg.Depth != 0 {
				t.Fatalf("depth = %d, want 0", cfg.Depth)
			}
		})
	}
}

func TestParseConfigGuessModeWarnsThatPassIsIgnored(t *testing.T) {
	t.Parallel()

	_, warnings, _, err := parseConfig([]string{
		"-url", "https://mail.example.com/EWS/Exchange.asmx",
		"-credfile", "creds.txt",
		"-pass", "secret",
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	found := false
	for _, warning := range warnings {
		if warning == "[!] -pass is ignored in guessing mode" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want ignored -pass warning", warnings)
	}
}

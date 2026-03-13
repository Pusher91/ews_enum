# ews_enum

Exchange Web Services (EWS) tool for credential validation, GAL enumeration, password spraying, and credential guessing.

Authenticates against an EWS endpoint using NTLM or Basic auth and can enumerate the Global Address List (GAL) via the `ResolveNames` SOAP operation.

## Build

```bash
go build -o ews_enum ./cmd/ews_enum/
```

## Modes

### Credential Check

Test a single set of credentials:

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -user 'DOMAIN\user' -pass 'P@ssword'
```

### GAL Enumeration

Authenticate and enumerate the Global Address List:

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -user 'DOMAIN\user' -pass 'P@ssword' -enum
```

Enumeration works by calling `ResolveNames` with short prefixes (a-z, 0-9). If a prefix returns 100+ results (the EWS truncation limit), it recurses deeper (e.g., `a` → `aa`, `ab`, ...) up to `-depth` characters.

Results stream to stderr as they're found. Final sorted output goes to stdout (or a file with `-o`).

### Password Spraying

Spray a single password against a list of users:

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -userfile users.txt -pass 'P@ssword' -o hits.csv
```

The user file should contain one username per line. Blank lines and `#` comments are skipped.

### Credential Guessing

Try a file of `username:password` guesses, attempting only the first credential seen for each username:

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -credfile guesses.txt -o hits.json -format json
```

The credential file should contain one `username:password` pair per line. Blank lines and `#` comments are skipped. The parser splits on the first `:` only, trims the username, and preserves the rest of the line as the password verbatim, including additional `:` characters or surrounding spaces. An empty password is represented as `username:`. If the same username appears multiple times, only the first pair is attempted and each later duplicate is logged to stderr as skipped.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | | EWS endpoint URL (required) |
| `-user` | | Single username (DOMAIN\user or user@domain.com) |
| `-userfile` | | File of usernames for spraying (one per line) |
| `-credfile` | | File of `username:password` guesses (one per line, first username wins) |
| `-pass` | | Password (required unless using `-credfile`) |
| `-enum` | `false` | Enumerate the GAL after authenticating |
| `-debug-dupes` | `false` | In spray mode, print duplicate usernames after trim/lowercase normalization |
| `-format` | `csv` | Output format: `csv`, `json`, `emails` (enum only) |
| `-o` | stdout | Output file path |
| `-ntlm` | `true` | Use NTLM auth (`false` for Basic) |
| `-timeout` | `30` | HTTP timeout in seconds |
| `-depth` | `3` | Max prefix depth for enumeration |
| `-delay` | `0` | Minimum delay between request starts in milliseconds, shared across workers |
| `-workers` | `10` | Number of concurrent workers |
| `-conns` | `20` | Max concurrent connections to the server |

## Examples

```bash
# Check creds with Basic auth
ews_enum -url https://10.0.0.1/EWS/Exchange.asmx -user 'user@corp.com' -pass 'P@ss' -ntlm=false

# Enumerate GAL, save as JSON
ews_enum -url https://mail.corp.com/EWS/Exchange.asmx -user 'CORP\admin' -pass 'P@ss' -enum -format json -o gal.json

# Enumerate GAL, emails only
ews_enum -url https://mail.corp.com/EWS/Exchange.asmx -user 'CORP\admin' -pass 'P@ss' -enum -format emails -o emails.txt

# Spray with 5 workers and 1s minimum delay between request starts
ews_enum -url https://mail.corp.com/EWS/Exchange.asmx -userfile users.txt -pass 'Summer2026!' -workers 5 -delay 1000

# Spray and show usernames collapsed as duplicates after normalization
ews_enum -url https://mail.corp.com/EWS/Exchange.asmx -userfile users.txt -pass 'Summer2026!' -debug-dupes

# Guess from a username:password file; later duplicate usernames are logged and skipped
ews_enum -url https://mail.corp.com/EWS/Exchange.asmx -credfile guesses.txt -format json -o guess_hits.json

# Fast enumeration with more workers
ews_enum -url https://mail.corp.com/EWS/Exchange.asmx -user 'CORP\svc' -pass 'P@ss' -enum -workers 20 -conns 30
```

## Output Formats

**Enumeration CSV** (default):
```
email,display_name,given_name,surname,title,department,office,company,phone
jsmith@corp.com,John Smith,John,Smith,IT Manager,Information Technology,HQ,Corp Inc,555-1234
```

**Credential CSV** (spray or guess):
```
username,password,status
CORP\jsmith,P@ssword,valid
```

**Emails** (`-format emails`): one email address per line.

**JSON** (`-format json`): array of contact or credential result objects.

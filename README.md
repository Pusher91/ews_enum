# ews_enum

`ews_enum` validates EWS credentials, sprays a password across users, tests `username:password` pairs, and can enumerate the GAL with `ResolveNames`.

Default mode is a single credential check.

## Build

```bash
go build -o ews_enum ./cmd/ews_enum
```

## Modes

### 1. Credential Check

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -user 'DOMAIN\user' -pass 'P@ssword'
```

Checks whether the credentials are valid. This is the default behavior.

### 2. Password Spray

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -userfile users.txt -pass 'Winter2026!' -o hits.csv
```

- `users.txt` is one username per line
- blank lines and `#` comments are ignored
- duplicate usernames are attempted once
- skipped duplicate usernames are printed after the run

### 3. Credential Guessing

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -credfile guesses.txt -format json -o hits.json
```

- `guesses.txt` is one `username:password` pair per line
- the line is split on the first `:`
- the username is trimmed
- the rest of the line is kept as the password verbatim
- `username:` means empty password
- if a username appears more than once, only the first pair is attempted
- skipped `username:password` combinations are printed after the run

### 4. GAL Enumeration

```bash
ews_enum -url https://mail.target.com/EWS/Exchange.asmx -user 'DOMAIN\user' -pass 'P@ssword' -enum
```

Enumeration uses `ResolveNames` over `a-z0-9` prefixes and recurses deeper when Exchange truncates results. Output is streamed to `stderr` as contacts are found, then written in sorted form to `stdout` or `-o`.

## Common Flags

| Flag | Meaning |
|------|---------|
| `-url` | EWS endpoint URL |
| `-user` | Single username |
| `-userfile` | Username list for spray mode |
| `-credfile` | `username:password` list for guess mode |
| `-pass` | Password for auth-check, spray, or enum |
| `-enum` | Enable GAL enumeration |
| `-format` | Spray/guess: `csv` or `json`. Enum: `csv`, `json`, or `emails` |
| `-o` | Write results to a file |
| `-ntlm=false` | Use Basic auth instead of NTLM |
| `-timeout` | HTTP timeout in seconds |
| `-workers` | Number of workers |
| `-conns` | Max in-flight requests |
| `-delay` | Minimum delay between request starts, shared across workers |
| `-depth` | Max enum prefix depth |

## Notes

- Auth-check ignores `-format` and `-o`.
- `-depth` only matters with `-enum`.
- `-debug-dupes` is deprecated and ignored.
- Output files are created with restrictive permissions.

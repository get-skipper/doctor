# Skipper Doctor

A CLI tool to validate your Skipper setup in one shot, before it goes anywhere near CI.

## Installation

Download a binary from [releases](https://github.com/get-skipper/doctor/releases), or build from source:

```bash
git clone https://github.com/get-skipper/doctor.git
cd doctor
go build ./...
./doctor --help
```

## Usage

Provide your credentials and spreadsheet ID via flags or env vars:

```bash
export SKIPPER_SPREADSHEET_ID="1Nbjf_..."
export GOOGLE_CREDS_B64="ey..."  # or GOOGLE_CREDENTIALS_FILE="path/to/sa.json"

doctor
```

Or:

```bash
doctor \
  --spreadsheet-id "1Nbjf_..." \
  --credentials "/path/to/service-account.json" \
  --sheet-name "E2E Tests" \
  --reference-sheets "Shared,Mobile"
```

## Checks

The doctor validates:

- **Credentials parsed** — service account JSON is valid and readable
- **Sheets API credentials** — Google auth succeeds
- **Sheets API reachable** — Google Sheets API responds with 200
- **Spreadsheet accessible** — the configured spreadsheet ID is accessible
- **Sheet tab found** — the configured sheet tab exists and has rows
- **Schema valid** — header row contains the required column names (`testId`, `disabledUntil`)
- **Date formats** — all `disabledUntil` values match `YYYY-MM-DD` (warns on invalid rows)
- **Write permission** — for sync mode, the service account has editor role
- **Reference sheets** — any linked tabs are accessible

Exit code is **0** if all checks pass, **1** if any warning or error is found.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `SKIPPER_SPREADSHEET_ID` | — | Spreadsheet ID (required) |
| `GOOGLE_CREDENTIALS_FILE` | — | Path to service account JSON |
| `GOOGLE_CREDS_B64` | — | Base64-encoded service account JSON (for CI) |
| `SKIPPER_SHEET_NAME` | — | Sheet name (optional, uses first sheet if empty) |
| `SKIPPER_REFERENCE_SHEETS` | — | Comma-separated reference sheet names (optional) |
| `NO_COLOR` | — | Disable colored output |

## GitHub Actions

Add a step to your CI workflow to validate the setup:

```yaml
- name: Run Doctor
  env:
    SKIPPER_SPREADSHEET_ID: ${{ secrets.SKIPPER_SPREADSHEET_ID }}
    GOOGLE_CREDS_B64: ${{ secrets.GOOGLE_CREDS_B64 }}
  run: go run .
```

Store the following as GitHub secrets:

- `SKIPPER_SPREADSHEET_ID` — your Skipper spreadsheet ID
- `GOOGLE_CREDS_B64` — service account JSON, base64-encoded (`cat sa.json | base64 | pbcopy`)

The doctor exits with code **1** if any checks fail, failing the workflow. Fix warnings before enabling sync mode.

## License

MIT

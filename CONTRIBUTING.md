# Contributing

Thanks for your interest in contributing to Skipper Doctor!

## Development Setup

```bash
git clone https://github.com/get-skipper/doctor.git
cd doctor
go mod download
```

## Building

```bash
go build ./...
./doctor --help
```

## Testing

Run `go test` (no integration tests currently, since this tool requires live Google Sheets API credentials):

```bash
go test ./...
go vet ./...
```

## Submitting Changes

### Conventional Commits

We use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages.

Format: `<type>(<scope>): <subject>`

Valid types:
- **feat**: new feature
- **fix**: bug fix
- **refactor**: code refactoring (no functional change)
- **test**: test improvements
- **docs**: documentation changes
- **ci**: CI/CD workflow changes
- **chore**: dependency updates, build changes

Examples:
- `feat: add timeout configuration via env var`
- `fix: handle sheet names with apostrophes`
- `test: add credentials validation test`
- `docs: clarify reference sheets behavior`

### Pull Request Process

1. Fork and create a feature branch from `main`
2. Make your changes and commit with conventional messages
3. Ensure `go test ./...` and `go vet ./...` pass
4. Push to your fork and open a PR

Once merged to `main`, CI runs automatically. To create a release, tag a commit with `v*`:

```bash
git tag v1.0.0
git push origin v1.0.0
```

This triggers the release workflow, which builds binaries for all platforms and uploads them to GitHub Releases.

## Code Style

- Use `gofmt` (automatic via `go fmt ./...`)
- Keep functions focused and small
- Comment exported functions
- Handle errors explicitly (no silent failures)

## Debugging

Enable debug output with `SKIPPER_DEBUG`:

```bash
SKIPPER_DEBUG=1 doctor --spreadsheet-id <id> --credentials <path>
```

(Note: doctor doesn't currently have debug output, but this pattern is available for future enhancements.)

## Questions?

Open an issue or start a discussion on GitHub.

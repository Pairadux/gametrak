# Gametrak

A CLI tool that tracks game time on Linux via Hyprland window events.

## Build & Run

```bash
go build ./...           # build
go run .                 # run daemon
gametrak history today   # example query
gametrak stats week      # example stats
```

No test suite exists yet.

## Architecture

- `cmd/` - Cobra CLI commands (root daemon, history, stats)
- `internal/config/` - Config loading and game defaults
- `internal/hyprland/` - IPC socket event parsing
- `internal/models/` - Session/SessionLog data structures
- `internal/session/` - JSONL session file I/O
- `internal/utility/` - Time formatting, title sanitization, game matching

### Key patterns

- `filterSessions()` and `parseFilterArg()` in `cmd/history.go` are shared by both `history` and `stats` commands
- `matchesTimeFilter()` handles all time filter logic (keywords + arbitrary dates)
- Game titles from Hyprland are sanitized via `utility.SanitizeTitle()` to strip invisible Unicode chars
- Sessions are filtered by **start time only** - a session belongs to the day it started on

## Pending work

- [ ] Write unit tests (especially for `filterSessions`, `matchesTimeFilter`, `parseFilterArg`, `SanitizeTitle`)
- [ ] Clean up the existing sessions file entry for "ARC Raiders" that has invisible Unicode chars baked in (sanitization only applies to new sessions going forward)

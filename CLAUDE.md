# Gametrak

A CLI tool that tracks game time on Linux via Hyprland window events.

## Build & Run

```bash
go build ./...           # build
go test ./...            # run tests
go run .                 # run daemon
gametrak status          # what is being tracked right now
gametrak history today   # example query
gametrak stats week      # example stats
```

## Architecture

- `cmd/` - Cobra CLI commands; thin wrappers that wire flags to internal packages
- `internal/config/` - Config loading, game defaults, add/remove
- `internal/hyprland/` - IPC sockets: event stream (socket2) and requests (socket)
- `internal/models/` - Session/SessionLog/Config data structures
- `internal/output/` - Aligned text tables and proportional bars
- `internal/query/` - Filter parsing and session selection
- `internal/session/` - JSONL session file I/O
- `internal/state/` - Active session persistence for restart recovery
- `internal/stats/` - Totals, streaks, and time buckets
- `internal/tracker/` - Turns window events into recorded sessions
- `internal/utility/` - Time formatting, title sanitization, game matching

### Key patterns

- `internal/query.Filter` resolves filter arguments into a concrete time window
  once, rather than re-deriving it per session. `history`, `stats`, `heatmap`,
  `game`, and `export` all share it via `runQuery()` in `cmd/sessions.go`
- Sessions are filtered by **start time only** - a session belongs to the day it
  started on, and to the hour it started in
- `internal/tracker` holds no package state and takes `now` as a parameter, so
  the recovery paths are testable without Hyprland or a real clock
- Shutdown **does not** record active sessions. Whether a game actually stopped
  is only knowable at the next startup, when Hyprland's window list says whether
  it is still open; recording at shutdown would split one session in two
- Hyprland reports window addresses with an `0x` prefix over the request socket
  but without one over the event socket; `hyprland.NormalizeAddress` reconciles them
- `output.Table` drops columns that are empty in every row, so callers emit
  optional cells (such as an hours column) without special-casing them
- Game titles are sanitized via `utility.SanitizeTitle()` at the point they enter
  the tracker, and again when the log is read, so entries written before
  sanitization existed still group with newer ones
- `config.Load()` reads from viper's in-memory copy, which is **not** re-read
  after a write. `AddGame`/`RemoveGame` return the updated config for this reason

## Pending work

- [ ] Idle detection (a paused game still counts as played time)
- [ ] Playtime goals or limits with warning notifications

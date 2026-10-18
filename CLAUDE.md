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
gametrak sync            # sync with Logbook now (setup steps: gametrak sync --help)
```

## Architecture

- `cmd/` - Cobra CLI commands; thin wrappers that wire flags to internal packages
- `internal/config/` - Config loading, game defaults, add/remove
- `internal/hyprland/` - IPC sockets: event stream (socket2) and requests (socket)
- `internal/lockfile/` - flock-based locks shared by the daemon and other commands
- `internal/logbook/` - Logbook API client, merging the sheet into the log, the sync cycle
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
- The session log is shared with `internal/logbook`: appends (`session.Log`) and
  rewrites (`session.Update`, temp file + rename, previous copy kept as `.bak`,
  unreadable lines preserved) both hold `sessions.jsonl.lock`

### Logbook sync

Sessions are mirrored to the `gaming` sheet of the owner's Logbook site
(`logs.austingause.com`, the Logs-Site repo), which is behind Cloudflare Access.

- **Off by default.** `logbook.enabled` in config.yaml turns it on. The Access
  service token is in its own file (`logbook.credentials_file`, default
  `~/.config/gametrak/logbook-credentials.yaml`), which must be mode 0600; it
  never goes in config.yaml, which may live in a dotfiles repo
- **When:** the daemon syncs at startup, after every logged session (the
  tracker's `OnLogged` hook pokes a coalescing channel), and every
  `logbook.poll_mins` minutes (0 = no polling). `gametrak sync` syncs now. Syncs
  run on their own goroutine so network calls never stall the event loop
- **One cycle** (`Syncer.Sync`, under `logbook.lock` in the state dir so the
  daemon and the command never upload twice): read the sheet's change counter
  (`GET /api/v1/sheets/gaming/version`, one D1 row read); pull the whole sheet
  only if it moved; then upload sessions with no `logbook_id`, oldest first
- **The site wins.** A pull applies edits, deletions and site-added sessions to
  the local log (`logbook.Merge`). gametrak itself never edits a synced
  session, so local changes are only ever new sessions
- **No duplicates.** After uploading N sessions, if the counter moved by exactly
  N nothing else changed and the next sync skips the pull. Otherwise it pulls,
  and an unlinked session whose upload reached the site without a response is
  matched to its row by start time and game (`Changes.Linked`)
- **Guards:** a sync refuses to run against a different URL or sheet than the
  one the log's ids belong to (`logbook.json` records it), and refuses a pull
  that would delete every linked session once there are 3 or more (an empty
  sheet, a reset database, another site). Each rewrite keeps a `.bak` copy
- Durations are whole minutes in Logbook, at least 1 (`logbook.Minutes`). A pull
  keeps a session's exact seconds unless the site changed the minutes. Every
  upload is platform `PC`
- A session the site rejects (400) is remembered in `logbook.json` and skipped
  by automatic syncs, so polls don't resend it forever; `gametrak sync` retries
- The daemon logs each new sync failure once, and sends a notification only for
  failures retrying won't fix (`logbook.Temporary` is false), e.g. a bad token
- Testing against a local Logbook (`pnpm dev` in Logs-Site, which skips Access):
  point `logbook.url` at `http://localhost:5173` in a separate config and data
  directory. Never point a test at the production site
- `go.mod` asks for Go 1.25.6. Where that toolchain can't be downloaded, check
  with an older local Go via a scratch copy of go.mod:
  `GOTOOLCHAIN=local go test -modfile=/tmp/go124.mod ./...` (with `go 1.24`
  in that copy and go.sum copied next to it as `go124.sum`)

## Pending work

- [ ] Idle detection (a paused game still counts as played time)
- [ ] Playtime goals or limits with warning notifications

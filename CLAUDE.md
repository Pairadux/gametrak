# Gametrak

A CLI tool that tracks game time on Linux: via Hyprland window events on the
laptop, or via the Steam client's game log on the Steam Deck (see
[STEAM_DECK.md](STEAM_DECK.md) for the owner's setup steps there).

## Build & Run

```bash
go build ./...           # build
go test ./...            # run tests
go run .                 # run daemon
gametrak status          # what is being tracked right now
gametrak history today   # example query
gametrak stats week      # example stats
gametrak sync            # sync with Logbook now (setup steps: gametrak sync --help)
gametrak import-steam --dry-run   # Steam Deck: preview past sessions from Steam's log
gametrak --version       # "dev" unless built by the release workflow
```

Releases: pushing a `v*` tag runs `.github/workflows/release.yml`, which tests,
builds static `gametrak-linux-amd64` (the Deck) and `gametrak-linux-arm64`, and
attaches them to a GitHub release. It builds with the newest stable Go, not
`go.mod`'s go line: that line is the minimum, and binaries built with an old
patch release carried its standard-library vulnerabilities. The Deck installs from
`releases/latest/download/gametrak-linux-amd64` (the repo is public).

## Architecture

- `cmd/` - Cobra CLI commands; thin wrappers that wire flags to internal packages
- `internal/config/` - Config loading, game defaults, add/remove
- `internal/hyprland/` - IPC sockets: event stream (socket2) and requests (socket), and the Hyprland source
- `internal/lockfile/` - flock-based locks shared by the daemon and other commands
- `internal/logbook/` - Logbook API client, merging the sheet into the log, the sync cycle
- `internal/models/` - Session/SessionLog/Config data structures
- `internal/output/` - Aligned text tables and proportional bars
- `internal/query/` - Filter parsing and session selection
- `internal/session/` - JSONL session file I/O
- `internal/sleepclock/` - Time asleep since boot, from the kernel's clocks
- `internal/source/` - What the tracker takes: windows opening and closing, from any source
- `internal/state/` - Active session persistence for restart recovery
- `internal/stats/` - Totals, streaks, and time buckets
- `internal/steam/` - Steam's files: installed games and names (VDF), the game log, the Steam source, history import
- `internal/tracker/` - Turns windows opening and closing into recorded sessions
- `internal/utility/` - Time formatting, title sanitization, game matching

### Key patterns

- `internal/query.Filter` resolves filter arguments into a concrete time window
  once, rather than re-deriving it per session. `history`, `stats`, `heatmap`,
  `game`, and `export` all share it via `runQuery()` in `cmd/sessions.go`
- Sessions are filtered by **start time only** - a session belongs to the day it
  started on, and to the hour it started in
- `internal/tracker` holds no package state and takes `now` as a parameter, so
  the recovery paths are testable without Hyprland or a real clock. It takes
  `source.Window`s and `source.Event`s; `cmd/root.go` picks the source
  (`settings.source`: `auto` is Hyprland when `HYPRLAND_INSTANCE_SIGNATURE`
  is set, otherwise Steam) and feeds its events to `Tracker.Apply`
- **Naming** (`Tracker.gameName`): a config entry for exactly the window's
  class with a name wins; then the source's proper name (Steam's name for the
  app, which the Hyprland source looks up for `steam_app_<id>` windows when
  Steam is installed, so a game is named the same on the laptop and the Deck);
  then the title for `use_title` entries; then the entry's name. `MatchGame`
  prefers an exact class over a prefix, wherever each sits in the list
- **Sleep isn't play.** CLOCK_BOOTTIME minus CLOCK_MONOTONIC is the total time
  suspended since boot (`sleepclock`); a session stores it at its start plus
  the boot ID, and its played time is wall-clock time less the growth. The
  two clocks aren't read at the same instant, so readings jitter by
  microseconds while awake; growth under a second isn't sleep
  (`sleepclock.Shortest`). All durations use wall-clock times (`Round(0)`):
  Go's monotonic readings already leave out suspend on Linux, which would
  subtract sleep twice. The state file
  keeps the sleep counted so far, so restarts lose none; a session from an
  earlier boot is always ended, never resumed. `duration_seconds` is time
  played, so it can be shorter than end minus start. The minimum session
  length compares against time played
- Shutdown **does not** record active sessions. Whether a game actually stopped
  is only knowable at the next startup, when the source's snapshot (Hyprland's
  window list, or the Steam games running) says whether it is still open;
  recording at shutdown would split one session in two
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

### The Steam source (Steam Deck)

Facts from auditing the owner's Deck (SteamOS, November 2026), which the code
relies on:

- The log is `~/.local/share/Steam/logs/gameprocess_log.txt` (`~/.steam/steam`
  links to the same folder). Steam has appended to the same file since
  August 2025, across client restarts and Game Mode/Desktop Mode switches; no
  `.previous` copy. Lines end in `\r\n`, and a few spots hold runs of NUL bytes
  that run into the next line
- `[2026-11-10 06:25:31] AppID 1127500 adding PID 18019 as a tracked process
  "…reaper SteamLaunch AppId=1127500 -- …"` marks a start (an app logs several
  PIDs; the first counts). `AppID … no longer tracking PID …, exit code 0`
  then `Remove 1127500 from running list` mark the stop, within a second of
  quitting, even after a force close. Timestamps are local time, no offset
- Each client start writes blank lines, `Client version: …` and
  `SSGL: UI mode (0->N)`. Steam writes nothing when it exits
- Non-Steam shortcuts have 64-bit IDs and never get a `Remove` line; Remote
  Play shows as its own app (202355). Neither has an appmanifest, so "only
  installed games" (`Library.Name`) leaves both out, as the owner chose
- `RunningAppID` in registry.vdf stays 0 on the Deck: useless
- Steam runs every game under `reaper SteamLaunch AppId=<id> -- …`, which lives
  until the whole game exits. The source finds running games at startup from
  `/proc` (one read), and watches each game's reaper with a pidfd, so a game
  still ends if Steam crashes and never logs it (`exitwatch.go`)
- The log is followed with fsnotify from its end at startup (`follower.go`);
  a replaced or truncated file is read from the top. Nothing polls
- Library folders (internal and the SD card) come from `libraryfolders.vdf`
  (`config/` on new clients, `steamapps/` on old); names from
  `appmanifest_<id>.acf`. A missed lookup rereads the manifests, at most every
  10 seconds
- The Deck's user services run while the deck user is logged in, which is
  always (linger is off, and doesn't need to be on). A mode switch may restart
  gametrak; startup recovery covers it
- **Two people use the Deck**, each with their own Steam account; only the
  owner's games may be recorded. With `settings.steam_account` set (account
  ID, `[U:1:…]` or SteamID64; `steam.ParseAccount`), a game counts only when
  the login name in its processes' environment (`SteamAppUser`, else
  `SteamUser`) is that account's `AccountName` in `config/loginusers.vdf`
  (matched ignoring case). A skipped game is still followed (`verdict`
  `skipped`), silently, so its later lines and its end are recognized. The
  daemon's log never prints login names: an account is named by its ID, plus
  its profile name only when that doesn't resemble the login name
  (`Account.String`). On the Deck the other account's profile name **is** her
  login name, and v0.2.2 printed it
- **A game's account can't be read at its first log line** (`Source.judge`).
  Steam logs a process the moment it forks it, when it is still a copy of the
  Steam client: no launch command line, no `SteamAppUser`. v0.2.0 read the
  environment right then and never again, and it wrote the owner's first real
  session (in Game Mode) off with "can't tell which Steam account". v0.2.1
  recorded the next one. So:
  - only a process that has become part of the launch is read (`inLaunch`:
    its command line carries `SteamLaunch AppId=<id>`);
  - until one names the account the game is `undecided` and nothing is
    reported. Each later "adding PID" line of the app looks again, which
    settles it within about a second;
  - once the reaper is up and still no process names an account, Steam's
    connection log gives a second opinion (`signedIn`: the newest line with
    an account since the client's last `Client version:` line, unless that
    line is `Log session ended`). The daemon logs when it used it;
  - if no later line comes, a one-shot timer (`settleAfter`, 5s) looks once
    more and decides with what's there. That's one look, not polling;
  - when neither source tells, the game is skipped with a warning (fail
    closed), and a game that ends undecided is never recorded;
  - the Opened event carries the time of the first line (`source.Event.At`),
    so a late decision doesn't shorten the session.
- The pidfd watch moves onto the reaper once it's up (`watchReaper`); before
  that it's on the first process Steam logged. **The reaper's exit is how a
  game normally ends here**: on the Deck it reaches gametrak a moment before
  Steam writes `Remove … from running list` (same second), and that line then
  finds the game already ended. So the exit says nothing about Steam having
  crashed (v0.2.1 logged "exited without Steam logging it" on every quit)
- **Test launches the way Steam does them**: write the "adding PID" line
  while the process is still something else, then let it become the wrapper
  (`launch_test.go`, and the fake-Deck script's processes that `sleep` before
  they `exec`). A stand-in that already is the reaper when its line is written
  hides exactly the bug above
- Tested on the Deck in Desktop Mode (November 2026), online and offline, a
  Proton game and a native one, both accounts, each read about 30 seconds into
  the game: `SteamAppUser` named the right account on the
  launch wrapper (the first "adding PID", comm `steam-runtime-l…`), the reaper
  and the reaper's children. `SteamUser` is set only on the wrapper and empty
  below it. The process between the wrapper and the reaper (comm
  `steam-runtime-s…`) has a launch command line but neither variable, which
  is why `judge` tries each of the game's processes. An app logs 12–20
  "adding PID" lines per launch
- Accounts switch offline, both ways: Steam ends the session and restarts
  (a restart marker in the game log) under the other account
- Game Mode launches carry `SteamAppUser` too: v0.2.1 recorded the owner's
  Game Mode session, and v0.2.2 skipped the other account's ("Not recording …
  another Steam account") the second it started, neither falling back to the
  connection log (the daemon logs when it does)
- `logs/connection_log.txt`: each client start writes `Client version:`, then
  `SetSteamID( [U:1:0] )`, then about 3 seconds later `SetSteamID( [U:1:<id>] )`,
  offline too; nearly every later line starts `[<state>, n, n] [U:1:<id>]`.
  Offline the state stays `Logged Off` (sign-ins stop at `LogOn() called; not
  connected yet`), so never wait for `Logged On`. A shutdown or account switch
  writes `Log session ended`. The file is replaced at about 4 MB
- Signals that don't work: `loginusers.vdf` has no `MostRecent` on the Deck
  and its timestamps lag a switch by ~20s; `registry.vdf` has no
  `ActiveProcess`, and `AutoLoginUser` is the next automatic sign-in, not who
  is playing (though it followed every switch in the test); `userdata/<id>`
  folders don't change when a game starts
- At startup the daemon warns when several accounts have signed in and
  `steam_account` isn't set, or when it's set to one that never has.
  `import-steam` refuses to run where more than one account has signed in,
  since the game log doesn't say whose session was whose
- Updating the binary under a running service: `curl -o` onto it fails with
  `Text file busy`; download next to it and `mv` it into place (STEAM_DECK.md)
- Deck config: `source: steam`, `steam_account: <the owner's>`,
  `min_session_mins: 5`, `notifications: false` (Game Mode has no
  notification daemon; notify-send also runs off the event loop with a
  timeout), `logbook.platform: Steam Deck`
- `import-steam` rebuilds history from the log with the same rules
  (`steam.History`), skipping sessions with no clear end, and names
  uninstalled games after their last non-tool `steamapps/common/<folder>`.
  Entries are numbered oldest first over sessions above the minimum, so
  numbers stay valid as play is added. `--ignore-entry` choices are
  remembered in `steam-import.json` in the state folder; "already recorded"
  means a session of the same app ID or name overlaps it. It refuses to run
  where gametrak watches Hyprland, whose log already has those sessions
- Testing without a Deck: a fake Steam folder (manifests, libraryfolders.vdf,
  loginusers.vdf, a log with `\r\n` lines), and real stand-in reapers (run
  them under `env -i PATH=/usr/bin:/bin SteamAppUser=<login> …` to launch them
  as an account):
  `setsid sh -c 'sleep 600; :' reaper SteamLaunch AppId=1127500 -- game`
  (the `; :` keeps sh from exec'ing sleep, which would lose the arguments)

### Logbook sync

Sessions are mirrored to the `gaming` sheet of the owner's Logbook site
(`logs.austingause.com`, the Logs-Site repo), which is behind Cloudflare Access.

- **Off by default.** `logbook.enabled` in config.yaml turns it on. The Access
  service token is in its own file (`logbook.credentials_file`, default
  `~/.config/gametrak/logbook-credentials.yaml`), which must be mode 0600; it
  never goes in config.yaml, which may live in a dotfiles repo
- **When:** the daemon syncs at startup and after every logged session (the
  tracker's `OnLogged` hook pokes a coalescing channel). **No polling**: the
  owner doesn't want it, and site changes (mostly sessions added from an iOS
  shortcut while the PC is off) can wait for the next startup or game.
  `gametrak sync` syncs now. Syncs run on their own goroutine so network calls
  never stall the event loop
- **Retries, not polling.** A daemon sync that fails for a temporary reason
  (`logbook.Temporary`: no network or DNS, 5xx, 429) is tried again after
  10s, 30s, 1m, 2m and 5m (`syncRetries`), then waits for the next trigger.
  The daemon starts at login before the network is up, so without this the
  startup sync failed on every boot. Nothing runs while syncs work
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
  keeps a session's exact seconds unless the site changed the minutes. Uploads
  name the platform from `logbook.platform` (below)
- A session the site rejects (400) is remembered in `logbook.json` and skipped
  by automatic syncs, so it isn't resent after every game; `gametrak sync` retries
- The daemon logs each new sync failure once, and sends a notification only for
  failures retrying won't fix (`logbook.Temporary` is false), e.g. a bad token
- Testing against a local Logbook (`pnpm dev` in Logs-Site, which skips Access):
  point `logbook.url` at `http://localhost:5173` in a separate config and data
  directory. Never point a test at the production site
- Uploads use `logbook.platform` (default `PC`; the Deck sets `Steam Deck`).
  Sessions the other device uploaded arrive as site sessions with no class
- `go.mod` asks for Go 1.25.6. Where that toolchain can't be downloaded, check
  with an older local Go 1.25 via a scratch copy of go.mod:
  `GOTOOLCHAIN=local go test -modfile=/tmp/go125.mod ./...` (with `go 1.25.0`
  in that copy and go.sum copied next to it as `go125.sum`). Go 1.24 can't:
  x/sys and x/text need 1.25. They're held at the last releases that don't
  need 1.26 (v0.47.0, v0.41.0), so a plain `go get -u` would raise the go line

## Pending work

- [ ] Idle detection (a game left open while the machine is awake still counts;
      time asleep is already left out)
- [ ] Playtime goals or limits with warning notifications

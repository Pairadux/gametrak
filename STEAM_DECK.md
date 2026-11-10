# gametrak on the Steam Deck

On the Steam Deck, gametrak follows the Steam client's own log of the games it
runs, since there's no Hyprland to report windows. It works the same in Game
Mode and Desktop Mode, and each session is uploaded to Logbook as soon as the
game closes, marked as played on the Steam Deck.

- **What counts:** every Steam game installed on the Deck, on internal storage
  or the SD card. Non-Steam shortcuts (Discord, emulators) and Remote Play
  streams are left out; log those with the Log Game shortcut.
- **Sleep:** time the Deck spends asleep with a game open isn't counted.
- **How it watches:** it reacts to Steam writing its log, and to the game's
  processes exiting; nothing runs on a timer.

Everything below happens in **Desktop Mode** (hold the power button, then
**Switch to Desktop**), in **Konsole**. It all lives in your home folder, so it
needs no password and survives SteamOS updates. Paste into Konsole with
**Ctrl+Shift+V**.

## 1. Install gametrak

```sh
mkdir -p ~/.local/bin
curl -fL -o ~/.local/bin/gametrak https://github.com/Pairadux/gametrak/releases/latest/download/gametrak-linux-amd64
chmod +x ~/.local/bin/gametrak
~/.local/bin/gametrak --version
```

The last line should print `gametrak version v…`. If `curl` says 404, no
release has been published yet (see [Publishing a release](#publishing-a-release)).

If plain `gametrak` isn't found later, add `~/.local/bin` to your PATH once:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

## 2. Settings

```sh
mkdir -p ~/.config/gametrak
cat > ~/.config/gametrak/config.yaml <<'EOF'
settings:
  notifications: false
  log_sessions: true
  min_session_mins: 5
  source: steam
logbook:
  enabled: true
  platform: Steam Deck
EOF
```

- `min_session_mins: 5`: sessions shorter than 5 minutes of play aren't kept.
- `notifications: false`: Game Mode has nowhere to show desktop notifications.
- `platform: Steam Deck`: spelled exactly like that, so the site accepts it.
- The site and sheet default to `https://logs.austingause.com` and `gaming`.

## 3. The Logbook service token

Use the same `logbook-gametrak` token as the laptop (Logbook's README,
"gametrak (game sessions)", explains creating it). This asks for the two values
without showing the secret or saving it in your shell history:

```sh
install -m 600 /dev/null ~/.config/gametrak/logbook-credentials.yaml
read -p "Client ID: " id; read -s -p "Client Secret: " secret; echo
printf 'client_id: %s\nclient_secret: %s\n' "$id" "$secret" > ~/.config/gametrak/logbook-credentials.yaml
unset id secret
```

Then check the connection:

```sh
gametrak sync
```

It should say `Already in step with …` or list sessions it pulled from the site
(the laptop's, say). `Cloudflare Access rejected the service token` means the
ID or secret is wrong: run the four lines above again.

## 4. Import your past Deck sessions (optional)

Steam's log goes back months. Preview what it found first; nothing changes:

```sh
gametrak import-steam --dry-run
```

Each session has a number. Sessions over 6 hours are flagged, since a Deck left
asleep mid-game shows up as one long session. Import them, leaving out any you
don't want (repeat `--ignore-entry` as often as you like):

```sh
gametrak import-steam --ignore-entry 12 --ignore-entry 40
```

They're uploaded straight away. Ignored entries are remembered, and running it
again never adds anything twice. Games uninstalled since are named after their
install folder; rename any on the site.

## 5. Run it in the background

```sh
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/gametrak.service <<'EOF'
[Unit]
Description=gametrak game time tracker

[Service]
ExecStart=%h/.local/bin/gametrak
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now gametrak
systemctl --user status gametrak --no-pager
```

`status` should say `active (running)`. It starts by itself whenever the Deck
logs you in, in Game Mode too.

## Checking on it

```sh
gametrak status                                    # what's being tracked right now
gametrak history                                   # recent sessions
journalctl --user -u gametrak -n 30 --no-pager     # what the background service logged
```

A finished game shows in the log as `Game ended: <game>` followed by
`Logbook: uploaded 1 session`.

## Updating

```sh
curl -fL -o ~/.local/bin/gametrak https://github.com/Pairadux/gametrak/releases/latest/download/gametrak-linux-amd64
systemctl --user restart gametrak
gametrak --version
```

## Removing it

```sh
systemctl --user disable --now gametrak
rm ~/.config/systemd/user/gametrak.service ~/.local/bin/gametrak
```

Your sessions stay in `~/.local/share/gametrak/` and on the site.

## Publishing a release

The download above comes from the repository's latest GitHub release. Pushing
a version tag builds one (`.github/workflows/release.yml` runs the tests, builds
`gametrak-linux-amd64` and `gametrak-linux-arm64`, and attaches them):

```sh
git tag v1.0.0 && git push origin v1.0.0
```

Or, from the website: **Releases → Draft a new release**, type a new tag such
as `v1.0.0`, and **Publish**. The binaries appear on the release a minute or
two later.

Without a release, build it on the laptop and copy it over (a USB stick works):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gametrak-linux-amd64 .
```

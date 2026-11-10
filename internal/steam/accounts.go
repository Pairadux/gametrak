package steam

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Account is a Steam account that has signed in to Steam on this machine.
type Account struct {
	ID      uint32 // the account ID, the number in [U:1:<id>]
	Login   string // the name the account signs in with
	Persona string // the name other players see
}

// steamID64Base is the 64-bit Steam ID of account ID 0, for individual
// accounts on Steam's public universe: SteamID64 = base + account ID.
const steamID64Base = 76561197960265728

// ParseAccount reads a Steam account as config.yaml gives it: the account ID
// (145131779), the same in Steam's [U:1:145131779] form, or the 17-digit
// Steam ID that Steam's account details page shows (76561198105397507).
func ParseAccount(s string) (uint32, error) {
	text := strings.TrimSpace(s)
	text = strings.TrimSuffix(strings.TrimPrefix(text, "["), "]")
	text = strings.TrimPrefix(text, "U:1:")
	n, err := strconv.ParseUint(text, 10, 64)
	switch {
	case err != nil:
	case n > 0 && n <= 1<<32-1:
		return uint32(n), nil
	case n > steamID64Base && n-steamID64Base <= 1<<32-1:
		return uint32(n - steamID64Base), nil
	}
	return 0, fmt.Errorf("steam_account %q isn't a Steam account: give the account ID (the number in [U:1:…]) or the 17-digit Steam ID", s)
}

// accountFromSteamID64 returns the account ID in a 64-bit Steam ID.
func accountFromSteamID64(s string) (uint32, bool) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n <= steamID64Base || n-steamID64Base > 1<<32-1 {
		return 0, false
	}
	return uint32(n - steamID64Base), true
}

// LoginUsersPath is where Steam lists the accounts that have signed in here.
func LoginUsersPath(root string) string {
	return filepath.Join(root, "config", "loginusers.vdf")
}

// Accounts lists the accounts that have signed in to Steam here, from
// loginusers.vdf, by account ID.
func Accounts(root string) ([]Account, error) {
	data, err := os.ReadFile(LoginUsersPath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("Steam's list of accounts (%s) doesn't exist", LoginUsersPath(root))
		}
		return nil, fmt.Errorf("can't read Steam's list of accounts: %w", err)
	}
	parsed, err := parseVDF(string(data))
	if err != nil {
		return nil, fmt.Errorf("can't read Steam's list of accounts (%s): %w", LoginUsersPath(root), err)
	}
	users := parsed.obj("users")
	var accounts []Account
	for key, value := range users {
		user, _ := value.(vdfObject)
		id, ok := accountFromSteamID64(key)
		if user == nil || !ok {
			continue
		}
		accounts = append(accounts, Account{
			ID:      id,
			Login:   strings.TrimSpace(user.str("accountname")),
			Persona: strings.TrimSpace(user.str("personaname")),
		})
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}

// String names an account for the daemon's log without its login name,
// which is half of what signs in to it.
func (a Account) String() string {
	if a.Persona == "" {
		return fmt.Sprintf("%d", a.ID)
	}
	return fmt.Sprintf("%d (%s)", a.ID, a.Persona)
}

// userVariables are the variables in a game's environment that Steam fills
// with the login name of the account that launched it. On the Deck,
// SteamAppUser is on the launch wrapper, the reaper and the game's processes,
// online and offline, for Proton and native games alike. SteamUser names the
// account only on the outermost wrapper and is empty below it.
var userVariables = []string{"SteamAppUser", "SteamUser"}

// launchUser returns the login name of the Steam account that launched a
// game, from the environment Steam gave the game's process. Steam sets it
// itself at launch, so it's there offline too. A process between the wrapper
// and the reaper has neither variable, so callers try each of the game's
// processes.
func launchUser(procDir string, pid int) (string, bool) {
	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "environ"))
	if err != nil {
		return "", false
	}
	values := map[string]string{}
	for _, entry := range strings.Split(string(data), "\x00") {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	for _, name := range userVariables {
		if user := strings.TrimSpace(values[name]); user != "" {
			return user, true
		}
	}
	return "", false
}

// inLaunch reports whether a process has become part of Steam's launch of
// appID: its command line carries "SteamLaunch AppId=<id>", as the launch
// wrapper's and the reaper's do. Steam logs a process the moment it's forked,
// and until it runs its own program it still shows the command line and the
// environment of the process that forked it, which say nothing about the game.
func inLaunch(procDir string, pid int, appID uint64) bool {
	args := processArgs(procDir, pid)
	want := "AppId=" + strconv.FormatUint(appID, 10)
	for i, arg := range args {
		// Usually separate arguments; a wrapper may also carry the whole
		// command as one.
		if arg == "SteamLaunch" && i+1 < len(args) && args[i+1] == want {
			return true
		}
		if rest, ok := cutAfter(arg, "SteamLaunch "+want); ok && (rest == "" || rest[0] == ' ') {
			return true
		}
	}
	return false
}

// cutAfter returns what follows the first sep in s.
func cutAfter(s, sep string) (string, bool) {
	_, after, found := strings.Cut(s, sep)
	return after, found
}

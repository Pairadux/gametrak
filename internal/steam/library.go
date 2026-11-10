package steam

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
)

// FindRoot returns Steam's data folder: dir if it's set, otherwise the first
// of the usual places that holds Steam's files. Symlinks are resolved, since
// ~/.steam/steam usually points at ~/.local/share/Steam.
func FindRoot(dir string) (string, error) {
	candidates := []string{dir}
	if dir == "" {
		home, _ := os.UserHomeDir()
		candidates = []string{
			filepath.Join(xdg.DataHome, "Steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".steam", "root"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		}
	}
	for _, c := range candidates {
		if !isRoot(c) {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(c); err == nil {
			return resolved, nil
		}
		return c, nil
	}
	if dir != "" {
		return "", fmt.Errorf("%s doesn't look like Steam's folder (it has no steamapps or logs folder)", dir)
	}
	return "", errors.New("Steam's folder wasn't found in ~/.local/share/Steam, ~/.steam/steam or Steam's Flatpak folder; set settings.steam_dir in config.yaml")
}

func isRoot(dir string) bool {
	for _, sub := range []string{"steamapps", "logs"} {
		if info, err := os.Stat(filepath.Join(dir, sub)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// GameLogPath is where the Steam client logs the game processes it tracks.
func GameLogPath(root string) string {
	return filepath.Join(root, "logs", "gameprocess_log.txt")
}

// AppIDFromClass returns the app ID in a Proton game's window class,
// "steam_app_<id>".
func AppIDFromClass(class string) (uint64, bool) {
	digits, ok := strings.CutPrefix(class, "steam_app_")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(digits, 10, 64)
	return id, err == nil && id > 0
}

// Library finds the names of the Steam games installed on this machine, in
// every library folder Steam knows: internal storage, an SD card, other drives.
type Library struct {
	root string
	// minReload spaces out rereading the manifests when an app isn't found,
	// so lookups for apps that will never be found (shortcuts, Remote Play)
	// don't rescan every time.
	minReload time.Duration

	mu     sync.Mutex
	names  map[uint64]string
	loaded time.Time
}

// NewLibrary reads the libraries under Steam's data folder root.
func NewLibrary(root string) *Library {
	return &Library{root: root, minReload: 10 * time.Second}
}

// Name returns an installed game's name. A game that isn't found makes the
// library reread its manifests (at most every few seconds), so a game
// installed since gametrak started is found too.
func (l *Library) Name(appID uint64) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if name, ok := l.names[appID]; ok {
		return name, true
	}
	if l.names != nil && time.Since(l.loaded) < l.minReload {
		return "", false
	}
	l.names = loadNames(libraryDirs(l.root))
	l.loaded = time.Now()
	name, ok := l.names[appID]
	return name, ok
}

// Count returns how many installed games the library knows, reading the
// manifests if it hasn't yet.
func (l *Library) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.names == nil {
		l.names = loadNames(libraryDirs(l.root))
		l.loaded = time.Now()
	}
	return len(l.names)
}

// libraryDirs lists Steam's library folders: its own, plus every one in
// libraryfolders.vdf (which newer clients keep in config/, older in steamapps/).
func libraryDirs(root string) []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
		if dir != "" && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	add(root)
	for _, file := range []string{
		filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		filepath.Join(root, "config", "libraryfolders.vdf"),
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		parsed, err := parseVDF(string(data))
		if err != nil {
			continue
		}
		for _, entry := range parsed.obj("libraryfolders") {
			switch entry := entry.(type) {
			case vdfObject: // "0" { "path" "/run/media/deck/SD1T5" ... }
				if path := entry.str("path"); filepath.IsAbs(path) {
					add(path)
				}
			case string: // older format: "1" "/path/to/library"
				if filepath.IsAbs(entry) {
					add(entry)
				}
			}
		}
	}
	return dirs
}

// loadNames reads the name of every installed app from its manifest.
func loadNames(dirs []string) map[uint64]string {
	names := map[uint64]string{}
	for _, dir := range dirs {
		manifests, _ := filepath.Glob(filepath.Join(dir, "steamapps", "appmanifest_*.acf"))
		for _, path := range manifests {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			parsed, err := parseVDF(string(data))
			if err != nil {
				continue
			}
			app := parsed.obj("appstate")
			id, err := strconv.ParseUint(app.str("appid"), 10, 64)
			name := strings.TrimSpace(app.str("name"))
			if err != nil || name == "" {
				continue
			}
			names[id] = name
		}
	}
	return names
}

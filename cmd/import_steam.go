package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/austincgause/gametrak/internal/output"
	"github.com/austincgause/gametrak/internal/session"
	"github.com/austincgause/gametrak/internal/steam"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var (
	importDryRun bool
	importIgnore []int
)

var importSteamCmd = &cobra.Command{
	Use:   "import-steam",
	Short: "Add past sessions from Steam's own game log (Steam Deck)",
	Long: `Add game sessions from before gametrak was installed, pieced together
from the log the Steam client keeps of the games it runs.

Start with --dry-run: it lists every session it found, numbered oldest first,
without changing anything. Then run it for real, adding --ignore-entry <#> for
each one to leave out (the flag can be given as many times as you like). New
play only ever adds numbers at the end, so the numbers from the preview stay
valid. Ignored entries are remembered, so a later import leaves them out too;
to bring them back, delete steam-import.json in gametrak's state folder.

Steam's log doesn't say which account played, so import-steam refuses to run
where more than one Steam account has signed in: it can't tell your sessions
from anyone else's.

It uses the same rules as live tracking: Steam games only (no non-Steam
shortcuts, no Remote Play), and only sessions at least min_session_mins long.
A session already in your log, recorded live or imported before, is skipped,
so running it twice adds nothing. Steam's log can't show whether the Deck slept
mid-game, so a past session includes any time asleep; sessions over 6 hours are
flagged so they're easy to check. Games uninstalled since are named after
their install folder; rename any on the site.

With Logbook sync on, the imported sessions are uploaded straight away.

This is for machines where gametrak follows Steam (the Steam Deck). Where it
watches Hyprland, those sessions are already in your log.`,
	Example: `  gametrak import-steam --dry-run
  gametrak import-steam --ignore-entry 12 --ignore-entry 40`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if watchesHyprland() {
			return errors.New("import-steam is for machines where gametrak follows Steam, like the Steam Deck; " +
				"here it watches Hyprland, so these sessions are already in your log")
		}
		root, err := steam.FindRoot(cfg.Settings.SteamDir)
		if err != nil {
			return err
		}
		if err := oneAccount(root); err != nil {
			return err
		}
		logPath := steam.GameLogPath(root)
		data, err := os.ReadFile(logPath)
		if err != nil {
			return fmt.Errorf("failed to read Steam's game log: %w", err)
		}

		library := steam.NewLibrary(root)
		past := steam.History(strings.Split(string(data), "\n"), time.Local, library.Name)
		existing, err := session.LoadAll(cfg.Settings.SessionsFile)
		if err != nil {
			return err
		}
		ignoredBefore, err := loadIgnored()
		if err != nil {
			return err
		}
		minimum := time.Duration(cfg.Settings.MinSessionMins) * time.Minute
		entries, err := steam.PlanImport(past, existing, minimum, importIgnore, ignoredBefore)
		if err != nil {
			return err
		}

		fmt.Printf("Steam games played, from %s\n", logPath)
		fmt.Printf("(sessions of at least %s; non-Steam shortcuts and Remote Play are left out)\n\n",
			utility.Plural(cfg.Settings.MinSessionMins, "min"))
		if len(entries) == 0 {
			fmt.Println("No past sessions found.")
			return nil
		}
		printEntries(entries)

		var toImport []steam.Entry
		recorded, ignored := 0, 0
		for _, e := range entries {
			switch {
			case e.Recorded:
				recorded++
			case e.Ignored:
				ignored++
			default:
				toImport = append(toImport, e)
			}
		}
		fmt.Printf("\n%s to import (%d already recorded, %d ignored).\n",
			utility.Plural(len(toImport), "session"), recorded, ignored)

		if importDryRun {
			if len(toImport) > 0 {
				fmt.Println("Nothing was changed. Run it again without --dry-run to import them,")
				fmt.Println("adding --ignore-entry <#> for any to leave out.")
			}
			return nil
		}
		if err := saveIgnored(entries); err != nil {
			return err
		}
		if len(toImport) == 0 {
			return nil
		}

		added, err := importEntries(toImport)
		if err != nil {
			return err
		}
		fmt.Printf("Added %s to %s.\n", utility.Plural(added, "session"), cfg.Settings.SessionsFile)

		if !cfg.Logbook.Enabled {
			fmt.Println("Logbook sync is off, so nothing was uploaded.")
			return nil
		}
		syncer, err := newSyncer(func(format string, args ...any) { fmt.Printf(format+"\n", args...) })
		if err != nil {
			return fmt.Errorf("the sessions were added, but uploading them failed: %w", err)
		}
		result, err := syncer.Sync(context.Background(), false)
		if summary := describeSync(result); summary != "" {
			fmt.Println(capitalize(summary))
		}
		if err != nil {
			return fmt.Errorf("the sessions were added, but uploading them failed (run gametrak sync to retry): %w", err)
		}
		return nil
	},
}

// oneAccount refuses an import where more than one Steam account has signed
// in, since Steam's game log doesn't say whose game each session was.
func oneAccount(root string) error {
	accounts, err := steam.Accounts(root)
	if err != nil {
		if strings.TrimSpace(cfg.Settings.SteamAccount) == "" {
			return nil // only one person uses this Steam, as far as gametrak knows
		}
		return fmt.Errorf("can't check whose games Steam's log holds: %w", err)
	}
	if len(accounts) <= 1 {
		return nil
	}
	return fmt.Errorf("%d Steam accounts have signed in here, and Steam's game log doesn't say which one played each game, "+
		"so import-steam can't tell your sessions from anyone else's; nothing was imported. "+
		"Games from here on are still recorded (only settings.steam_account's, when it's set)", len(accounts))
}

// watchesHyprland reports whether the daemon would watch Hyprland here.
func watchesHyprland() bool {
	switch strings.ToLower(strings.TrimSpace(cfg.Settings.Source)) {
	case "hyprland":
		return true
	case "steam":
		return false
	}
	return os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != ""
}

func printEntries(entries []steam.Entry) {
	table := output.NewTable(output.Right, output.Left, output.Left, output.Right, output.Left, output.Right, output.Left, output.Left).
		Gaps("  ", "  ", "  ", " ", "  ", " ", "  ")
	for _, e := range entries {
		var notes []string
		switch {
		case e.Recorded:
			notes = append(notes, "already recorded")
		case e.IgnoredBefore:
			notes = append(notes, "ignored before")
		case e.Ignored:
			notes = append(notes, "ignored")
		}
		if e.Played() > steam.LongEntry {
			notes = append(notes, "over 6 hours, check it")
		}
		cells := []string{strconv.Itoa(e.Number), e.Start.Format("2006-01-02 15:04"), e.Name}
		cells = append(cells, utility.DurationCells(e.Played())...)
		table.Row(append(cells, strings.Join(notes, ", "))...)
	}
	table.Print(os.Stdout)
}

// importEntries adds the entries to the session log, skipping any that were
// recorded since the plan was made (by the daemon, say), and returns how
// many it added.
func importEntries(entries []steam.Entry) (int, error) {
	added := 0
	err := session.Update(cfg.Settings.SessionsFile, func(sessions []models.SessionLog) ([]models.SessionLog, bool) {
		for _, e := range entries {
			if steam.Recorded(e.PastSession, sessions) {
				continue
			}
			sessions = append(sessions, e.Log())
			added++
		}
		return sessions, added > 0
	})
	return added, err
}

// ignoredFile remembers the entries --ignore-entry left out, by session key.
func ignoredFile() string {
	return filepath.Join(filepath.Dir(cfg.Settings.StateFile), "steam-import.json")
}

type ignoredState struct {
	Ignored []string `json:"ignored"`
}

func loadIgnored() (map[string]bool, error) {
	data, err := os.ReadFile(ignoredFile())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", ignoredFile(), err)
	}
	var state ignoredState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse %s (delete it to start over): %w", ignoredFile(), err)
	}
	ignored := map[string]bool{}
	for _, key := range state.Ignored {
		ignored[key] = true
	}
	return ignored, nil
}

// saveIgnored adds this run's ignored entries to the remembered ones.
func saveIgnored(entries []steam.Entry) error {
	ignored, err := loadIgnored()
	if err != nil {
		return err
	}
	if ignored == nil {
		ignored = map[string]bool{}
	}
	changed := false
	for _, e := range entries {
		if e.Ignored && !ignored[e.Key()] {
			ignored[e.Key()] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	var state ignoredState
	for key := range ignored {
		state.Ignored = append(state.Ignored, key)
	}
	sort.Strings(state.Ignored)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ignoredFile()), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	if err := os.WriteFile(ignoredFile(), append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to remember the ignored entries: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(importSteamCmd)

	importSteamCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "list the sessions found without importing anything")
	importSteamCmd.Flags().IntSliceVar(&importIgnore, "ignore-entry", nil, "leave out the entry with this number (repeatable)")
}

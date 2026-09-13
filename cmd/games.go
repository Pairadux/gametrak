package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/austincgause/gametrak/internal/config"
	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/austincgause/gametrak/internal/output"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var gamesCmd = &cobra.Command{
	Use:     "games",
	Aliases: []string{"list"},
	Short:   "List the games being watched",
	Long:    `List every game pattern in the config, how it is matched, and whether it is open right now.`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(cfg.Games) == 0 {
			fmt.Println("No games configured. Add one with 'gametrak add <class>'.")
			return nil
		}

		// Knowing what is open makes it obvious whether a pattern is correct.
		open := make(map[string]int)
		if clients, err := hyprland.Clients(); err == nil {
			for _, client := range clients {
				if game, matched := utility.MatchGame(client.Class, cfg.Games); matched {
					open[game.Class]++
				}
			}
		}

		fmt.Printf("Watching %s:\n\n", utility.Plural(len(cfg.Games), "game"))

		table := output.NewTable(output.Left, output.Left, output.Left, output.Left)
		for _, game := range cfg.Games {
			var flags []string
			if game.Prefix {
				flags = append(flags, "prefix")
			}
			if game.UseTitle {
				flags = append(flags, "use-title")
			}

			running := ""
			if count := open[game.Class]; count > 0 {
				running = fmt.Sprintf("running (%s)", utility.Plural(count, "window"))
			}
			table.Row(game.DisplayName(), game.Class, strings.Join(flags, ", "), running)
		}
		table.Print(os.Stdout)
		fmt.Println()
		return nil
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove <class|name>",
	Aliases: []string{"rm"},
	Short:   "Remove a game from the tracking list",
	Long: `Remove a game from the configuration and rebuild games.conf.

The argument matches a window class exactly, or a display name
case-insensitively. Recorded sessions are never touched.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		removed, updated, err := config.RemoveGame(args[0])
		if err != nil {
			return err
		}
		cfg = updated

		fmt.Printf("Removed game: %s (class: %s)\n", removed.DisplayName(), removed.Class)

		if err := hyprland.GenerateGamesConf(cfg.Games, cfg.Settings.HyprlandConf); err != nil {
			return fmt.Errorf("failed to regenerate games.conf: %w", err)
		}

		fmt.Printf("Regenerated %s\n", cfg.Settings.HyprlandConf)
		return nil
	},
}

var windowsCmd = &cobra.Command{
	Use:   "windows",
	Short: "List open windows and their classes",
	Long: `List the windows Hyprland currently has open, with the class each one
reports. Use this to find the class to pass to 'gametrak add'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		clients, err := hyprland.Clients()
		if err != nil {
			return err
		}
		if len(clients) == 0 {
			fmt.Println("No windows are open.")
			return nil
		}

		table := output.NewTable(output.Left, output.Left, output.Left)
		for _, client := range clients {
			marker := ""
			if _, matched := utility.MatchGame(client.Class, cfg.Games); matched {
				marker = "tracked"
			}
			table.Row(client.Class, utility.SanitizeTitle(client.Title), marker)
		}

		fmt.Printf("%s open:\n\n", utility.Plural(len(clients), "window"))
		table.Print(os.Stdout)
		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(gamesCmd, removeCmd, windowsCmd)
}

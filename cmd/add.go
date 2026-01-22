/*
Copyright © 2026 Austin Gause
*/
package cmd

import (
	"fmt"

	"github.com/austincgause/gametrak/internal/config"
	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/austincgause/gametrak/internal/models"
	"github.com/spf13/cobra"
)

var (
	gameName   string
	gamePrefix bool
)

var addCmd = &cobra.Command{
	Use:   "add <class>",
	Short: "Add a game to the tracking list",
	Long: `Add a game to the configuration by its window class.

The window class can be found by running 'hyprctl clients' while the game is open.

Examples:
  gametrak add Terraria.bin.x86_64
  gametrak add Terraria.bin.x86_64 --name "Terraria"
  gametrak add factorio --prefix --name "Factorio"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		class := args[0]

		game := models.Game{
			Class:  class,
			Name:   gameName,
			Prefix: gamePrefix,
		}

		if err := config.AddGame(game); err != nil {
			return err
		}

		fmt.Printf("Added game: %s (class: %s", game.DisplayName(), class)
		if gamePrefix {
			fmt.Print(", prefix match")
		}
		fmt.Println(")")

		// Reload config and regenerate games.conf
		if err := config.Load(&cfg); err != nil {
			return fmt.Errorf("failed to reload config: %w", err)
		}

		if err := hyprland.GenerateGamesConf(cfg.Games, cfg.Settings.HyprlandConf); err != nil {
			return fmt.Errorf("failed to regenerate games.conf: %w", err)
		}

		fmt.Printf("Regenerated %s\n", cfg.Settings.HyprlandConf)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)

	addCmd.Flags().StringVarP(&gameName, "name", "n", "", "display name for the game")
	addCmd.Flags().BoolVarP(&gamePrefix, "prefix", "p", false, "match as prefix (e.g., steam_app_ matches steam_app_12345)")
}

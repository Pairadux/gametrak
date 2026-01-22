/*
Copyright © 2026 Austin Gause
*/
package cmd

import (
	"fmt"

	"github.com/austincgause/gametrak/internal/hyprland"
	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Regenerate the Hyprland games.conf file",
	Long: `Regenerate the games.conf file containing the $game_regex variable.

This file can be sourced in your Hyprland config to use the game list
for window rules.

Example hyprland.conf:
  source = ~/.config/gametrak/games.conf

  windowrule {
    match:class = $game_regex
    workspace = 100
  }`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := hyprland.GenerateGamesConf(cfg.Games, cfg.Settings.HyprlandConf); err != nil {
			return err
		}

		fmt.Printf("Generated %s\n", cfg.Settings.HyprlandConf)
		fmt.Printf("Regex: %s\n", hyprland.BuildGameRegex(cfg.Games))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)
}

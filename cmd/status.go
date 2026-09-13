package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/austincgause/gametrak/internal/output"
	"github.com/austincgause/gametrak/internal/state"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what is being tracked right now",
	Long: `Report whether the tracker is running, which games are currently being
timed, and how much has been played today.

The --json output is stable and intended for status bars and scripts.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		current, err := state.Load(cfg.Settings.StateFile)
		if err != nil {
			return err
		}

		now := time.Now()
		today, err := playedToday(now)
		if err != nil {
			return err
		}

		// Time already banked today plus whatever is still running.
		live := today
		for _, active := range current.Sessions {
			live += now.Sub(active.Start)
		}

		if statusJSON {
			return printStatusJSON(current, today, live, now)
		}

		if current.DaemonRunning() {
			fmt.Printf("Tracker running (pid %d)\n", current.PID)
		} else {
			fmt.Println("Tracker not running")
		}

		if len(current.Sessions) == 0 {
			fmt.Println("No games being tracked.")
		} else {
			fmt.Printf("\nActive sessions:\n")
			table := output.NewTable(output.Left, output.Right, output.Left, output.Right, output.Left, output.Left).
				Gaps("  ", " ", "  ", " ", "  ")
			for _, active := range current.Sessions {
				cells := append([]string{active.Game}, utility.DurationCells(now.Sub(active.Start))...)
				table.Row(append(cells, "since "+active.Start.Format("15:04"))...)
			}
			table.Print(os.Stdout)
		}

		fmt.Printf("\nToday: %s recorded", utility.FormatDurationRounded(today))
		if live != today {
			fmt.Printf(", %s including active sessions", utility.FormatDurationRounded(live))
		}
		fmt.Println()
		return nil
	},
}

// playedToday totals the sessions already written to the log today.
func playedToday(now time.Time) (time.Duration, error) {
	q, err := runQuery([]string{"today"})
	if err != nil {
		return 0, err
	}

	var total time.Duration
	for _, s := range q.Matched {
		total += s.Duration()
	}
	return total, nil
}

func printStatusJSON(current state.State, today, live time.Duration, now time.Time) error {
	type activeJSON struct {
		Game           string    `json:"game"`
		Class          string    `json:"class"`
		Start          time.Time `json:"start"`
		ElapsedSeconds int64     `json:"elapsed_seconds"`
	}

	payload := struct {
		Running            bool         `json:"running"`
		PID                int          `json:"pid,omitempty"`
		Active             []activeJSON `json:"active"`
		TodaySeconds       int64        `json:"today_seconds"`
		TodayLiveSeconds   int64        `json:"today_live_seconds"`
		TodayFormatted     string       `json:"today_formatted"`
		TodayLiveFormatted string       `json:"today_live_formatted"`
	}{
		Running:            current.DaemonRunning(),
		PID:                current.PID,
		Active:             []activeJSON{},
		TodaySeconds:       int64(today.Seconds()),
		TodayLiveSeconds:   int64(live.Seconds()),
		TodayFormatted:     utility.FormatDurationRounded(today),
		TodayLiveFormatted: utility.FormatDurationRounded(live),
	}

	for _, active := range current.Sessions {
		payload.Active = append(payload.Active, activeJSON{
			Game:           active.Game,
			Class:          active.Class,
			Start:          active.Start,
			ElapsedSeconds: int64(now.Sub(active.Start).Seconds()),
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output status as JSON for scripts and status bars")
}

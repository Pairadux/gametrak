package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/austincgause/gametrak/internal/models"
	"github.com/spf13/cobra"
)

var (
	exportFormat string
	exportOutput string
)

var exportCmd = &cobra.Command{
	Use:   "export [filters...]",
	Short: "Export sessions as CSV or JSON",
	Long: `Write recorded sessions to stdout, or to a file with --output.

Formats: csv (default), json, jsonl.

` + filterArgsUsage,
	Example: `  gametrak export > sessions.csv
  gametrak export year --format json -o year.json
  gametrak export month deadlock`,
	RunE: func(cmd *cobra.Command, args []string) error {
		q, err := runQuery(args)
		if err != nil {
			return err
		}

		sessions := q.Matched
		sortByStart(sessions, false)

		out := io.Writer(os.Stdout)
		if exportOutput != "" {
			f, err := os.Create(exportOutput)
			if err != nil {
				return fmt.Errorf("failed to create %s: %w", exportOutput, err)
			}
			defer f.Close()
			out = f
		}

		if err := writeSessions(out, sessions, exportFormat); err != nil {
			return err
		}

		// Keep stdout clean when the export itself is going there.
		if exportOutput != "" {
			fmt.Printf("Exported %d sessions to %s\n", len(sessions), exportOutput)
		}
		return nil
	},
}

func writeSessions(w io.Writer, sessions []models.SessionLog, format string) error {
	switch format {
	case "csv":
		return writeCSV(w, sessions)
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if sessions == nil {
			sessions = []models.SessionLog{}
		}
		return encoder.Encode(sessions)
	case "jsonl":
		encoder := json.NewEncoder(w)
		for _, s := range sessions {
			if err := encoder.Encode(s); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown format %q (want csv, json, or jsonl)", format)
	}
}

func writeCSV(w io.Writer, sessions []models.SessionLog) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	if err := writer.Write([]string{"game", "class", "start", "end", "duration_seconds"}); err != nil {
		return err
	}
	for _, s := range sessions {
		row := []string{s.Game, s.Class, s.Start, s.End, strconv.FormatInt(s.DurationSeconds, 10)}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	writer.Flush()
	return writer.Error()
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "csv", "output format: csv, json, or jsonl")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "write to a file instead of stdout")
}

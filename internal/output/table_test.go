package output

import (
	"testing"
	"time"
)

func TestTableAlignsColumns(t *testing.T) {
	table := NewTable(Left, Right)
	table.Row("Deadlock", "5")
	table.Row("RimWorld", "120")

	want := "  Deadlock    5\n  RimWorld  120\n"
	if got := table.String(); got != want {
		t.Errorf("table =\n%q\nwant\n%q", got, want)
	}
}

func TestTableDropsEmptyColumns(t *testing.T) {
	// The hour cells are empty for every row, so they should not take space.
	table := NewTable(Left, Right, Left, Right, Left)
	table.Row("Deadlock", "", "", "30", "mins")
	table.Row("RimWorld", "", "", "5", "mins")

	want := "  Deadlock  30  mins\n  RimWorld   5  mins\n"
	if got := table.String(); got != want {
		t.Errorf("table =\n%q\nwant\n%q", got, want)
	}
}

func TestTableGapsSurviveDroppedColumns(t *testing.T) {
	table := NewTable(Left, Right, Left, Right, Left).Gaps("  ", " ", "  ", " ")
	table.Row("Deadlock", "", "", "30", "mins")

	want := "  Deadlock  30 mins\n"
	if got := table.String(); got != want {
		t.Errorf("table =\n%q\nwant\n%q", got, want)
	}
}

func TestTableMeasuresWidthInRunes(t *testing.T) {
	// "Ōkami" is 5 runes but 6 bytes; padding must follow the rendered width.
	table := NewTable(Left, Left)
	table.Row("Ōkami", "x")
	table.Row("abcde", "y")

	want := "  Ōkami  x\n  abcde  y\n"
	if got := table.String(); got != want {
		t.Errorf("table =\n%q\nwant\n%q", got, want)
	}
}

func TestTableTrimsTrailingBlanks(t *testing.T) {
	table := NewTable(Left, Left)
	table.Row("Deadlock", "running")
	table.Row("RimWorld", "")

	want := "  Deadlock  running\n  RimWorld\n"
	if got := table.String(); got != want {
		t.Errorf("table =\n%q\nwant\n%q", got, want)
	}
}

func TestTableEmpty(t *testing.T) {
	table := NewTable(Left)
	if !table.Empty() {
		t.Error("a new table should be empty")
	}
	if got := table.String(); got != "" {
		t.Errorf("empty table rendered %q", got)
	}

	table.Row("x")
	if table.Empty() {
		t.Error("table with a row should not report empty")
	}
}

func TestBar(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		max   float64
		want  string
	}{
		{"full", 10, 10, "██████████"},
		{"half", 5, 10, "█████░░░░░"},
		{"empty", 0, 10, "░░░░░░░░░░"},
		{"tiny values still show", 0.01, 10, "█░░░░░░░░░"},
		{"no maximum", 0, 0, "░░░░░░░░░░"},
		{"value above maximum is clamped", 20, 10, "██████████"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Bar(tc.value, tc.max, 10); got != tc.want {
				t.Errorf("Bar(%v, %v, 10) = %q, want %q", tc.value, tc.max, got, tc.want)
			}
		})
	}

	if got := Bar(1, 1, 0); got != "" {
		t.Errorf("Bar with zero width = %q, want empty", got)
	}
}

func TestBarScalesDurations(t *testing.T) {
	got := Bar((30 * time.Minute).Seconds(), time.Hour.Seconds(), 4)
	if got != "██░░" {
		t.Errorf("Bar = %q, want %q", got, "██░░")
	}
}

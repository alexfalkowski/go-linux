//go:build linux

package display

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/alexfalkowski/go-linux/examples/top/internal/stats"
)

// Options controls process ordering and frame length.
type Options struct {
	// Sort accepts cpu, memory, or pid.
	Sort string
	// Limit is the positive maximum number of displayed processes.
	Limit int
}

// New borrows output and uses CLI-validated display options.
func New(output io.Writer, options Options) *Table {
	return &Table{output: output, order: options.Sort, limit: options.Limit}
}

// Table renders complete snapshots as ordinary, safely quoted text.
type Table struct {
	output io.Writer
	order  string
	limit  int
}

// Write orders the snapshot's owned rows and writes one complete frame.
func (table *Table) Write(frame stats.Snapshot) error {
	rows, memory := frame.Processes, frame.Memory
	sortRows(rows, table.order)
	var text strings.Builder
	fmt.Fprintf(&text, "\nRAM %.1f MiB total / %.1f MiB available | %d processes | %d unreadable/disappeared\n",
		float64(memory.Total)/(1<<20), float64(memory.Available)/(1<<20), len(rows), frame.Skipped)
	writer := tabwriter.NewWriter(&text, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "PID\tSTATE\tCPU%\tRSS MiB\tTHREADS\tNAME")
	for _, row := range rows[:min(table.limit, len(rows))] {
		rss := float64(row.Stat.ResidentPages) * float64(os.Getpagesize()) / (1 << 20)
		fmt.Fprintf(writer, "%d\t%q\t%.1f\t%.1f\t%d\t%q\n", row.PID, row.Stat.State, row.CPUPercent, rss, row.Stat.Threads, row.Stat.Name)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprint(table.output, text.String())
	return err
}

func sortRows(rows []stats.Process, order string) {
	slices.SortFunc(rows, func(a, b stats.Process) int {
		switch {
		case order == "cpu" && a.CPUPercent != b.CPUPercent:
			return cmp.Compare(b.CPUPercent, a.CPUPercent)
		case order == "memory" && a.Stat.ResidentPages != b.Stat.ResidentPages:
			if a.Stat.ResidentPages > b.Stat.ResidentPages {
				return -1
			}
			return 1
		default:
			return a.PID - b.PID
		}
	})
}

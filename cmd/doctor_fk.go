package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/marcus/td/internal/db"
	"github.com/spf13/cobra"
)

// doctorFkCmd is a hidden diagnostic that reports foreign-key orphan counts
// for the current td database. Read-only: it performs only SELECT COUNT(*)
// queries and never deletes or modifies rows. Used as a preflight check
// before enabling PRAGMA foreign_keys=ON.
var doctorFkCmd = &cobra.Command{
	Use:    "fk",
	Short:  "Report orphan-row counts for each FK relation (read-only)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer database.Close()

		results, err := db.AuditForeignKeys(database.Conn())
		if err != nil {
			return fmt.Errorf("audit: %w", err)
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "RELATION\tORPHANS")
		var total int
		for _, r := range results {
			fmt.Fprintf(tw, "%s\t%d\n", r.Relation, r.Count)
			total += r.Count
		}
		fmt.Fprintf(tw, "---\t\n")
		fmt.Fprintf(tw, "total\t%d\n", total)
		return tw.Flush()
	},
}

func init() {
	doctorCmd.AddCommand(doctorFkCmd)
}

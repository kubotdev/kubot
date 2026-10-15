package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubotdev/kubot/internal/history"
	"github.com/kubotdev/kubot/internal/report"
)

func newReportCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print the full inspection as a self-contained HTML page",
		Long: "Render the deterministic report — score, findings with evidence, " +
			"workload topology, inventory tables, health history — as one HTML " +
			"file. Inline CSS, zero external requests: safe to file, mail, or " +
			"attach to a ticket. Prints to stdout (`kubot report > report.html`), " +
			"or writes UTF-8 directly with -o (recommended on Windows, where " +
			"shell redirection re-encodes the bytes).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeoutFlag)
			defer cancel()

			rep, snap, err := gather(ctx)
			if err != nil {
				return err
			}

			var hist []history.Digest
			if dir, err := history.Dir(); err == nil {
				fp := history.Fingerprint(snap.Context)
				if ds, err := history.Load(dir, fp, namespaceFlag); err == nil {
					hist = ds
				}
			}
			page := report.Render(rep, snap, hist, appVersion(), time.Now())
			if output == "" {
				fmt.Fprintln(cmd.OutOrStdout(), page)
				return nil
			}
			if err := os.WriteFile(output, []byte(page), 0o644); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d bytes, UTF-8)\n", output, len(page))
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the page to a file as UTF-8 instead of stdout")
	return cmd
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubotdev/kubot/internal/history"
)

func newDiffCmd() *cobra.Command {
	var sinceStr string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "diff [--since 24h]",
		Short: "Show what changed between two stored runs (offline)",
		Long: "Compare the newest stored run against the nearest run older than " +
			"--since (default 24h): resolved findings, new findings, and workload " +
			"image changes. Fully offline — no cluster connection. History is " +
			"written by every inspect-family run; without two runs there is " +
			"nothing to compare.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			since, err := time.ParseDuration(sinceStr)
			if err != nil || since <= 0 {
				return usageErrf("--since must be a positive duration like 24h, got %q", sinceStr)
			}
			ctxName := currentContextName()
			if ctxName == "" {
				return usageErrf("no current context (pass --context)")
			}
			root, err := history.Dir()
			if err != nil {
				return err
			}
			ds, err := history.Load(root, history.Fingerprint(ctxName), namespaceFlag)
			if err != nil {
				return err
			}
			older, newer, elapsed, err := history.SelectPair(ds, since)
			if err != nil {
				return err
			}
			res := classifyDiff(older, newer)
			out := cmd.OutOrStdout()
			if asJSON {
				raw, err := json.MarshalIndent(res.report(ctxName, namespaceFlag, older, newer, elapsed), "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(raw))
				return nil
			}
			printDiffText(out, ctxName, namespaceFlag, older, newer, elapsed, since, res)
			return nil
		},
	}
	cmd.Flags().StringVar(&sinceStr, "since", "24h", "compare against the nearest run at least this old (e.g. 1h, 30m)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func currentContextName() string {
	if contextFlag != "" {
		return contextFlag
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigFlag != "" {
		rules.ExplicitPath = kubeconfigFlag
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil)
	raw, err := cfg.RawConfig()
	if err != nil {
		return ""
	}
	return raw.CurrentContext
}

type imageChange struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	OldImages []string `json:"old_images"`
	NewImages []string `json:"new_images"`
	OldRev    int      `json:"old_revision"`
	NewRev    int      `json:"new_revision"`
}

type diffResult struct {
	Resolved  []history.FindingDigest `json:"resolved"`
	New       []history.FindingDigest `json:"new"`
	Changed   []imageChange           `json:"changed_images"`
	Unchanged int                     `json:"unchanged"`
}

func findingKey(f history.FindingDigest) string {
	return f.Reason + "\x00" + f.Namespace + "/" + f.Resource
}

func classifyDiff(older, newer *history.Digest) diffResult {
	oldIdx := map[string]history.FindingDigest{}
	for _, f := range older.Findings {
		oldIdx[findingKey(f)] = f
	}
	newIdx := map[string]history.FindingDigest{}
	for _, f := range newer.Findings {
		newIdx[findingKey(f)] = f
	}
	var res diffResult
	for k, f := range oldIdx {
		if _, ok := newIdx[k]; ok {
			res.Unchanged++
		} else {
			res.Resolved = append(res.Resolved, f)
		}
	}
	for k, f := range newIdx {
		if _, ok := oldIdx[k]; !ok {
			res.New = append(res.New, f)
		}
	}
	oldWl := map[string]history.WorkloadDigest{}
	for _, w := range older.Workloads {
		oldWl[w.Namespace+"/"+w.Name] = w
	}
	for _, w := range newer.Workloads {
		o, ok := oldWl[w.Namespace+"/"+w.Name]
		if !ok {
			continue
		}
		if !equalStringSlices(o.Images, w.Images) {
			res.Changed = append(res.Changed, imageChange{
				Namespace: w.Namespace, Name: w.Name,
				OldImages: o.Images, NewImages: w.Images,
				OldRev: o.Revision, NewRev: w.Revision,
			})
		}
	}
	return res
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r diffResult) report(ctxName, namespace string, older, newer *history.Digest, elapsed time.Duration) map[string]any {
	scope := namespace
	if scope == "" {
		scope = "all namespaces"
	}
	return map[string]any{
		"context": ctxName, "namespace": scope,
		"older":    older.CollectedAt.UTC().Format(time.RFC3339),
		"newer":    newer.CollectedAt.UTC().Format(time.RFC3339),
		"elapsed":  elapsed.String(),
		"resolved": r.Resolved, "new": r.New,
		"changed_images": r.Changed, "unchanged": r.Unchanged,
	}
}

func printDiffText(out io.Writer, ctxName, namespace string, older, newer *history.Digest, elapsed, since time.Duration, res diffResult) {
	scope := namespace
	if scope == "" {
		scope = "all namespaces"
	}
	fmt.Fprintf(out, "diff · %s · %s\n", ctxName, scope)
	fmt.Fprintf(out, "%s → %s  ·  %s elapsed\n",
		older.CollectedAt.Local().Format("2006-01-02 15:04"),
		newer.CollectedAt.Local().Format("2006-01-02 15:04"),
		elapsed.Round(time.Minute))
	if elapsed < since*9/10 || elapsed > since*11/10 {
		fmt.Fprintf(out, "note: you asked for ~%s back, but the nearest older snapshot is %s back — comparing that.\n",
			since, elapsed.Round(time.Minute))
	}
	if len(res.Resolved) == 0 && len(res.New) == 0 && len(res.Changed) == 0 {
		fmt.Fprintf(out, "\nno changes — %d finding(s) unchanged.\n", res.Unchanged)
		return
	}
	printDigestList := func(title string, list []history.FindingDigest) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(out, "\n%s (%d)\n", title, len(list))
		for _, f := range list {
			where := f.Namespace
			if where == "" {
				where = "cluster"
			}
			fmt.Fprintf(out, "● %s (%s)\n    %s\n", f.Resource, where, f.Message)
		}
	}
	printDigestList("RESOLVED", res.Resolved)
	printDigestList("NEW", res.New)
	if len(res.Changed) > 0 {
		fmt.Fprintf(out, "\nCHANGED IMAGES (%d)\n", len(res.Changed))
		for _, c := range res.Changed {
			fmt.Fprintf(out, "● deployment/%s (%s)\n    %s → %s\n",
				c.Name, c.Namespace,
				strings.Join(c.OldImages, ", "), strings.Join(c.NewImages, ", "))
		}
	}
	fmt.Fprintf(out, "\n%d unchanged.\n", res.Unchanged)
}

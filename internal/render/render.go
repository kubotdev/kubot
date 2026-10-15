package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kubotdev/kubot/internal/model"
)

type Options struct {
	NoColor bool
	JSON    bool
	Full    bool
	Width   int
	NoScore bool
}

type styler struct{ on bool }

func (s styler) c(code string, bold bool, text string) string {
	if !s.on {
		return text
	}
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(code))
	if bold {
		st = st.Bold(true)
	}
	return st.Render(text)
}

func (s styler) dim(text string) string  { return s.c("243", false, text) }
func (s styler) head(text string) string { return s.c("110", true, text) }
func (s styler) crit(text string) string { return s.c("203", true, text) }
func (s styler) warn(text string) string { return s.c("216", true, text) }
func (s styler) good(text string) string { return s.c("114", false, text) }

func UseColor(noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func PrintReport(w io.Writer, rep model.Report, opt Options) error {
	if opt.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	return PrintText(w, rep, UseColor(opt.NoColor), opt)
}

func PrintText(w io.Writer, rep model.Report, color bool, opt Options) error {
	st := styler{on: color}
	width := opt.Width
	if width < 80 {
		width = 80
	}

	scope := "all namespaces"
	if rep.Cluster.Namespace != "" {
		scope = "namespace " + rep.Cluster.Namespace
	}
	context := rep.Cluster.Context
	if context == "" {
		context = "cluster"
	}
	fmt.Fprintf(w, "%s · %s · %s · %s\n\n",
		st.good("connected"), st.head(context), scope, st.dim("read-only"))

	for _, d := range rep.Degraded {
		for _, l := range wrap("unavailable: "+d, width) {
			fmt.Fprintf(w, "%s\n", st.warn(l))
		}
	}
	if len(rep.Degraded) > 0 {
		fmt.Fprintln(w)
	}

	score := HealthScore(rep.Issues)
	paint := st.good
	switch {
	case score < 70:
		paint = st.crit
	case score < 90:
		paint = st.warn
	}
	if !opt.NoScore {
		label := "Cluster health:"
		if rep.Cluster.Namespace != "" {
			label = "Namespace health:"
		}
		fmt.Fprintf(w, "%s %s\n\n", st.dim(label), paint(fmt.Sprintf("%d/100", score)))
	}

	if len(rep.Issues) == 0 {
		fmt.Fprintf(w, "  %s\n\n", st.good("OK — no problems detected."))
	} else {
		emit := func(header string, paint func(string) string, fs []model.Finding) {
			if len(fs) == 0 {
				return
			}
			fmt.Fprintln(w, paint(header))
			for _, f := range fs {
				if opt.Full {
					printFinding(w, f, st, width)
					continue
				}
				name := st.head(f.Resource)
				if f.Namespace != "" {
					name += " " + st.dim("("+f.Namespace+")")
				}
				if f.Suppressed {
					name += " " + st.dim("(muted: "+f.SuppressionReason+")")
				}
				fmt.Fprintf(w, "%s %s\n", paint("●"), name)
				for _, l := range wrap(f.Message, width-4) {
					fmt.Fprintf(w, "    %s\n", l)
				}
			}
			fmt.Fprintln(w)
		}
		emit("CRITICAL", st.crit, filterSev(rep.Issues, model.SeverityCritical))
		emit("WARNING", st.warn, filterSev(rep.Issues, model.SeverityWarning))
		emit("NOTE", st.dim, filterSev(rep.Issues, model.SeverityNote))
	}

	if len(rep.Checked) > 0 {
		cp := append([]string{}, rep.Checked...)
		sort.Strings(cp)
		fmt.Fprintf(w, "%s\n", st.dim("checked · "+strings.Join(cp, " · ")))
	}
	fmt.Fprintln(w)
	if len(rep.Issues) > 0 {
		detail := "kubot inspect --full"
		if !opt.Full {
			fmt.Fprintln(w, st.dim("Details: "+detail+"   ·   Machine-readable: kubot inspect --json"))
		}
	}
	return nil
}

func filterSev(fs []model.Finding, sev string) []model.Finding {
	var out []model.Finding
	for _, f := range fs {
		if f.Severity == sev {
			out = append(out, f)
		}
	}
	return out
}

func HealthScore(fs []model.Finding) int {
	penalty := 0
	for _, f := range fs {
		if f.Suppressed {
			continue
		}
		switch f.Severity {
		case model.SeverityCritical:
			penalty += 10
		case model.SeverityWarning:
			penalty += 3
		default:
			penalty += 1
		}
	}
	if s := 100 - penalty; s > 0 {
		return s
	}
	return 0
}

func wrap(text string, width int) []string {
	if width < 20 {
		width = 20
	}
	var lines []string
	cur := ""
	for _, w := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) > width:
			lines = append(lines, cur)
			cur = w
		default:
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func printFinding(w io.Writer, f model.Finding, st styler, width int) {
	res := f.Resource
	if f.Namespace != "" {
		res = fmt.Sprintf("%s %s", f.Resource, st.dim("("+f.Namespace+")"))
	}
	if f.Suppressed {
		res += " " + st.dim("(muted: "+f.SuppressionReason+")")
	}
	fmt.Fprintf(w, "  %s\n", st.head(res))
	for _, l := range wrap(f.Message, width-4) {
		fmt.Fprintf(w, "    %s\n", l)
	}
	if len(f.Evidence) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "    "+st.dim("Evidence:"))
		keys := make([]string, 0, len(f.Evidence))
		for k := range f.Evidence {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "      %s %s: %v\n", st.dim("·"), k, f.Evidence[k])
		}
	}
	if f.Recommendation != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "    "+st.dim("Recommendation:"))
		for _, l := range wrap(f.Recommendation, width-6) {
			fmt.Fprintf(w, "      %s\n", l)
		}
	}
	fmt.Fprintln(w)
}

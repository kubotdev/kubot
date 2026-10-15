package report

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/kubotdev/kubot/internal/history"
	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
	"github.com/kubotdev/kubot/internal/render"
)

type page struct{ b strings.Builder }

func (p *page) w(format string, a ...any) {
	fmt.Fprintf(&p.b, format, a...)
}
func (p *page) raw(s string) {
	p.b.WriteString(s)
}
func esc(s string) string {
	return html.EscapeString(s)
}

func asciiSafe(s string) string {
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "&#x%X;", r)
		}
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}

func (p *page) section(id, title string) {
	p.w(`<section id="%s"><h2>%s</h2>`, esc(id), esc(title))
}

func (p *page) table(headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	p.raw(`<div class="tblwrap"><table><thead><tr>`)
	for _, h := range headers {
		p.w(`<th>%s</th>`, esc(h))
	}
	p.raw(`</tr></thead><tbody>`)
	for _, r := range rows {
		p.raw(`<tr>`)
		for _, c := range r {
			p.w(`<td>%s</td>`, esc(c))
		}
		p.raw(`</tr>`)
	}
	p.raw(`</tbody></table></div>`)
}

func evText(v any) string {
	switch t := v.(type) {
	case []string:
		return strings.Join(t, ", ")
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, fmt.Sprintf("%v", e))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func Render(rep model.Report, snap *k8s.Snapshot, hist []history.Digest, version string, now time.Time) string {
	p := &page{}
	nav := []string{}
	addNav := func(id, label string) { nav = append(nav, fmt.Sprintf(`<a href="#%s">%s</a>`, esc(id), esc(label))) }
	body := &page{}

	scope := rep.Cluster.Namespace
	scopeLabel := "all namespaces"
	if scope != "" {
		scopeLabel = "namespace " + scope
	}
	score := render.HealthScore(rep.Issues)
	scoreCls := "good"
	switch {
	case score < 70:
		scoreCls = "crit"
	case score < 90:
		scoreCls = "warn"
	}

	// Findings
	addNav("findings", "Findings")
	body.section("findings", "Findings")
	if len(rep.Issues) == 0 {
		body.raw(`<p class="ok">OK — no problems detected.</p>`)
	}
	for _, sev := range []string{model.SeverityCritical, model.SeverityWarning, model.SeverityNote} {
		for _, f := range rep.Issues {
			if f.Severity != sev {
				continue
			}
			cls := sev
			if f.Suppressed {
				cls += " suppressed"
			}
			res := f.Resource
			if f.Namespace != "" {
				res += " (" + f.Namespace + ")"
			}
			body.w(`<div class="finding %s"><div class="sev">%s</div><div class="fbody"><b>%s</b> <span class="rid">%s</span>`,
				esc(cls), esc(strings.ToUpper(sev)), esc(res), esc(f.Reason))
			body.w(`<p>%s</p>`, esc(f.Message))
			if len(f.Evidence) > 0 {
				keys := make([]string, 0, len(f.Evidence))
				for k := range f.Evidence {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					body.w(`<div class="evi">%s: %s</div>`, esc(k), esc(evText(f.Evidence[k])))
				}
			}
			if f.Recommendation != "" {
				body.w(`<p class="fix">%s</p>`, esc(f.Recommendation))
			}
			if f.Suppressed {
				body.w(`<p class="caveat">muted: %s (visible, never moves exit codes)</p>`, esc(f.SuppressionReason))
			}
			body.raw(`</div></div>`)
		}
	}
	body.raw(`</section>`)

	// Topology
	if topo := buildTopo(snap, rep); len(topo.ingress)+len(topo.svc)+len(topo.work) > 0 {
		addNav("topology", "Topology")
		body.section("topology", "Topology — ingress → service → workload")
		body.raw(renderTopo(topo))
		body.raw(`</section>`)
	}

	// Workloads
	if rows := workloadRows(snap); len(rows) > 0 {
		addNav("workloads", "Workloads")
		body.section("workloads", "Workloads")
		body.table([]string{"kind", "namespace", "name", "ready", "images"}, rows)
		body.raw(`</section>`)
	}

	// Services
	if rows := serviceRows(snap); len(rows) > 0 {
		addNav("services", "Services")
		body.section("services", "Services")
		body.table([]string{"namespace", "name", "selector", "ready endpoints"}, rows)
		body.raw(`</section>`)
	}

	// History
	if svg := historyStrip(hist); svg != "" {
		addNav("history", "History")
		body.section("history", "Health over time (stored runs, same scope)")
		body.raw(svg)
		body.raw(`</section>`)
	}

	degraded := ""
	if len(rep.Degraded) > 0 {
		var rows [][]string
		for _, d := range rep.Degraded {
			rows = append(rows, []string{d})
		}
		dp := &page{}
		dp.table([]string{"unavailable collectors (partial data)"}, rows)
		degraded = `<div class="degraded">` + dp.b.String() + `</div>`
	}
	checked := ""
	if len(rep.Checked) > 0 {
		cp := append([]string{}, rep.Checked...)
		sort.Strings(cp)
		checked = esc("checked · " + strings.Join(cp, " · "))
	}
	p.raw(`<!doctype html><html><head><meta charset="utf-8">
<title>` + esc(rep.Cluster.Context) + ` — kubot report</title>
<style>
  :root{--bg:#0b0d0f;--panel:#14171b;--line:#262c33;--fg:#dfe4ea;--muted:#8b95a3;--dim:#5a6472;
        --accent:#6aa8ff;--green:#5ecf9a;--orange:#e8a75a;--red:#e06c6c}
  body{margin:0;background:var(--bg);color:var(--fg);
       font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:14px;line-height:1.5}
  header{padding:1.4rem 2rem;border-bottom:1px solid var(--line)}
  header h1{margin:0;font-size:1.2rem}
  header .meta{color:var(--muted);margin-top:.3rem}
  header .score{float:right;font-size:2rem;font-weight:700}
  header .score.good{color:var(--green)} header .score.warn{color:var(--orange)} header .score.crit{color:var(--red)}
  nav{position:sticky;top:0;background:var(--bg);border-bottom:1px solid var(--line);
      padding:.5rem 2rem;display:flex;gap:1.2rem;flex-wrap:wrap;z-index:2}
  nav a{color:var(--accent);text-decoration:none;font-size:.85rem}
  main{padding:1rem 2rem 4rem;max-width:1600px;margin:0 auto}
  h2{font-size:1rem;color:var(--accent);border-bottom:1px solid var(--line);padding-bottom:.4rem;margin-top:2.2rem}
  .tblwrap{overflow-x:auto}
  table{border-collapse:collapse;width:100%;font-size:.82rem}
  th{text-align:left;color:var(--dim);font-weight:600;padding:.35rem .7rem .35rem 0;cursor:pointer;
     border-bottom:1px solid var(--line);white-space:nowrap}
  td{padding:.3rem .7rem .3rem 0;border-bottom:1px solid #1a1f24;vertical-align:top;
     font-variant-numeric:tabular-nums}
  .finding{display:flex;gap:1rem;border:1px solid var(--line);border-radius:6px;
           background:var(--panel);padding:.8rem 1rem;margin:.6rem 0}
  .finding .sev{font-weight:700;font-size:.72rem;letter-spacing:.08em;min-width:5.5rem}
  .finding.critical .sev{color:var(--red)} .finding.warning .sev{color:var(--orange)}
  .finding.note .sev{color:var(--muted)} .finding.suppressed{opacity:.55}
  .finding .rid{color:var(--dim);font-size:.75rem}
  .fbody p{margin:.3rem 0;color:var(--muted)} .fbody b{color:var(--fg)}
  .fbody .ok{color:var(--green)}
  .evi{color:var(--muted);font-size:.82rem;padding-left:.8rem}
  .fix{color:var(--green)} .caveat{color:var(--orange);font-size:.82rem}
  p.ok{color:var(--green);font-weight:700}
  .degraded{border:1px solid var(--orange);border-radius:6px;padding:.6rem 1rem;margin:1rem 2rem 0}
  .checked{color:var(--dim);padding:.8rem 2rem 0;font-size:.85rem}
  svg.topo text{font-family:inherit}
  footer{padding:1rem 2rem;color:var(--dim);border-top:1px solid var(--line);font-size:.8rem}
</style></head><body>
<header><span class="score ` + scoreCls + `">` + fmt.Sprintf("%d", score) + `<span style="font-size:.9rem;color:var(--dim)">/100</span></span>
<h1>` + esc(rep.Cluster.Context) + ` — kubot report</h1>
<div class="meta">` + esc(scopeLabel) + ` · status ` + esc(rep.Status) + ` · ` + esc(now.UTC().Format("2006-01-02 15:04 UTC")) + `</div>
</header>
<nav>` + strings.Join(nav, "") + `</nav>
` + degraded + `
<main>`)
	p.raw(body.b.String())
	p.raw(`</main>
<div class="checked">` + checked + `</div>
<footer>generated by kubot ` + esc(version) + ` · deterministic findings-first report · self-contained file, no external requests</footer>
<script>
document.querySelectorAll('th').forEach(function(th){
  th.addEventListener('click',function(){
    var tb=th.closest('table').tBodies[0],i=th.cellIndex,asc=th.asc=!th.asc;
    Array.prototype.slice.call(tb.rows).sort(function(a,b){
      var x=a.cells[i].textContent,y=b.cells[i].textContent,
          nx=parseFloat(x.replace(/[^0-9.\-]/g,'')),ny=parseFloat(y.replace(/[^0-9.\-]/g,''));
      var c=(!isNaN(nx)&&!isNaN(ny))?nx-ny:x.localeCompare(y);
      return asc?c:-c;
    }).forEach(function(r){tb.appendChild(r)});
  });
});
</script></body></html>
`)
	return asciiSafe(p.b.String())
}

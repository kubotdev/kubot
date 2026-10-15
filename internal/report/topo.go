package report

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubotdev/kubot/internal/history"
	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
	"github.com/kubotdev/kubot/internal/render"
)

const topoCap = 25 // nodes per column; beyond that the diagram notes truncation

type topoNode struct {
	key   string
	label string
	sub   string
	bad   bool
}

type topo struct {
	ingress []topoNode
	svc     []topoNode
	work    []topoNode
	i2s     map[string][]string // ingress key -> service keys
	s2w     map[string][]string // service key -> workload keys
	capped  bool
}

func buildTopo(snap *k8s.Snapshot, rep model.Report) topo {
	t := topo{i2s: map[string][]string{}, s2w: map[string][]string{}}
	bad := map[string]bool{}
	for _, f := range rep.Issues {
		if !f.Suppressed {
			bad[f.Resource] = true
		}
	}
	isBad := func(kind, ns, name string) bool {
		return bad[kind+"/"+name]
	}
	// Workloads: deployments, statefulsets, daemonsets.
	workKey := map[string]string{}
	addWork := func(kind, ns, name, sub string) {
		key := kind + "/" + ns + "/" + name
		if _, ok := workKey[ns+"/"+name]; ok {
			return
		}
		workKey[ns+"/"+name] = key
		t.work = append(t.work, topoNode{key: key, label: kind + "/" + name, sub: ns + " · " + sub, bad: isBad(kind, ns, name)})
	}
	for _, d := range snap.Deployments {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		addWork("deployment", d.Namespace, d.Name, fmt.Sprintf("%d/%d ready", d.Status.ReadyReplicas, desired))
	}
	for _, st := range snap.StatefulSets {
		desired := int32(1)
		if st.Spec.Replicas != nil {
			desired = *st.Spec.Replicas
		}
		addWork("statefulset", st.Namespace, st.Name, fmt.Sprintf("%d/%d ready", st.Status.ReadyReplicas, desired))
	}
	for _, ds := range snap.DaemonSets {
		addWork("daemonset", ds.Namespace, ds.Name, fmt.Sprintf("%d ready", ds.Status.NumberReady))
	}
	// Services: edge to workloads whose pods match the selector.
	ownerOf := map[string]string{}
	for _, p := range snap.Pods {
		ownerOf[p.Namespace+"/"+p.Name] = k8s.TopOwnerName(&p)
	}
	for _, svc := range snap.Services {
		key := "service/" + svc.Namespace + "/" + svc.Name
		ready, total := snap.ReadyEndpoints(svc.Namespace, svc.Name)
		t.svc = append(t.svc, topoNode{key: key, label: "service/" + svc.Name,
			sub: fmt.Sprintf("%s · %d/%d endpoints", svc.Namespace, ready, total), bad: isBad("service", svc.Namespace, svc.Name)})
		if len(svc.Spec.Selector) == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, p := range snap.Pods {
			if p.Namespace != svc.Namespace {
				continue
			}
			match := true
			for k, v := range svc.Spec.Selector {
				if p.Labels[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if wk, ok := workKey[p.Namespace+"/"+ownerOf[p.Namespace+"/"+p.Name]]; ok && !seen[wk] {
				seen[wk] = true
				t.s2w["service/"+svc.Namespace+"/"+svc.Name] = append(t.s2w["service/"+svc.Namespace+"/"+svc.Name], wk)
			}
		}
	}
	// Ingresses: edges to backend services.
	svcByName := map[string]string{}
	for _, svc := range snap.Services {
		svcByName[svc.Namespace+"/"+svc.Name] = "service/" + svc.Namespace + "/" + svc.Name
	}
	for _, ing := range snap.Ingresses {
		key := "ingress/" + ing.Namespace + "/" + ing.Name
		host := ""
		if len(ing.Spec.Rules) > 0 {
			host = ing.Spec.Rules[0].Host
		}
		sub := ing.Namespace
		if host != "" {
			sub += " · " + host
		}
		t.ingress = append(t.ingress, topoNode{key: key, label: "ingress/" + ing.Name, sub: sub, bad: isBad("ingress", ing.Namespace, ing.Name)})
		addBe := func(ns, name string) {
			if sk, ok := svcByName[ns+"/"+name]; ok {
				t.i2s["ingress/"+ing.Namespace+"/"+ing.Name] = append(t.i2s["ingress/"+ing.Namespace+"/"+ing.Name], sk)
			}
		}
		if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
			addBe(ing.Namespace, ing.Spec.DefaultBackend.Service.Name)
		}
		for _, r := range ing.Spec.Rules {
			if r.HTTP == nil {
				continue
			}
			for _, path := range r.HTTP.Paths {
				if path.Backend.Service != nil {
					addBe(ing.Namespace, path.Backend.Service.Name)
				}
			}
		}
	}
	if len(t.ingress) > topoCap || len(t.svc) > topoCap || len(t.work) > topoCap {
		t.capped = true
		t.ingress = firstNodes(t.ingress)
		t.svc = firstNodes(t.svc)
		t.work = firstNodes(t.work)
	}
	return t
}

func firstNodes(ns []topoNode) []topoNode {
	if len(ns) > topoCap {
		return ns[:topoCap]
	}
	return ns
}

func renderTopo(t topo) string {
	const (
		nw, nh  = 300, 46
		gapY    = 58
		topPad  = 34
		colX    = 20
		colD    = 330
		colHead = 16
	)
	rows := len(t.ingress)
	if len(t.svc) > rows {
		rows = len(t.svc)
	}
	if len(t.work) > rows {
		rows = len(t.work)
	}
	H := topPad + rows*gapY + 10
	W := colX*2 + colD*2 + nw
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="topo" viewBox="0 0 %d %d" width="100%%" role="img" aria-label="workload topology">`, W, H)
	cols := []struct {
		x     int
		title string
		nodes []topoNode
	}{
		{colX, "ingress", t.ingress},
		{colX + colD, "service", t.svc},
		{colX + colD*2, "workload", t.work},
	}
	pos := map[string][2]int{} // node key -> center
	for _, c := range cols {
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#5a6472" font-size="12">%s</text>`, c.x, colHead, esc(c.title))
		for i, n := range c.nodes {
			x, y := c.x, topPad+i*gapY
			stroke := "#262c33"
			if n.bad {
				stroke = "#e06c6c"
			}
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="6" fill="#14171b" stroke="%s"/>`,
				x, y, nw, nh, stroke)
			fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#dfe4ea" font-size="12">%s</text>`,
				x+10, y+19, esc(trunc(n.label, 36)))
			fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#8b95a3" font-size="11">%s</text>`,
				x+10, y+35, esc(trunc(n.sub, 40)))
			pos[n.key] = [2]int{x + nw, y + nh/2}
		}
	}
	edge := func(a, c string) {
		pa, ok1 := pos[a]
		pc, ok2 := pos[c]
		if !ok1 || !ok2 {
			return
		}
		// c column sits colD right of a: line from a's right edge to c's left.
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#5a6472" stroke-width="1.5"/>`,
			pa[0], pa[1], pc[0]-nw, pc[1])
	}
	for a, cs := range t.i2s {
		for _, c := range cs {
			edge(a, c)
		}
	}
	for a, cs := range t.s2w {
		for _, c := range cs {
			edge(a, c)
		}
	}
	b.WriteString(`</svg>`)
	if t.capped {
		fmt.Fprintf(&b, `<p class="caveat">truncated to %d nodes per column — scope with -n for the full graph.</p>`, topoCap)
	}
	return b.String()
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func workloadRows(snap *k8s.Snapshot) [][]string {
	var rows [][]string
	add := func(kind, ns, name, ready string, images []string) {
		rows = append(rows, []string{kind, ns, name, ready, strings.Join(images, ", ")})
	}
	for _, d := range snap.Deployments {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		add("deployment", d.Namespace, d.Name,
			fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desired), templateImages(d.Spec.Template.Spec.Containers))
	}
	for _, st := range snap.StatefulSets {
		desired := int32(1)
		if st.Spec.Replicas != nil {
			desired = *st.Spec.Replicas
		}
		add("statefulset", st.Namespace, st.Name,
			fmt.Sprintf("%d/%d", st.Status.ReadyReplicas, desired), templateImages(st.Spec.Template.Spec.Containers))
	}
	for _, ds := range snap.DaemonSets {
		add("daemonset", ds.Namespace, ds.Name,
			fmt.Sprintf("%d", ds.Status.NumberReady), templateImages(ds.Spec.Template.Spec.Containers))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][1] != rows[j][1] {
			return rows[i][1] < rows[j][1]
		}
		return rows[i][2] < rows[j][2]
	})
	return rows
}

func templateImages(cs []corev1.Container) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cs {
		if c.Image != "" && !seen[c.Image] {
			seen[c.Image] = true
			out = append(out, c.Image)
		}
	}
	sort.Strings(out)
	return out
}

func serviceRows(snap *k8s.Snapshot) [][]string {
	var rows [][]string
	for _, svc := range snap.Services {
		sel := []string{}
		for k, v := range svc.Spec.Selector {
			sel = append(sel, k+"="+v)
		}
		sort.Strings(sel)
		ready, total := snap.ReadyEndpoints(svc.Namespace, svc.Name)
		rows = append(rows, []string{svc.Namespace, svc.Name, strings.Join(sel, ", "),
			fmt.Sprintf("%d/%d", ready, total)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][1] < rows[j][1]
	})
	return rows
}

func historyStrip(hist []history.Digest) string {
	if len(hist) < 2 {
		return ""
	}
	if len(hist) > 30 {
		hist = hist[len(hist)-30:]
	}
	type pt struct {
		at    string
		score int
	}
	var pts []pt
	for _, d := range hist {
		var fs []model.Finding
		for _, f := range d.Findings {
			fs = append(fs, model.Finding{Severity: f.Severity, Suppressed: f.Suppressed})
		}
		pts = append(pts, pt{at: d.CollectedAt.Format("01-02 15:04"), score: render.HealthScore(fs)})
	}
	const W, H, pad = 600, 120, 8
	n := len(pts)
	var b strings.Builder
	b.WriteString(`<svg viewBox="0 0 600 120" width="100%" role="img" aria-label="health history">`)
	x := func(i int) float64 {
		if n == 1 {
			return W / 2
		}
		return pad + float64(W-2*pad)*float64(i)/float64(n-1)
	}
	y := func(s int) float64 { return float64(H-pad) - float64(H-2*pad)*float64(s)/100 }
	var line, dots strings.Builder
	for i, p := range pts {
		if i > 0 {
			fmt.Fprintf(&line, `M%.0f,%.0fL%.0f,%.0f`, x(i-1), y(pts[i-1].score), x(i), y(p.score))
		}
		col := "#5ecf9a"
		switch {
		case p.score < 70:
			col = "#e06c6c"
		case p.score < 90:
			col = "#e8a75a"
		}
		fmt.Fprintf(&dots, `<circle cx="%.0f" cy="%.0f" r="3" fill="%s"><title>%s · %d/100</title></circle>`,
			x(i), y(p.score), col, esc(p.at), p.score)
	}
	fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="#6aa8ff" stroke-width="2"/>%s`, line.String(), dots.String())
	fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#5a6472" font-size="11">%s</text>`, pad, H-0, esc(pts[0].at))
	last := pts[n-1]
	fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#5a6472" font-size="11" text-anchor="end">%s · %d/100</text>`,
		W-pad, H-0, esc(last.at), last.score)
	b.WriteString(`</svg>`)
	return b.String()
}

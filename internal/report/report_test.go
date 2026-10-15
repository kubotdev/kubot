package report

import (
	"html"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubotdev/kubot/internal/history"
	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

func reportSnap() *k8s.Snapshot {
	one := int32(1)
	mkpod := func(name string, lbls map[string]string) corev1.Pod {
		return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "production", Labels: lbls}}
	}
	return &k8s.Snapshot{
		Pods: []corev1.Pod{
			mkpod("web-abc-x1", map[string]string{"app": "web"}),
			mkpod("web-abc-x2", map[string]string{"app": "web"}),
		},
		Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "production"},
			Spec: appsv1.DeploymentSpec{Replicas: &one,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "nginx:1.28"}}}}},
			Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		}},
		Services: []corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "production"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
		}},
		Ingresses: []networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "production"},
			Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
				Host: "shop.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: "web", Port: networkingv1.ServiceBackendPort{Number: 80}}},
					}},
				}},
			}}},
		}},
	}
}

func reportRep() model.Report {
	return model.Report{
		SchemaVersion: "1.0.0",
		Cluster:       model.ClusterInfo{Context: "kind-test", Namespace: "production"},
		Status:        "warning",
		Issues: []model.Finding{{
			Severity: "warning", Resource: "service/web", Namespace: "production",
			Reason: "service_no_endpoints", Message: "Service has no ready endpoints",
			Evidence:       map[string]any{"ready": 0, "total": 2},
			Recommendation: "Fix the selector.",
		}},
		Checked: []string{"pods", "services"},
	}
}

func TestRender_sectionsAndScore(t *testing.T) {
	out := Render(reportRep(), reportSnap(), nil, "v9.9.9", time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"kind-test &#x2014; kubot report", `id="findings"`, `id="topology"`,
		`id="workloads"`, `id="services"`, "service_no_endpoints",
		"ingress/shop", "deployment/web", "shop.example.com", "nginx:1.28",
		">97<", "namespace production",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, `id="history"`) {
		t.Error("no history section without digests")
	}
}

func TestRender_escapingAndNoExternal(t *testing.T) {
	rep := reportRep()
	rep.Issues[0].Message = `xss <img src=x onerror=alert(1)> & "quoted"`
	rep.Issues[0].Evidence = map[string]any{"k": `<script>alert(2)</script>`}
	out := Render(rep, reportSnap(), nil, "v1", time.Now())
	if strings.Contains(out, "<img src=x") || strings.Contains(out, "<script>alert(2)") {
		t.Fatal("finding content was not escaped")
	}
	if !strings.Contains(out, html.EscapeString(`xss <img src=x onerror=alert(1)> & "quoted"`)) {
		t.Fatal("escaped message missing")
	}
	if strings.Count(out, "<script") != 1 {
		t.Fatalf("exactly one inline script allowed, got %d", strings.Count(out, "<script"))
	}
	for _, banned := range []string{`<img`, `<iframe`, `<link`, `src="`, `href="http`, `url(`, `@import`} {
		if strings.Contains(out, banned) {
			t.Fatalf("external reference %q forbidden", banned)
		}
	}
}

func TestRender_suppressedMark(t *testing.T) {
	rep := reportRep()
	rep.Issues[0].Suppressed = true
	rep.Issues[0].SuppressionReason = "tolerated"
	out := Render(rep, reportSnap(), nil, "v1", time.Now())
	if !strings.Contains(out, "suppressed") || !strings.Contains(out, "tolerated") {
		t.Fatal("suppressed findings must stay visible with the reason")
	}
}

func TestRender_topoTruncation(t *testing.T) {
	snap := &k8s.Snapshot{}
	for i := 0; i < 30; i++ {
		name := string(rune('a'+i/26)) + string(rune('a'+i%26))
		snap.Deployments = append(snap.Deployments, appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "d-" + name, Namespace: "production"},
		})
	}
	out := Render(reportRep(), snap, nil, "v1", time.Now())
	if !strings.Contains(out, "truncated to 25 nodes") {
		t.Fatal("oversized topology must note truncation")
	}
}

func TestRender_historyStrip(t *testing.T) {
	mk := func(min time.Time, sev string) history.Digest {
		return history.Digest{Version: 1, Namespace: "production", CollectedAt: min,
			Findings: []history.FindingDigest{{Severity: sev, Reason: "x", Resource: "pod/x"}}}
	}
	base := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	full := []history.Digest{mk(base, "critical"), mk(base.Add(time.Hour), "warning"), mk(base.Add(2*time.Hour), "note")}
	out := Render(reportRep(), reportSnap(), full, "v1", time.Now())
	if !strings.Contains(out, `id="history"`) || !strings.Contains(out, "<path") {
		t.Fatal("history strip must render path for 3 digests")
	}
}

func TestRender_asciiSafe(t *testing.T) {
	rep := reportRep()
	rep.Cluster.Context = "kind-pro d"
	rep.Issues[0].Message = "em \u2014 mid \u00b7 arrow \u2192 end"
	out := Render(rep, reportSnap(), nil, "v1", time.Now())
	for _, want := range []string{"&#x2014;", "&#xB7;", "&#x2192;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing entity %q", want)
		}
	}
	for i := 0; i < len(out); i++ {
		if out[i] >= 128 {
			t.Fatalf("non-ASCII byte at offset %d — page must be pure ASCII", i)
		}
	}
}

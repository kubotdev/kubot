package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"github.com/kubotdev/kubot/internal/model"
)

func testReport() model.Report {
	return model.Report{
		SchemaVersion: model.SchemaVersion,
		Cluster:       model.ClusterInfo{Context: "kind-test"},
		Status:        "critical",
		Issues: []model.Finding{
			{
				Severity:  model.SeverityCritical,
				Resource:  "pod/payments-api-xxx",
				Namespace: "default",
				Reason:    "pod_oom_killed",
				Message:   `Container "api" was OOMKilled (exit 137)`,
			},
			{
				Severity:  model.SeverityWarning,
				Resource:  "service/shop",
				Namespace: "default",
				Reason:    "service_no_endpoints",
				Message:   "Service selector matches no pods",
			},
		},
		Checked: []string{"pods", "services"},
	}
}

func TestPrintReport_JSONMatchesSchemaShape(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintReport(&buf, testReport(), Options{JSON: true}); err != nil {
		t.Fatalf("PrintReport: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if decoded["schema_version"] != model.SchemaVersion {
		t.Fatalf("schema_version missing: %+v", decoded)
	}
	if decoded["status"] != "critical" {
		t.Fatalf("status missing: %+v", decoded)
	}
}

func TestPrintText_compact(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintText(&buf, testReport(), false, Options{Width: 100}); err != nil {
		t.Fatalf("PrintText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"connected", "kind-test", "Cluster health:", "/100",
		"CRITICAL", "WARNING", "●",
		"pod/payments-api-xxx", "service/shop",
		"checked", "--full", "--json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compact report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Evidence:") {
		t.Errorf("compact report must not show evidence:\n%s", out)
	}
}

func TestPrintText_full(t *testing.T) {
	rep := testReport()
	rep.Issues[0].Evidence = map[string]any{"memory_limit": "64Mi"}
	rep.Issues[0].Recommendation = "Increase the limit."
	var buf bytes.Buffer
	if err := PrintText(&buf, rep, false, Options{Width: 100, Full: true}); err != nil {
		t.Fatalf("PrintText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Evidence:", "memory_limit", "64Mi", "Recommendation:", "Increase the limit."} {
		if !strings.Contains(out, want) {
			t.Errorf("full report missing %q:\n%s", want, out)
		}
	}
}

func TestHealthScore(t *testing.T) {
	if got := HealthScore(nil); got != 100 {
		t.Errorf("empty = %d, want 100", got)
	}
	fs := []model.Finding{{Severity: "critical"}, {Severity: "warning"}, {Severity: "note"}}
	if got := HealthScore(fs); got != 86 {
		t.Errorf("got %d, want 86", got)
	}
}

func TestScoreHiddenWhenScoped(t *testing.T) {
	var scoped bytes.Buffer
	if err := PrintText(&scoped, testReport(), false, Options{Width: 100, NoScore: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scoped.String(), "/100") {
		t.Errorf("scoped view must not grade:\n%s", scoped.String())
	}
	var full bytes.Buffer
	if err := PrintText(&full, testReport(), false, Options{Width: 100}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full.String(), "Cluster health:") {
		t.Errorf("full view must grade:\n%s", full.String())
	}
}

func TestDegradedAnnounced(t *testing.T) {
	rep := testReport()
	rep.Degraded = []string{"events: forbidden"}
	var buf bytes.Buffer
	if err := PrintText(&buf, rep, false, Options{Width: 100}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "unavailable: events: forbidden") {
		t.Errorf("degraded collectors must be announced:\n%s", buf.String())
	}
}

func TestScoreNamespaceLabel(t *testing.T) {

	rep := testReport()
	rep.Cluster.Namespace = "default"
	var buf bytes.Buffer
	if err := PrintText(&buf, rep, false, Options{Width: 100}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Namespace health:") {
		t.Errorf("namespaced view mislabeled:\n%s", buf.String())
	}
}

func TestResourcesTable(t *testing.T) {
	rows := []ResourceRow{
		{Pod: "default/a", Container: "app", CPUReq: "—", MemReq: "—", MemLimit: "64Mi", MemUse: "53Mi", UseRatio: 0.83, Hot: true},
		{Pod: "default/b", Container: "app", CPUReq: "—", MemReq: "—", MemLimit: "—", MemUse: "—", UseRatio: -1},
	}
	var buf bytes.Buffer
	PrintResourcesTable(&buf, rows, false, 50, 0)
	out := buf.String()
	for _, want := range []string{"POD", "CONTAINER", "USE%", "83%", "default/a", "default/b", "╭", "╰"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestEventsTable(t *testing.T) {

	rows := []EventRow{
		{Namespace: "default", Object: "Pod/crashy-x", Reason: "BackOff", Count: 41, Message: "Back-off restarting failed container"},
		{Namespace: "default", Object: "Pod/hungry", Reason: "FailedScheduling", Count: 1, Message: "0/1 nodes are available"},
	}
	var buf bytes.Buffer
	PrintEventsTable(&buf, rows, false, 20, 120)
	out := buf.String()
	for _, want := range []string{"NS", "OBJECT", "REASON", "COUNT", "MESSAGE", "x41", "BackOff", "FailedScheduling"} {
		if !strings.Contains(out, want) {
			t.Errorf("events table missing %q:\n%s", want, out)
		}
	}
	// Most repeated first.
	if strings.Index(out, "x41") > strings.Index(out, "FailedScheduling") {
		t.Errorf("events not ordered by count:\n%s", out)
	}
	// Long messages truncate with … instead of wrapping mid-word.
	long := []EventRow{{Namespace: "n", Object: "Pod/x", Reason: "R", Count: 1, Message: strings.Repeat("m", 500)}}
	buf.Reset()
	PrintEventsTable(&buf, long, false, 20, 120)
	if !strings.Contains(buf.String(), "…") {
		t.Errorf("long message should truncate:\n%s", buf.String())
	}
	// Long pod names keep head and tail, cut the middle hash.
	if got := cutMiddle("Pod/unready-56f8fc5f69-gqqm7", 24); got != "Pod/unready-56f…69-gqqm7" {
		t.Errorf("cutMiddle = %q", got)
	}
}

func TestResourcesTable_evenWidths(t *testing.T) {
	rows := []ResourceRow{
		{Pod: "default/payments-api-68ffdf658-m2wq4", Container: "api", CPUReq: "-", MemReq: "64Mi", MemLimit: "64Mi", MemUse: "-", UseRatio: -1},
		{Pod: "default/steady-74f69994db-szhdg", Container: "app", CPUReq: "-", MemReq: "64Mi", MemLimit: "64Mi", MemUse: "53Mi", UseRatio: 0.83, Hot: true},
		{Pod: "default/tiny-d967b5d8b-rkxnf", Container: "app", CPUReq: "-", MemReq: "16Mi", MemLimit: "16Mi", MemUse: "0Mi", UseRatio: 0.02},
	}
	var buf bytes.Buffer
	PrintResourcesTable(&buf, rows, false, 50, 120)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := runewidth.StringWidth(lines[0])
	for i, l := range lines[1:] {
		if got := runewidth.StringWidth(l); got != want {
			t.Errorf("line %d width %d, want %d: %q", i+1, got, want, l)
		}
	}
}

func TestSARIF(t *testing.T) {
	rep := testReport()
	rep.Issues = append(rep.Issues, model.Finding{
		Severity: model.SeverityCritical, Resource: "pod/muted",
		Reason: "test_muted", Message: "muted critical",
		Suppressed: true, SuppressionReason: "demo",
	})
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, rep, "v0.0.0-test"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name    string `json:"name"`
					Version string `json:"version"`
					Rules   []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID       string `json:"ruleId"`
				Level        string `json:"level"`
				Suppressions []struct {
					Justification string `json:"justification"`
				} `json:"suppressions"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if doc.Version != "2.1.0" || doc.Runs[0].Tool.Driver.Name != "kubot" {
		t.Fatalf("bad envelope: %+v", doc.Version)
	}
	if doc.Runs[0].Tool.Driver.Version != "v0.0.0-test" {
		t.Errorf("driver version missing")
	}
	byID := map[string]string{}
	var muted []string
	for _, r := range doc.Runs[0].Results {
		byID[r.RuleID] = r.Level
		if len(r.Suppressions) > 0 {
			muted = append(muted, r.RuleID)
		}
	}
	if byID["pod_oom_killed"] != "error" || byID["service_no_endpoints"] != "warning" {
		t.Errorf("severity levels wrong: %+v", byID)
	}
	if len(muted) != 1 || muted[0] != "test_muted" {
		t.Errorf("suppressed must carry suppression: %+v", muted)
	}
}

func TestJUnit(t *testing.T) {
	rep := testReport()
	rep.Issues = append(rep.Issues, model.Finding{
		Severity: model.SeverityCritical, Resource: "pod/muted",
		Reason: "test_muted", Message: "muted critical",
		Suppressed: true, SuppressionReason: "demo",
	})
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, rep); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`<testsuite name="kubot"`, `failures="2"`, `skipped="1"`, `classname="pod_oom_killed"`, `<skipped/>`} {
		if !strings.Contains(out, want) {
			t.Errorf("junit missing %q:\n%s", want, out)
		}
	}
}

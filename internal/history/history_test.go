package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

func testDigest(at time.Time, ns string, reasons ...string) Digest {
	d := Digest{Version: DigestVersion, Context: "kind-test", CollectedAt: at, Namespace: ns}
	for _, r := range reasons {
		d.Findings = append(d.Findings, FindingDigest{Severity: "warning", Reason: r, Resource: "pod/x", Namespace: ns})
	}
	return d
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	d := testDigest(time.Now().UTC(), "production", "pod_oom_killed")
	if err := Save(root, "kind-test", d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root, "kind-test", "production")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || len(got[0].Findings) != 1 || got[0].Findings[0].Reason != "pod_oom_killed" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestLoad_scopeIsolation(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	if err := Save(root, "fp", testDigest(now, "production", "a")); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, "fp", testDigest(now.Add(time.Second), "", "b")); err != nil {
		t.Fatal(err)
	}
	scoped, _ := Load(root, "fp", "production")
	if len(scoped) != 1 || len(scoped[0].Findings) != 1 || scoped[0].Findings[0].Reason != "a" {
		t.Fatalf("scoped load must not see unscoped runs: %+v", scoped)
	}
	if _, err := Load(root, "missing", "production"); err != nil {
		t.Fatalf("missing fingerprint must load empty, got %v", err)
	}
	if got, _ := Load(root, "fp", "other"); len(got) != 0 {
		t.Fatalf("other scope must load empty, got %+v", got)
	}
}

func TestSelectPair_windowAnchored(t *testing.T) {
	base := time.Now().UTC()
	ds := []Digest{
		testDigest(base.Add(-30*time.Hour), "", "old"),
		testDigest(base.Add(-2*time.Hour), "", "mid"),
		testDigest(base, "", "new"),
	}
	older, newer, elapsed, err := SelectPair(ds, 24*time.Hour)
	if err != nil {
		t.Fatalf("SelectPair: %v", err)
	}
	if older.Findings[0].Reason != "old" || newer.Findings[0].Reason != "new" {
		t.Fatalf("want oldest-vs-newest for 24h, got %v vs %v", older.Findings, newer.Findings)
	}
	if elapsed < 29*time.Hour || elapsed > 31*time.Hour {
		t.Fatalf("must report the actual window, got %v", elapsed)
	}
	older, _, _, err = SelectPair(ds, time.Hour)
	if err != nil {
		t.Fatalf("SelectPair: %v", err)
	}
	if older.Findings[0].Reason != "mid" {
		t.Fatalf("1h window must pick nearest-older, got %v", older.Findings)
	}
}

func TestSelectPair_needsTwo(t *testing.T) {
	if _, _, _, err := SelectPair(nil, time.Hour); err == nil {
		t.Fatal("empty history must error")
	}
	ds := []Digest{testDigest(time.Now().UTC(), "", "only")}
	if _, _, _, err := SelectPair(ds, time.Hour); err == nil {
		t.Fatal("single run must error")
	}
}

func TestFingerprint_sanitizes(t *testing.T) {
	if got := Fingerprint("arn:aws:eks:us-east-2:123:cluster/prod"); strings.ContainsAny(got, ":/") {
		t.Fatalf("must be path-safe, got %q", got)
	}
	if Fingerprint("") != "default" {
		t.Fatal("empty context needs a fallback")
	}
}

func TestPrune_caps(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fp")
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if err := Save(root, "fp", testDigest(now.Add(time.Duration(i)*time.Second), "", "r")); err != nil {
			t.Fatal(err)
		}
	}
	// Force tiny caps via direct prune after backdating one file.
	old := filepath.Join(dir, "20000101T000000Z.json")
	if err := os.WriteFile(old, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	ancient, _ := time.Parse(time.RFC3339, "2000-01-02T00:00:00Z")
	if err := os.Chtimes(old, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	if err := prune(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("ancient files must be pruned")
	}
}

func TestFromSnapshot_collectsWorkloads(t *testing.T) {
	one := int32(1)
	rep := model.Report{Issues: []model.Finding{
		{Severity: "critical", Reason: "pod_oom_killed", Resource: "pod/x", Namespace: "production", Message: "boom"},
	}}
	snap := &k8s.Snapshot{
		Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "production"},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		}},
		ReplicaSets: []appsv1.ReplicaSet{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-abc", Namespace: "production",
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": "7"},
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api"}},
			},
		}},
	}
	snap.ReplicaSets[0].Spec.Template.Spec.Containers = []corev1.Container{{Name: "api", Image: "img:v2"}}
	d := FromSnapshot(snap, rep, "kind-test", "production")
	if len(d.Findings) != 1 || len(d.Workloads) != 1 {
		t.Fatalf("must carry findings and workloads: %+v", d)
	}
	if d.Workloads[0].Revision != 7 || len(d.Workloads[0].Images) != 1 || d.Workloads[0].Images[0] != "img:v2" {
		t.Fatalf("workload must carry revision and images: %+v", d.Workloads[0])
	}
}

func TestDir_respectsEnv(t *testing.T) {
	t.Setenv("KUBOT_STATE_HOME", t.TempDir())
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, filepath.Join("kubot", "history")) {
		t.Fatalf("must end in kubot/history, got %q", dir)
	}
}

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/kubotdev/kubot/internal/history"
)

func diffDigest(ns string, reasons ...string) *history.Digest {
	d := &history.Digest{Namespace: ns, CollectedAt: time.Now().UTC()}
	for _, r := range reasons {
		d.Findings = append(d.Findings, history.FindingDigest{
			Severity: "warning", Reason: r, Resource: "pod/x", Namespace: ns, Message: r,
		})
	}
	return d
}

func TestClassifyDiff_resolvedNewUnchanged(t *testing.T) {
	older := diffDigest("production", "pod_oom_killed", "pod_pending")
	newer := diffDigest("production", "pod_pending", "pod_imagepull_backoff")
	res := classifyDiff(older, newer)
	if len(res.Resolved) != 1 || res.Resolved[0].Reason != "pod_oom_killed" {
		t.Fatalf("want oom resolved, got %+v", res.Resolved)
	}
	if len(res.New) != 1 || res.New[0].Reason != "pod_imagepull_backoff" {
		t.Fatalf("want imagepull new, got %+v", res.New)
	}
	if res.Unchanged != 1 {
		t.Fatalf("want 1 unchanged, got %d", res.Unchanged)
	}
}

func TestClassifyDiff_messageIgnored(t *testing.T) {
	older := diffDigest("production", "pod_pending")
	newer := diffDigest("production", "pod_pending")
	newer.Findings[0].Message = "reworded human text"
	res := classifyDiff(older, newer)
	if len(res.Resolved) != 0 || len(res.New) != 0 || res.Unchanged != 1 {
		t.Fatalf("message rewording must not count as change: %+v", res)
	}
}

func TestClassifyDiff_imageChange(t *testing.T) {
	wl := func(imgs ...string) history.WorkloadDigest {
		return history.WorkloadDigest{Kind: "deployment", Namespace: "production", Name: "api", Images: imgs, Revision: 1}
	}
	older := diffDigest("production")
	older.Workloads = []history.WorkloadDigest{wl("img:v1")}
	newer := diffDigest("production")
	newer.Workloads = []history.WorkloadDigest{wl("img:v2")}
	res := classifyDiff(older, newer)
	if len(res.Changed) != 1 || res.Changed[0].Name != "api" {
		t.Fatalf("want one image change, got %+v", res.Changed)
	}
	same := diffDigest("production")
	same.Workloads = []history.WorkloadDigest{wl("img:v1")}
	if res := classifyDiff(older, same); len(res.Changed) != 0 {
		t.Fatalf("same images must not flag, got %+v", res.Changed)
	}
}

func TestPrintDiffText_honestyNote(t *testing.T) {
	older := diffDigest("production", "a")
	newer := diffDigest("production", "a")
	var sb strings.Builder
	printDiffText(&sb, "kind-test", "production", older, newer, 31*time.Hour, 24*time.Hour, classifyDiff(older, newer))
	if !strings.Contains(sb.String(), "nearest older snapshot") {
		t.Fatalf("must state the actual window, got:\n%s", sb.String())
	}
	if !strings.Contains(sb.String(), "no changes") {
		t.Fatalf("empty diff must say so, got:\n%s", sb.String())
	}
}

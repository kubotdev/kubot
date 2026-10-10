package diagnose

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

type DeploymentStalledRule struct{}

func (DeploymentStalledRule) Name() string { return "deployment_rollout_stalled" }

func (DeploymentStalledRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, d := range s.Deployments {
		msg := ""
		for _, c := range d.Status.Conditions {
			if c.Type == appsv1.DeploymentProgressing && c.Reason == "ProgressDeadlineExceeded" {
				msg = c.Message
			}
		}
		if msg == "" {
			continue
		}
		desired := int32(0)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("deployment/%s", d.Name),
			Namespace: d.Namespace,
			Reason:    "deployment_rollout_stalled",
			Message:   "Rollout exceeded its progress deadline and is stuck",
			Evidence: map[string]any{
				"desired":   desired,
				"updated":   d.Status.UpdatedReplicas,
				"ready":     d.Status.ReadyReplicas,
				"condition": truncate(msg, 300),
			},
			Recommendation: "Check the new ReplicaSet's pods (usually a bad image or failing probe); rollback or fix forward, then the rollout completes.",
		})
	}
	return out
}

type ReplicaSetLeftoverRule struct{}

func (ReplicaSetLeftoverRule) Name() string { return "replicaset_leftover" }

func (ReplicaSetLeftoverRule) Run(s *k8s.Snapshot) []model.Finding {
	byDeploy := map[string][]appsv1.ReplicaSet{}
	for _, rs := range s.ReplicaSets {
		for _, o := range rs.OwnerReferences {
			if o.Kind == "Deployment" {
				key := rs.Namespace + "/" + o.Name
				byDeploy[key] = append(byDeploy[key], rs)
			}
		}
	}
	var out []model.Finding
	for key, rss := range byDeploy {
		var live []appsv1.ReplicaSet
		for _, rs := range rss {
			if rs.Spec.Replicas != nil && *rs.Spec.Replicas > 0 {
				live = append(live, rs)
			}
		}
		if len(live) < 2 {
			continue
		}
		sort.Slice(live, func(i, j int) bool {
			return live[i].CreationTimestamp.After(live[j].CreationTimestamp.Time)
		})
		old := []string{}
		for _, rs := range live[1:] {
			old = append(old, rs.Name)
		}
		ns, name := splitKey(key)
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("deployment/%s", name),
			Namespace: ns,
			Reason:    "replicaset_leftover",
			Message:   fmt.Sprintf("%d old ReplicaSet(s) still run pods beside the newest", len(old)),
			Evidence: map[string]any{
				"current":          live[0].Name,
				"old_replicasets":  old,
				"replicasets_live": len(live),
			},
			Recommendation: "If the rollout finished, the old sets should have scaled to zero — check for a stuck rollout or manual scaling.",
		})
	}
	return out
}

func splitKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}

// RolloutRecentWindow bounds the "what just changed" signal: a rollout older than this is history
const RolloutRecentWindow = time.Hour

type DeploymentRolloutRecentRule struct{}

func (DeploymentRolloutRecentRule) Name() string { return "deployment_rollout_recent" }

func (DeploymentRolloutRecentRule) Run(s *k8s.Snapshot) []model.Finding {
	byDeploy := map[string][]appsv1.ReplicaSet{}
	for _, rs := range s.ReplicaSets {
		for _, o := range rs.OwnerReferences {
			if o.Kind == "Deployment" {
				key := rs.Namespace + "/" + o.Name
				byDeploy[key] = append(byDeploy[key], rs)
			}
		}
	}
	var out []model.Finding
	for _, d := range s.Deployments {
		rss := byDeploy[d.Namespace+"/"+d.Name]
		if len(rss) == 0 {
			continue
		}
		sort.Slice(rss, func(i, j int) bool {
			if !rss[i].CreationTimestamp.Equal(&rss[j].CreationTimestamp) {
				return rss[i].CreationTimestamp.After(rss[j].CreationTimestamp.Time)
			}
			return rsRevision(rss[i]) > rsRevision(rss[j])
		})
		newest := rss[0]
		age := time.Since(newest.CreationTimestamp.Time)
		if age > RolloutRecentWindow {
			continue
		}
		images := rsImages(newest)
		ev := map[string]any{
			"replicaset": newest.Name,
			"revision":   rsRevision(newest),
			"images":     images,
			"age":        humanAge(age),
		}
		msg := fmt.Sprintf("first rollout %s ago (image %s)", humanAge(age), strings.Join(images, ", "))
		if len(rss) > 1 {
			prev := rss[1]
			prevImages := rsImages(prev)
			ev["previous_replicaset"] = prev.Name
			ev["previous_revision"] = rsRevision(prev)
			ev["previous_images"] = prevImages
			msg = fmt.Sprintf("rolled out %s ago (image %s)", humanAge(age), strings.Join(images, ", "))
			if !equalStrings(images, prevImages) {
				msg = fmt.Sprintf("rolled out %s ago (image %s → %s)",
					humanAge(age), strings.Join(prevImages, ", "), strings.Join(images, ", "))
			}
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("deployment/%s", d.Name),
			Namespace: d.Namespace,
			Reason:    "deployment_rollout_recent",
			Message:   msg,
			Evidence:  ev,
			Recommendation: fmt.Sprintf("Correlate with new symptoms: if problems started with this rollout, "+
				"`kubectl rollout undo deployment/%s -n %s`; confirm the diagnosis with `kubot why %s`.",
				d.Name, d.Namespace, d.Name),
		})
	}
	return out
}

func rsRevision(rs appsv1.ReplicaSet) int {
	n, _ := strconv.Atoi(rs.Annotations["deployment.kubernetes.io/revision"])
	return n
}

func rsImages(rs appsv1.ReplicaSet) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range rs.Spec.Template.Spec.Containers {
		if c.Image != "" && !seen[c.Image] {
			seen[c.Image] = true
			out = append(out, c.Image)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	h := m / 60
	if h < 48 {
		return fmt.Sprintf("%dh%dm", h, m%60)
	}
	return fmt.Sprintf("%dd", m/1440)
}

func equalStrings(a, b []string) bool {
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

package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

// on-disk format version. Bump when Digest changes shape;
// readers refuse newer versions rather than misread them.
const DigestVersion = 1

const KeepDays = 30
const KeepFiles = 500

type Digest struct {
	Version     int              `json:"version"`
	Context     string           `json:"context"`
	CollectedAt time.Time        `json:"collected_at"`
	Namespace   string           `json:"namespace"`
	Findings    []FindingDigest  `json:"findings"`
	Workloads   []WorkloadDigest `json:"workloads"`
}

type FindingDigest struct {
	Severity  string `json:"severity"`
	Reason    string `json:"reason"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Message   string `json:"message"`
}

type WorkloadDigest struct {
	Kind      string   `json:"kind"`
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Images    []string `json:"images"`
	Revision  int      `json:"revision"`
	Replicas  int32    `json:"replicas"`
	Ready     int32    `json:"ready"`
}

func FromSnapshot(snap *k8s.Snapshot, rep model.Report, contextName, namespace string) Digest {
	d := Digest{
		Version:     DigestVersion,
		Context:     contextName,
		CollectedAt: time.Now().UTC(),
		Namespace:   namespace,
		Findings:    []FindingDigest{},
		Workloads:   []WorkloadDigest{},
	}
	for _, f := range rep.Issues {
		d.Findings = append(d.Findings, FindingDigest{
			Severity: f.Severity, Reason: f.Reason,
			Resource: f.Resource, Namespace: f.Namespace, Message: f.Message,
		})
	}
	byDeploy := map[string][]appsv1.ReplicaSet{}
	for _, rs := range snap.ReplicaSets {
		for _, o := range rs.OwnerReferences {
			if o.Kind == "Deployment" {
				byDeploy[rs.Namespace+"/"+o.Name] = append(byDeploy[rs.Namespace+"/"+o.Name], rs)
			}
		}
	}
	for _, dep := range snap.Deployments {
		wd := WorkloadDigest{Kind: "deployment", Namespace: dep.Namespace, Name: dep.Name}
		if dep.Spec.Replicas != nil {
			wd.Replicas = *dep.Spec.Replicas
		}
		wd.Ready = dep.Status.ReadyReplicas
		rss := byDeploy[dep.Namespace+"/"+dep.Name]
		if len(rss) > 0 {
			newest := rss[0]
			for _, rs := range rss[1:] {
				if rs.CreationTimestamp.After(newest.CreationTimestamp.Time) {
					newest = rs
				}
			}
			wd.Revision, _ = strconv.Atoi(newest.Annotations["deployment.kubernetes.io/revision"])
			seen := map[string]bool{}
			for _, c := range newest.Spec.Template.Spec.Containers {
				if c.Image != "" && !seen[c.Image] {
					seen[c.Image] = true
					wd.Images = append(wd.Images, c.Image)
				}
			}
			sort.Strings(wd.Images)
		}
		if wd.Images == nil {
			wd.Images = []string{}
		}
		d.Workloads = append(d.Workloads, wd)
	}
	sort.Slice(d.Workloads, func(i, j int) bool {
		if d.Workloads[i].Namespace != d.Workloads[j].Namespace {
			return d.Workloads[i].Namespace < d.Workloads[j].Namespace
		}
		return d.Workloads[i].Name < d.Workloads[j].Name
	})
	return d
}

func Dir() (string, error) {
	base := os.Getenv("KUBOT_STATE_HOME")
	if base == "" {
		base = os.Getenv("XDG_STATE_HOME")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("history store: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "kubot", "history"), nil
}

// Maps a context name to one directory. Same context, same directory
// diff refuses to compare across fingerprints.
func Fingerprint(contextName string) string {
	var b strings.Builder
	for _, r := range contextName {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func Save(root, fingerprint string, d Digest) error {
	dir := filepath.Join(root, fingerprint)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	name := d.CollectedAt.UTC().Format("20060102T150405Z") + ".json"
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return prune(dir)
}

// Returns one fingerprint's digests in scope, oldest first.
// Scope must match the run's namespace filter
func Load(root, fingerprint, namespace string) ([]Digest, error) {
	dir := filepath.Join(root, fingerprint)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Digest
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var d Digest
		if err := json.Unmarshal(raw, &d); err != nil {
			continue
		}
		if d.Version > DigestVersion {
			continue
		}
		if d.Namespace != namespace {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CollectedAt.Before(out[j].CollectedAt) })
	return out, nil
}

// Picks the newest digest and the nearest digest older than (newest − since).
func SelectPair(ds []Digest, since time.Duration) (older, newer *Digest, elapsed time.Duration, err error) {
	if len(ds) == 0 {
		return nil, nil, 0, fmt.Errorf("no history yet — run `kubot inspect` first")
	}
	if len(ds) == 1 {
		return nil, nil, 0, fmt.Errorf("only one run stored — run `kubot inspect` again later to compare")
	}
	n := ds[len(ds)-1]
	cutoff := n.CollectedAt.Add(-since)
	var o *Digest
	for i := len(ds) - 2; i >= 0; i-- {
		if !ds[i].CollectedAt.After(cutoff) {
			o = &ds[i]
			break
		}
	}
	if o == nil {
		o = &ds[0]
	}
	return o, &n, n.CollectedAt.Sub(o.CollectedAt), nil
}

func prune(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type item struct {
		name string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{e.Name(), info.ModTime()})
	}
	cutoff := time.Now().Add(-KeepDays * 24 * time.Hour)
	var keep []item
	for _, it := range items {
		if it.mod.Before(cutoff) {
			os.Remove(filepath.Join(dir, it.name))
			continue
		}
		keep = append(keep, it)
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].mod.Before(keep[j].mod) })
	for len(keep) > KeepFiles {
		os.Remove(filepath.Join(dir, keep[0].name))
		keep = keep[1:]
	}
	return nil
}

package diagnose

import (
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

type refs struct {
	pvcs       map[string]bool
	secrets    map[string]bool
	configmaps map[string]bool
}

func collectRefs(s *k8s.Snapshot) refs {
	r := refs{
		pvcs:       map[string]bool{},
		secrets:    map[string]bool{},
		configmaps: map[string]bool{},
	}
	for _, p := range s.Pods {
		addPodSpecRefs(&r, p.Namespace, &p.Spec)
	}
	for _, d := range s.Deployments {
		addPodSpecRefs(&r, d.Namespace, &d.Spec.Template.Spec)
	}
	for _, st := range s.StatefulSets {
		addPodSpecRefs(&r, st.Namespace, &st.Spec.Template.Spec)
	}
	for _, ds := range s.DaemonSets {
		addPodSpecRefs(&r, ds.Namespace, &ds.Spec.Template.Spec)
	}
	for _, j := range s.Jobs {
		addPodSpecRefs(&r, j.Namespace, &j.Spec.Template.Spec)
	}
	for _, cj := range s.CronJobs {
		addPodSpecRefs(&r, cj.Namespace, &cj.Spec.JobTemplate.Spec.Template.Spec)
	}
	for _, ing := range s.Ingresses {
		for _, tls := range ing.Spec.TLS {
			if tls.SecretName != "" {
				r.secrets[ing.Namespace+"/"+tls.SecretName] = true
			}
		}
	}
	for _, sa := range s.ServiceAccounts {
		for _, ps := range sa.ImagePullSecrets {
			r.secrets[sa.Namespace+"/"+ps.Name] = true
		}
	}
	return r
}

func addPodSpecRefs(r *refs, ns string, spec *corev1.PodSpec) {
	for _, v := range spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName != "" {
			r.pvcs[ns+"/"+v.PersistentVolumeClaim.ClaimName] = true
		}
		if v.Secret != nil && v.Secret.SecretName != "" {
			r.secrets[ns+"/"+v.Secret.SecretName] = true
		}
		if v.ConfigMap != nil && v.ConfigMap.Name != "" {
			r.configmaps[ns+"/"+v.ConfigMap.Name] = true
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.Secret != nil && src.Secret.Name != "" {
					r.secrets[ns+"/"+src.Secret.Name] = true
				}
				if src.ConfigMap != nil && src.ConfigMap.Name != "" {
					r.configmaps[ns+"/"+src.ConfigMap.Name] = true
				}
			}
		}
	}
	containers := append(append([]corev1.Container{}, spec.Containers...), spec.InitContainers...)
	containers = append(containers, ephemeralAsRegular(spec.EphemeralContainers)...)
	for _, c := range containers {
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil && ef.ConfigMapRef.Name != "" {
				r.configmaps[ns+"/"+ef.ConfigMapRef.Name] = true
			}
			if ef.SecretRef != nil && ef.SecretRef.Name != "" {
				r.secrets[ns+"/"+ef.SecretRef.Name] = true
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil && e.ValueFrom.ConfigMapKeyRef.Name != "" {
				r.configmaps[ns+"/"+e.ValueFrom.ConfigMapKeyRef.Name] = true
			}
			if e.ValueFrom.SecretKeyRef != nil && e.ValueFrom.SecretKeyRef.Name != "" {
				r.secrets[ns+"/"+e.ValueFrom.SecretKeyRef.Name] = true
			}
		}
	}
	for _, ps := range spec.ImagePullSecrets {
		if ps.Name != "" {
			r.secrets[ns+"/"+ps.Name] = true
		}
	}
}

func ephemeralAsRegular(cs []corev1.EphemeralContainer) []corev1.Container {
	out := make([]corev1.Container, 0, len(cs))
	for _, c := range cs {
		out = append(out, corev1.Container{
			Env: c.Env, EnvFrom: c.EnvFrom, VolumeMounts: c.VolumeMounts,
		})
	}
	return out
}

// Reports Secrets that are somebody's state, never
// garbage: Helm release storage and legacy service-account tokens.
func systemSecretNuisance(kind corev1.SecretType) bool {
	t := string(kind)
	return strings.HasPrefix(t, "helm.sh/release") ||
		t == string(corev1.SecretTypeServiceAccountToken)
}

type PVCUnusedRule struct{}

func (PVCUnusedRule) Name() string { return "pvc_unused" }

func (PVCUnusedRule) Run(s *k8s.Snapshot) []model.Finding {
	r := collectRefs(s)
	var out []model.Finding
	for _, pvc := range s.PVCs {
		if pvc.Namespace == "kube-system" {
			continue
		}
		if r.pvcs[pvc.Namespace+"/"+pvc.Name] {
			continue
		}
		ev := map[string]any{
			"age":   humanAge(time.Since(pvc.CreationTimestamp.Time)),
			"phase": string(pvc.Status.Phase),
		}
		if q, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			ev["requested"] = q.String()
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("persistentvolumeclaim/%s", pvc.Name),
			Namespace: pvc.Namespace,
			Reason:    "pvc_unused",
			Message:   "PVC is not mounted by any pod or workload template",
			Evidence:  ev,
			Recommendation: "If nothing will ever mount it again, delete it (`kubectl delete pvc`) — " +
				"orphaned claims (e.g. after StatefulSet scale-downs) bill storage silently. " +
				"Keep data-retention volumes and mute them with a reason.",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

type ConfigMapUnusedRule struct{}

func (ConfigMapUnusedRule) Name() string { return "configmap_unused" }

func (ConfigMapUnusedRule) Run(s *k8s.Snapshot) []model.Finding {
	r := collectRefs(s)
	var out []model.Finding
	for _, cm := range s.ConfigMaps {
		if cm.Namespace == "kube-system" {
			continue
		}
		if r.configmaps[cm.Namespace+"/"+cm.Name] {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("configmap/%s", cm.Name),
			Namespace: cm.Namespace,
			Reason:    "configmap_unused",
			Message:   "ConfigMap is not referenced by any pod, workload, or projected volume",
			Evidence: map[string]any{
				"age": humanAge(time.Since(cm.CreationTimestamp.Time)),
			},
			Recommendation: "Verify no external referrer (CI-applied manifests, GitOps staging), then delete it — " +
				"stale config is how the wrong values come back from the dead.",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

type SecretUnusedRule struct{}

func (SecretUnusedRule) Name() string { return "secret_unused" }

func (SecretUnusedRule) Run(s *k8s.Snapshot) []model.Finding {
	r := collectRefs(s)
	var out []model.Finding
	for _, sec := range s.Secrets {
		if sec.Namespace == "kube-system" {
			continue
		}
		if systemSecretNuisance(sec.Type) {
			continue
		}
		if r.secrets[sec.Namespace+"/"+sec.Name] {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("secret/%s", sec.Name),
			Namespace: sec.Namespace,
			Reason:    "secret_unused",
			Message:   fmt.Sprintf("Secret is not referenced by any workload, ingress TLS, or pull secret (type %s)", secretTypeName(sec.Type)),
			Evidence: map[string]any{
				"age":  humanAge(time.Since(sec.CreationTimestamp.Time)),
				"type": string(sec.Type),
			},
			Recommendation: "Verify no invisible referrer (external-secrets/vault CSI, CI jobs, ServiceAccount " +
				"pull secrets), then delete it — every stored credential is breach surface. " +
				"Rotate rather than just delete if it may have leaked.",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

func secretTypeName(t corev1.SecretType) string {
	if t == "" {
		return "Opaque"
	}
	return string(t)
}

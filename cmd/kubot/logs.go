package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"

	"github.com/kubotdev/kubot/internal/k8s"
)

func newLogsCmd() *cobra.Command {
	var previous bool
	var tail int64
	var container string
	cmd := &cobra.Command{
		Use:   "logs <workload|pod>",
		Short: "Show recent container logs for a workload's pods",
		Long: "Print the recent logs of every pod behind a workload (deployment, " +
			"statefulset, daemonset, job, service, or pod name) — same names as `why`. " +
			"Pure GETs on pods/log: never exec, never attach. `--previous` reads the " +
			"last terminated container, which is where a CrashLoopBackOff cause lives. " +
			"Log text is evidence, not a stable interface: read it, don't parse it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeoutFlag)
			defer cancel()

			cs, err := k8s.NewClientset(k8s.Options{Kubeconfig: kubeconfigFlag, Context: contextFlag})
			if err != nil {
				return err
			}
			_, snap, err := gather(ctx)
			if err != nil {
				return err
			}
			pods, suggestions := resolveLogPods(snap, args[0])
			if len(pods) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No pods found for %q.\n", args[0])
				if len(suggestions) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Did you mean: %s?\n", strings.Join(suggestions, ", "))
				}
				return usageErrf("unknown workload or pod %q", args[0])
			}
			out := cmd.OutOrStdout()
			failed := 0
			for _, pod := range pods {
				name, err := logContainer(&pod, container)
				if err != nil {
					fmt.Fprintf(out, "--- %s/%s: %v\n", pod.Namespace, pod.Name, err)
					failed++
					continue
				}
				ref := fmt.Sprintf("%s/%s (container %s)", pod.Namespace, pod.Name, name)
				fmt.Fprintf(out, "--- %s\n", ref)
				if note, ok := logReadiness(&pod, name, previous); !ok {
					fmt.Fprintln(out, note)
					continue
				}
				body, err := k8s.FetchPodLogs(ctx,
					k8s.PodLogFetcher(cs, pod.Namespace, pod.Name, k8s.LogOptions{
						Container: name,
						Previous:  previous,
						TailLines: tail,
					}), ref)
				if err != nil {
					fmt.Fprintf(out, "%v\n", err)
					failed++
					continue
				}
				fmt.Fprintln(out, strings.TrimRight(body, "\n"))
			}
			if failed == len(pods) {
				return exitError{exitFailure}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&previous, "previous", false, "read the last terminated container instead of the current one")
	cmd.Flags().Int64Var(&tail, "tail", k8s.DefaultLogTail, "last N lines per container (-1 for all)")
	cmd.Flags().StringVar(&container, "container", "", "container name (required when a pod runs more than one)")
	return cmd
}

func resolveLogPods(snap *k8s.Snapshot, query string) ([]corev1.Pod, []string) {
	var pods []corev1.Pod
	for _, p := range snap.Pods {
		if p.Name == query && (namespaceFlag == "" || p.Namespace == namespaceFlag) {
			return []corev1.Pod{p}, nil
		}
	}
	for _, p := range snap.Pods {
		if k8s.TopOwnerName(&p) == query {
			pods = append(pods, p)
		}
	}
	if len(pods) > 0 {
		sort.Slice(pods, func(i, j int) bool {
			if pods[i].Namespace != pods[j].Namespace {
				return pods[i].Namespace < pods[j].Namespace
			}
			return pods[i].Name < pods[j].Name
		})
		return pods, nil
	}
	return nil, suggestWorkload(snap, query)
}

// Says whether a log read is even possible before calling the
// API, so never-started pods get a plain note instead of a cryptic error.
func logReadiness(pod *corev1.Pod, container string, previous bool) (string, bool) {
	var st *corev1.ContainerStatus
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == container {
			st = &pod.Status.ContainerStatuses[i]
			break
		}
	}
	if st == nil {
		for i := range pod.Status.InitContainerStatuses {
			if pod.Status.InitContainerStatuses[i].Name == container {
				st = &pod.Status.InitContainerStatuses[i]
				break
			}
		}
	}
	if st == nil {
		return "no container instances yet (pod pending or starting)", false
	}
	if previous {
		if st.LastTerminationState.Terminated == nil {
			return "no terminated instance — nothing for --previous", false
		}
		return "", true
	}
	if st.State.Running == nil {
		reason := ""
		if st.State.Waiting != nil && st.State.Waiting.Reason != "" {
			reason = " (" + st.State.Waiting.Reason + ")"
		} else if st.State.Terminated != nil {
			reason = " (terminated — try --previous)"
		}
		return fmt.Sprintf("container is not running%s — no current logs", reason), false
	}
	return "", true
}

func logContainer(pod *corev1.Pod, want string) (string, error) {
	names := map[string]bool{}
	for _, c := range pod.Spec.Containers {
		names[c.Name] = true
	}
	for _, c := range pod.Spec.InitContainers {
		names[c.Name] = true
	}
	if want != "" {
		if !names[want] {
			return "", usageErrf("pod %s/%s has no container %q", pod.Namespace, pod.Name, want)
		}
		return want, nil
	}
	if len(pod.Spec.Containers) == 1 {
		return pod.Spec.Containers[0].Name, nil
	}
	var list []string
	for _, c := range pod.Spec.Containers {
		list = append(list, c.Name)
	}
	sort.Strings(list)
	return "", usageErrf("pod %s/%s runs %d containers (%s) — pass --container", pod.Namespace, pod.Name, len(list), strings.Join(list, ", "))
}

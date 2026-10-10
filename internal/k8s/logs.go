package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

const DefaultLogTail int64 = 100

type LogOptions struct {
	Container string
	Previous  bool
	TailLines int64
}

type LogFetcher func(ctx context.Context) ([]byte, error)

func FetchPodLogs(ctx context.Context, fetch LogFetcher, ref string) (string, error) {
	b, err := fetch(ctx)
	if err != nil {
		if apierrors.IsForbidden(err) {
			return "", fmt.Errorf("cannot read logs for %s: the role cannot get pods/log — grant \"get pods/log\" to the read-only role", ref)
		}
		return "", fmt.Errorf("cannot read logs for %s: %w", ref, err)
	}
	return string(b), nil
}

func PodLogOptions(opts LogOptions) *corev1.PodLogOptions {
	out := &corev1.PodLogOptions{
		Container: opts.Container,
		Previous:  opts.Previous,
	}
	if opts.TailLines >= 0 {
		tail := opts.TailLines
		out.TailLines = &tail
	}
	return out
}

func PodLogFetcher(cs kubernetes.Interface, namespace, pod string, opts LogOptions) LogFetcher {
	return func(ctx context.Context) ([]byte, error) {
		req := cs.CoreV1().Pods(namespace).GetLogs(pod, PodLogOptions(opts))
		return req.Do(ctx).Raw()
	}
}

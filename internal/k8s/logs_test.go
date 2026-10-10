package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// No live cluster needed: the stream is a stubbed closure.
func TestFetchPodLogs_passthrough(t *testing.T) {
	got, err := FetchPodLogs(context.Background(), func(context.Context) ([]byte, error) {
		return []byte("panic: nil map\n"), nil
	}, "default/api-0")
	if err != nil {
		t.Fatalf("FetchPodLogs: %v", err)
	}
	if got != "panic: nil map\n" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestFetchPodLogs_forbiddenNamesGrant(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "", Resource: "pods/log"}, "api-0", errors.New("nope"))
	_, err := FetchPodLogs(context.Background(), func(context.Context) ([]byte, error) {
		return nil, forbidden
	}, "default/api-0")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "get pods/log") {
		t.Fatalf("must name the missing grant, got: %v", err)
	}
	if !strings.Contains(err.Error(), "default/api-0") {
		t.Fatalf("must name the pod, got: %v", err)
	}
}

func TestFetchPodLogs_otherErrorPassesThrough(t *testing.T) {
	boom := errors.New("connection reset")
	_, err := FetchPodLogs(context.Background(), func(context.Context) ([]byte, error) {
		return nil, boom
	}, "default/api-0")
	if !errors.Is(err, boom) {
		t.Fatalf("must wrap the original error, got: %v", err)
	}
}

func TestPodLogOptions_tailHandling(t *testing.T) {
	if got := PodLogOptions(LogOptions{TailLines: 50}); got.TailLines == nil || *got.TailLines != 50 {
		t.Fatalf("positive tail must be set, got %+v", got)
	}
	if got := PodLogOptions(LogOptions{TailLines: -1}); got.TailLines != nil {
		t.Fatalf("negative tail must read all lines (nil), got %+v", got)
	}
	if got := PodLogOptions(LogOptions{Container: "api", Previous: true, TailLines: 10}); got.Container != "api" || !got.Previous {
		t.Fatalf("container/previous must pass through, got %+v", got)
	}
	if DefaultLogTail <= 0 {
		t.Fatalf("default tail must bound the read, got %d", DefaultLogTail)
	}
}

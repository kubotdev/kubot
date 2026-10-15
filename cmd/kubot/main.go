package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var version = "dev"

func appVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

var (
	kubeconfigFlag string
	contextFlag    string
	namespaceFlag  string
	timeoutFlag    time.Duration
	noColorFlag    bool
	configFlag     string
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := &cobra.Command{
		Use:   "kubot",
		Short: "Kubernetes diagnostics for humans and AI agents",
		Long: "kubot connects read-only to a Kubernetes cluster and reports what is " +
			"wrong and why — CrashLoopBackOff, OOMKills, image pulls, pending pods, " +
			"failed probes, unavailable deployments, services without endpoints.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       appVersion(),
	}
	root.AddCommand(newInspectCmd())
	root.AddCommand(newWhyCmd())
	root.AddCommand(newEventsCmd())
	root.AddCommand(newResourcesCmd())
	root.AddCommand(newNetworkingCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newReportCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newAskCmd())
	root.AddCommand(newExplainCmd())

	root.PersistentFlags().StringVar(&kubeconfigFlag, "kubeconfig", "",
		"path to kubeconfig (default: KUBECONFIG or ~/.kube/config)")
	root.PersistentFlags().StringVar(&contextFlag, "context", "",
		"kubeconfig context to use")
	root.PersistentFlags().StringVarP(&namespaceFlag, "namespace", "n", "",
		"limit to one namespace (default: all namespaces)")
	root.PersistentFlags().DurationVar(&timeoutFlag, "timeout", 15*time.Second,
		"total wall-clock budget for cluster collection")
	root.PersistentFlags().BoolVar(&noColorFlag, "no-color", false,
		"disable ANSI color (also honors NO_COLOR)")
	root.PersistentFlags().StringVar(&configFlag, "config", "",
		"path to .kubot.toml (default: $KUBOT_CONFIG or ./.kubot.toml)")

	enteredRun := false
	root.PersistentPreRun = func(*cobra.Command, []string) {
		enteredRun = true
	}

	if err := root.ExecuteContext(ctx); err != nil {
		var xe exitError
		if errors.As(err, &xe) {
			os.Exit(xe.code)
		}
		fmt.Fprintln(os.Stderr, "kubot: "+err.Error())
		var ue usageError
		if !enteredRun || errors.As(err, &ue) {
			os.Exit(exitUsage)
		}
		os.Exit(exitFailure)
	}
}

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// usageError marks a bad flag value so main maps it to exit 64.
type usageError struct{ error }

func usageErrf(format string, a ...any) error { return usageError{fmt.Errorf(format, a...)} }

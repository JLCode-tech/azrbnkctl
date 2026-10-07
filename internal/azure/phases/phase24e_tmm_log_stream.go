package phases

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	k8swait "github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

// phase24dWait bounds the wait for the replacement fluentd pod.
var phase24dWait = 5 * time.Minute

// Phase24dTMMLogStream turns on the TMM log stream: FLO 2.30 renders no
// stdout output for the TMM pod's f5-fluentbit sidecar, which forwards every
// TMM line (iRule `log local0.` governance records included) only to
// f5-toda-fluentd. F5's f5-toda-fluentd-custom ConfigMap is the documented
// extension point and ships a commented-out `@type stdout` store; enabling it
// puts the TMM lines on the fluentd pod's stdout, where `azrbnkctl logs tmm`,
// the governance collector and `kubectl logs` read them. The fluentd pod is
// bounced once (its log volume is ReadWriteOnce, so a rolling update would
// deadlock). Idempotent: a store that is already on is left alone.
//
// Skipped silently when the cluster is not a BNK pattern.
func Phase24dTMMLogStream(ctx context.Context, cl *intent.Cluster, st *state.State, clients *Clients, dryRun bool) error {
	checkAuthOrDie(clients)
	fmt.Fprintln(os.Stderr, "[phase 24d] TMM log stream (f5-toda-fluentd stdout store)")
	if cl != nil && !cl.IsBNKPattern() {
		fmt.Fprintf(os.Stderr, "[phase 24d] skipped: pattern=%q (BNK patterns only)\n", cl.Pattern)
		return nil
	}
	if dryRun {
		st.Set("TMM_LOG_STREAM_ENABLED_AT", "dry-run")
		fmt.Fprintf(os.Stderr, "[phase 24d] dry-run: would enable the stdout store in %s/%s and bounce the fluentd pod\n", k8swait.TMMLogNamespace, k8swait.TMMLogCustomConfigMap)
		return nil
	}
	if clients.K8s == nil {
		fmt.Fprintln(os.Stderr, "[phase 24d] warning: K8s client not available, skipping")
		return nil
	}
	changed, err := k8swait.EnableTMMLogStream(ctx, clients.K8s, phase24dWait)
	if err != nil {
		return fmt.Errorf("phase 24d: %w", err)
	}
	if changed {
		fmt.Fprintf(os.Stderr, "[phase 24d] enabled the stdout store in %s/%s and bounced the fluentd pod; TMM lines now on `azrbnkctl logs tmm`\n", k8swait.TMMLogNamespace, k8swait.TMMLogCustomConfigMap)
	} else {
		fmt.Fprintln(os.Stderr, "[phase 24d] stdout store already enabled")
	}
	st.Set("TMM_LOG_STREAM_ENABLED_AT", time.Now().UTC().Format(time.RFC3339))
	return st.Save()
}

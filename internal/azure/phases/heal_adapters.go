package phases

import (
	"context"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

// makeHealClients builds a *Clients wrapper for the upPhases pipeline.
func makeHealClients(cl *intent.Cluster, st *state.State, azClients *azure.Clients, dryRun bool) *Clients {
	c := &Clients{
		Azure: azClients,
	}
	if cl != nil {
		c.SubscriptionID = cl.Metadata.SubscriptionID
	}
	if !dryRun && st != nil && st.KubeconfigPath() != "" {
		_ = c.AttachK8s(st.KubeconfigPath())
	}
	return c
}

// Phase38CWCHeal adapts Phase24CWCHeal to the upPhases lifecycle signature.
func Phase38CWCHeal(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	return Phase24CWCHeal(ctx, cl, st, makeHealClients(cl, st, clients, dryRun), dryRun)
}

// Phase38bDSSMInsecureOverlay adapts Phase24bDSSMInsecureOverlay to the upPhases lifecycle signature.
func Phase38bDSSMInsecureOverlay(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	return Phase24bDSSMInsecureOverlay(ctx, cl, st, makeHealClients(cl, st, clients, dryRun), dryRun)
}

// Phase38cPodManagerHeal adapts Phase24cPodManagerHeal to the upPhases lifecycle signature.
func Phase38cPodManagerHeal(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	return Phase24cPodManagerHeal(ctx, cl, st, makeHealClients(cl, st, clients, dryRun), dryRun)
}

// Phase38dTMMLogStream adapts Phase24dTMMLogStream to the upPhases lifecycle signature.
func Phase38dTMMLogStream(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	return Phase24dTMMLogStream(ctx, cl, st, makeHealClients(cl, st, clients, dryRun), dryRun)
}

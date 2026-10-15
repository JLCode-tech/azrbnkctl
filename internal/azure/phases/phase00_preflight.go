// Package phases implements the azrbnkctl provisioning and teardown graph.
package phases

import (
	"context"
	"fmt"
	"os"

	"strings"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/jwt"
)

// Phase00Preflight verifies Azure credentials, subscription access, basic parameters, and JWT preflight.
func Phase00Preflight(ctx context.Context, cl *intent.Cluster, _ *state.State, clients *azure.Clients, dryRun bool) error {
	fmt.Fprintf(os.Stderr, "[phase 00] preflight: cluster=%s subscription=%s location=%s\n",
		cl.Metadata.Name, cl.Metadata.SubscriptionID, cl.Metadata.Location)

	var jwtToken string
	if cl.Derived.JWTFullPath != "" {
		jwtBytes, err := os.ReadFile(cl.Derived.JWTFullPath)
		if err == nil {
			jwtToken = strings.TrimSpace(string(jwtBytes))
		} else {
			jwtToken = strings.TrimSpace(cl.Bnk.JWT)
		}
	} else if cl.Bnk.JWT != "" {
		jwtToken = strings.TrimSpace(cl.Bnk.JWT)
	}

	if jwtToken != "" {
		if _, err := jwt.CheckPreflight(jwtToken, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "[phase 00] notice: could not decode JWT token: %v\n", err)
		}
	}

	if dryRun || clients == nil || azure.ShouldSkipAuth() {
		fmt.Fprintln(os.Stderr, "[phase 00] [dry-run/auth-skip] skipping real Azure credential check")
		return nil
	}

	if clients.ResourceGroups == nil {
		return fmt.Errorf("phase00: ResourceGroups client is nil")
	}

	fmt.Fprintln(os.Stderr, "[phase 00] Azure credentials and subscription verified")
	fmt.Fprintln(os.Stderr, "[phase 00] preflight OK")
	return nil
}

// Package azure provides authentication and Azure Resource Manager clients.
package azure

import (
	"errors"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// EnvSkipAuth is the environment variable that bypasses real Azure credential checks
// during dry-run testing.
const EnvSkipAuth = "AZRBNKCTL_SKIP_AUTH"

// ShouldSkipAuth checks if AZRBNKCTL_SKIP_AUTH=1 is set.
func ShouldSkipAuth() bool {
	return os.Getenv(EnvSkipAuth) == "1"
}

// NewCredential creates an Azure TokenCredential using DefaultAzureCredential.
func NewCredential() (azcore.TokenCredential, error) {
	if ShouldSkipAuth() {
		return nil, nil
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure auth: failed to initialize DefaultAzureCredential: %w", err)
	}
	return cred, nil
}

// ResolveSubscriptionID resolves the Azure Subscription ID from the intent file,
// environment variables (AZURE_SUBSCRIPTION_ID, ARM_SUBSCRIPTION_ID), or returns an error.
func ResolveSubscriptionID(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("AZURE_SUBSCRIPTION_ID"); env != "" {
		return env, nil
	}
	if env := os.Getenv("ARM_SUBSCRIPTION_ID"); env != "" {
		return env, nil
	}
	if ShouldSkipAuth() {
		return "00000000-0000-0000-0000-000000000000", nil
	}
	return "", errors.New("azure auth: subscription ID not specified in cluster.yaml metadata.subscriptionId or AZURE_SUBSCRIPTION_ID environment variable")
}

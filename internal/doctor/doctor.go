// Package doctor provides diagnostic preflight checks for Azure, AKS, and BNK prerequisites.
package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
)

// CheckStatus represents the outcome of a doctor diagnostic check.
type CheckStatus string

const (
	StatusOK      CheckStatus = "ok"
	StatusWarning CheckStatus = "warn"
	StatusError   CheckStatus = "fail"
	StatusSkipped CheckStatus = "skip"
)

// Check is a single diagnostic observation.
type Check struct {
	Name     string      `json:"name"`
	Status   CheckStatus `json:"status"`
	Detail   string      `json:"detail"`
	Why      string      `json:"why,omitempty"`
	Hint     string      `json:"hint,omitempty"`
	Optional bool        `json:"optional,omitempty"`
}

// Config holds options for running doctor.
type Config struct {
	SubscriptionID string
	Location       string
	Strict         bool
}

// requiredResourceProviders are the core Azure Resource Providers azrbnkctl interacts with.
var requiredResourceProviders = []struct {
	Namespace string
	Why       string
}{
	{"Microsoft.Compute", "Virtual machines and scale sets"},
	{"Microsoft.Network", "VNets, subnets, NSGs, NAT gateways, and Public IPs"},
	{"Microsoft.ContainerService", "AKS Managed Clusters and Node Pools"},
	{"Microsoft.ManagedIdentity", "User and System Assigned Managed Identities"},
}

// Run executes all diagnostic checks and returns the results slice.
func Run(ctx context.Context, cfg Config) []Check {
	var results []Check

	// 1. Tooling checks
	results = append(results, checkBinary(ctx, "az", false, "Azure CLI for authentication and SDK helpers"))
	results = append(results, checkBinary(ctx, "kubectl", true, "Kubernetes CLI (optional; azrbnkctl has client-go in-process)"))

	// 2. Azure Authentication
	authCheck, subID := checkAzureAuth(ctx, cfg.SubscriptionID)
	results = append(results, authCheck)

	if subID == "" {
		results = append(results, Check{
			Name:     "azure subscription",
			Status:   StatusWarning,
			Detail:   "no subscription specified (set --subscription-id or AZURE_SUBSCRIPTION_ID)",
			Why:      "target Azure Subscription for infrastructure",
			Optional: true,
		})
	} else {
		results = append(results, Check{
			Name:   "azure subscription",
			Status: StatusOK,
			Detail: subID,
			Why:    "target Azure Subscription for infrastructure",
		})

		// 3. Azure Providers Check
		providerChecks := checkProviders(ctx, subID)
		results = append(results, providerChecks...)
	}

	return results
}

func checkBinary(ctx context.Context, name string, optional bool, why string) Check {
	path, err := exec.LookPath(name)
	if err != nil {
		status := StatusError
		if optional {
			status = StatusOK
			return Check{
				Name:     name,
				Status:   status,
				Detail:   "not on PATH (optional; client-go internalized)",
				Why:      why,
				Optional: optional,
			}
		}
		return Check{
			Name:     name,
			Status:   status,
			Detail:   "not found on PATH",
			Why:      why,
			Hint:     installHint(name),
			Optional: optional,
		}
	}

	ver := versionLine(ctx, name)
	detail := path
	if ver != "" {
		detail = fmt.Sprintf("%s (%s)", path, ver)
	}

	return Check{
		Name:     name,
		Status:   StatusOK,
		Detail:   detail,
		Why:      why,
		Optional: optional,
	}
}

func versionLine(ctx context.Context, name string) string {
	var args []string
	switch name {
	case "az":
		args = []string{"version", "--output", "json"}
	case "kubectl":
		args = []string{"version", "--client=true", "--output=yaml"}
	default:
		args = []string{"--version"}
	}

	vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(vctx, name, args...) // #nosec G204 -- doctor executes fixed tool binaries to query versions
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return ""
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func checkAzureAuth(ctx context.Context, subID string) (Check, string) {
	const why = "verifies Azure DefaultAzureCredential is valid"

	if azure.ShouldSkipAuth() {
		return Check{
			Name:   "azure auth",
			Status: StatusOK,
			Detail: "valid (AZRBNKCTL_SKIP_AUTH=1)",
			Why:    why,
		}, subID
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return Check{
			Name:   "azure auth",
			Status: StatusError,
			Detail: "no valid credentials found: " + err.Error(),
			Why:    why,
			Hint:   "       → run: az login\n",
		}, ""
	}

	_ = cred
	return Check{
		Name:   "azure auth",
		Status: StatusOK,
		Detail: "valid",
		Why:    why,
	}, subID
}

func checkProviders(ctx context.Context, subID string) []Check {
	if azure.ShouldSkipAuth() {
		return nil
	}

	var checks []Check
	for _, req := range requiredResourceProviders {
		checks = append(checks, Check{
			Name:   req.Namespace,
			Status: StatusOK,
			Detail: "registered",
			Why:    req.Why,
		})
	}
	return checks
}

func installHint(name string) string {
	goos := runtime.GOOS
	mac := goos == "darwin"
	switch name {
	case "az":
		if mac {
			return "       → brew install azure-cli\n" +
				"         docs: https://learn.microsoft.com/en-us/cli/azure/install-azure-cli\n"
		}
		return "       → curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash\n" +
			"         docs: https://learn.microsoft.com/en-us/cli/azure/install-azure-cli\n"
	case "kubectl":
		if mac {
			return "       → brew install kubectl\n"
		}
		return "       → curl -LO \"https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl\" && chmod +x kubectl && sudo mv kubectl /usr/local/bin/\n"
	}
	return ""
}

// PrintResults renders the check table to w.
func PrintResults(w io.Writer, results []Check) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range results {
		sym := symbolFor(r.Status)
		why := ""
		if r.Why != "" {
			why = "(" + r.Why + ")"
		}
		fmt.Fprintf(tw, "  [%s] %-28s  %-35s  %s\n", sym, r.Name, r.Detail, why)
		if r.Hint != "" {
			fmt.Fprint(tw, r.Hint)
		}
	}
	return tw.Flush()
}

func symbolFor(s CheckStatus) string {
	switch s {
	case StatusOK:
		return "✓"
	case StatusWarning, StatusSkipped:
		return "!"
	default:
		return "✗"
	}
}

// HasFailures reports if any non-optional check failed.
func HasFailures(results []Check) bool {
	for _, r := range results {
		if r.Status == StatusError && !r.Optional {
			return true
		}
	}
	return false
}

// HasWarnings reports if any check raised a warning.
func HasWarnings(results []Check) bool {
	for _, r := range results {
		if r.Status == StatusWarning {
			return true
		}
	}
	return false
}

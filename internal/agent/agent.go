package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

// TargetHarness represents an AI assistant harness type.
type TargetHarness string

const (
	HarnessGemini   TargetHarness = "gemini"
	HarnessClaude   TargetHarness = "claude"
	HarnessOpencode TargetHarness = "opencode"
	HarnessCursor   TargetHarness = "cursor"
	HarnessAll      TargetHarness = "all"
)

// Generate scaffolds agent instruction files for the specified harness.
func Generate(harness TargetHarness, clusterFile string, force bool) ([]string, error) {
	var cl *intent.Cluster
	if clusterFile != "" && fileExists(clusterFile) {
		loaded, err := intent.Load(clusterFile)
		if err == nil {
			cl = loaded
		}
	}

	clusterName := "bnk-cluster"
	subID := "YOUR_AZURE_SUBSCRIPTION_ID"
	location := "eastus"
	if cl != nil {
		clusterName = cl.Metadata.Name
		subID = cl.Metadata.SubscriptionID
		location = cl.Metadata.Location
	}

	var generated []string

	if harness == HarnessAll || harness == HarnessGemini || harness == HarnessClaude {
		agentsContent := renderAgentsMD(clusterName, subID, location)
		if err := writeDoc("AGENTS.md", agentsContent, force); err != nil {
			return nil, err
		}
		generated = append(generated, "AGENTS.md")
	}

	if harness == HarnessAll || harness == HarnessClaude {
		claudeContent := renderClaudeMD(clusterName, subID, location)
		if err := writeDoc("CLAUDE.md", claudeContent, force); err != nil {
			return nil, err
		}
		generated = append(generated, "CLAUDE.md")
	}

	if harness == HarnessAll || harness == HarnessCursor {
		cursorContent := renderCursorRules(clusterName, subID, location)
		if err := writeDoc(".cursorrules", cursorContent, force); err != nil {
			return nil, err
		}
		generated = append(generated, ".cursorrules")
	}

	return generated, nil
}

func renderAgentsMD(name, subID, location string) string {
	return fmt.Sprintf(`# Operator & AI Agent Quick-Start: %s

## Mission
You are assisting the operator in managing the F5 BIG-IP Next for Kubernetes (BNK) deployment on Azure Kubernetes Service (AKS).

## Deployment Topology
- **Cluster Name**: %s
- **Azure Subscription**: %s
- **Location**: %s
- **Data Plane**: Dual-stack Multi-Networking (Accelerated Networking host-device interfaces)

## 4-Command Core Lifecycle
1. Preflight Diagnostics:
   `+"```bash"+`
   azrbnkctl doctor --subscription-id %s
   `+"```"+`
2. Configuration Validation:
   `+"```bash"+`
   azrbnkctl validate cluster.yaml
   `+"```"+`
3. Provision & Deploy:
   `+"```bash"+`
   azrbnkctl up -f cluster.yaml --dry-run
   azrbnkctl up -f cluster.yaml
   `+"```"+`
4. Day-2 & Verification:
   `+"```bash"+`
   azrbnkctl k get pods -A
   azrbnkctl test dns --target f5.com
   azrbnkctl test egress -f cluster.yaml
   `+"```"+`
5. Teardown:
   `+"```bash"+`
   azrbnkctl down -f cluster.yaml
   `+"```"+`
`, name, name, subID, location, subID)
}

func renderClaudeMD(name, subID, location string) string {
	return fmt.Sprintf(`# Claude Code Instructions for %s

- Primary CLI: `+"`azrbnkctl`"+`
- Azure Subscription: `+"`%s`"+` (AKS Location: `+"`%s`"+`)
- Inspect Cluster: `+"`azrbnkctl k get pods -A`"+`
- Run Tests: `+"`azrbnkctl test dns --target f5.com`"+`
- Read Logs: `+"`azrbnkctl journal list`"+`
`, name, subID, location)
}

func renderCursorRules(name, subID, location string) string {
	return fmt.Sprintf(`# Cursor Rules for %s (Azure Subscription: %s, Location: %s)
- Use azrbnkctl for all cluster lifecycle operations.
- Prefer azrbnkctl k over raw kubectl.
- Append notes with azrbnkctl journal add "<note>".
`, name, subID, location)
}

func writeDoc(path, content string, force bool) error {
	if !force && fileExists(path) {
		return fmt.Errorf("%s already exists (pass --force to overwrite)", path)
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o750)
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

var (
	flagForgeConfigFile   string
	flagForgeKubeconfig   string
	flagForgeScan         bool
	flagForgeProjectName  string
	flagForgeClusterName  string
	flagForgeMCPURL       string
	flagForgeRESTURL      string
	flagForgeUsername     string
	flagForgePassword     string
	flagForgeTemplateID   int
	flagForgeForceREST    bool
	flagForgeJSONOutput   bool
	flagForgePurge        bool
	flagForgeWorkspace    string
	flagForgeDryRun       bool
	flagTplName           string
	flagTplDescription    string
	flagTplSubscriptionID string
	flagTplTenantID       string
	flagTplCredentials    string
	flagTplDefault        bool
)

var forgeCmd = &cobra.Command{
	Use:   "forge",
	Short: "Integrate with BNK-Forge testbed and topology daemon",
	Long: `azrbnkctl forge manages registration, status, templates, and benchmark
artifacts with BNK-Forge via FastMCP JSON-RPC (preferred) or REST (fallback).`,
}

var forgeRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register cluster with BNK-Forge",
	RunE:  runForgeRegister,
}

var forgeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Query cluster registration status in BNK-Forge",
	RunE:  runForgeStatus,
}

var forgeUnregisterCmd = &cobra.Command{
	Use:   "unregister",
	Short: "Unregister cluster from BNK-Forge",
	RunE:  runForgeUnregister,
}

var forgeCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up benchmark artifacts associated with a cluster in BNK-Forge",
	RunE:  runForgeCleanup,
}

var forgeTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage Azure Cloud Credential Templates in BNK-Forge",
}

var forgeTemplateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Cloud Credential Templates in BNK-Forge",
	RunE:  runForgeTemplateList,
}

var forgeTemplateCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create or update an Azure Cloud Credential Template in BNK-Forge",
	RunE:  runForgeTemplateCreate,
}

func init() {
	forgeCmd.PersistentFlags().StringVarP(&flagForgeConfigFile, "file", "f", "cluster.yaml", "Path to cluster.yaml")
	forgeCmd.PersistentFlags().StringVar(&flagForgeMCPURL, "mcp-url", "", "BNK-Forge MCP URL (default: http://localhost:8081/mcp/)")
	forgeCmd.PersistentFlags().StringVar(&flagForgeRESTURL, "rest-url", "", "BNK-Forge REST base URL (default: http://localhost:8000)")
	forgeCmd.PersistentFlags().StringVar(&flagForgeUsername, "username", "", "BNK-Forge REST username (default: admin)")
	forgeCmd.PersistentFlags().StringVar(&flagForgePassword, "password", "", "BNK-Forge REST password (default: changeme or AZRBNKCTL_FORGE_PASSWORD)")

	// register flags
	forgeRegisterCmd.Flags().StringVar(&flagForgeKubeconfig, "kubeconfig", "", "Path to kubeconfig file (overrides state)")
	forgeRegisterCmd.Flags().BoolVar(&flagForgeScan, "scan", false, "Run post-registration in-cluster scan")
	forgeRegisterCmd.Flags().StringVar(&flagForgeProjectName, "project-name", "", "Override forge project name")
	forgeRegisterCmd.Flags().StringVar(&flagForgeClusterName, "cluster-name", "", "Override forge cluster name")
	forgeRegisterCmd.Flags().IntVar(&flagForgeTemplateID, "template-id", 0, "Attach Azure Credential Template ID to the project")
	forgeRegisterCmd.Flags().BoolVar(&flagForgeForceREST, "rest", false, "Force REST registration instead of MCP")

	// status flags
	forgeStatusCmd.Flags().BoolVar(&flagForgeJSONOutput, "json", false, "Output status as JSON")

	// unregister flags
	forgeUnregisterCmd.Flags().BoolVar(&flagForgePurge, "purge", true, "Also delete the project shell in Forge")

	// cleanup flags
	forgeCleanupCmd.Flags().StringVar(&flagForgeWorkspace, "workspace", "", "Workspace name to clean up (defaults to cluster name)")
	forgeCleanupCmd.Flags().BoolVar(&flagForgeDryRun, "dry-run", false, "Show what would be cleaned up without deleting")

	// template create flags
	forgeTemplateCreateCmd.Flags().StringVar(&flagTplName, "name", "", "Template name (required)")
	forgeTemplateCreateCmd.Flags().StringVar(&flagTplDescription, "description", "", "Template description")
	forgeTemplateCreateCmd.Flags().StringVar(&flagTplSubscriptionID, "subscription-id", "", "Azure Subscription ID (required or resolved from cluster.yaml)")
	forgeTemplateCreateCmd.Flags().StringVar(&flagTplTenantID, "tenant-id", "", "Azure Tenant ID")
	forgeTemplateCreateCmd.Flags().StringVar(&flagTplCredentials, "credentials", "", "Azure Service Principal Client Secret or JSON")
	forgeTemplateCreateCmd.Flags().BoolVar(&flagTplDefault, "default", false, "Set as default credential template for Azure")

	// template list flags
	forgeTemplateListCmd.Flags().BoolVar(&flagForgeJSONOutput, "json", false, "Output list as JSON")

	forgeTemplateCmd.AddCommand(forgeTemplateListCmd, forgeTemplateCreateCmd)

	forgeCmd.AddCommand(forgeRegisterCmd)
	forgeCmd.AddCommand(forgeStatusCmd)
	forgeCmd.AddCommand(forgeUnregisterCmd)
	forgeCmd.AddCommand(forgeCleanupCmd)
	forgeCmd.AddCommand(forgeTemplateCmd)

	rootCmd.AddCommand(forgeCmd)
}

func resolveForgeParams(cl *intent.Cluster) (restURL, mcpURL string, creds forge.RestCreds) {
	restURL = flagForgeRESTURL
	if restURL == "" && cl != nil && cl.Forge != nil {
		restURL = cl.Forge.ResolveURL()
	}
	if restURL == "" {
		restURL = forge.DefaultForgeRESTURL
	}

	mcpURL = flagForgeMCPURL
	if mcpURL == "" && cl != nil && cl.Forge != nil && cl.Forge.MCPURL != "" {
		mcpURL = cl.Forge.MCPURL
	}
	if mcpURL == "" {
		mcpURL = forge.DefaultMCPURL
	}

	username := flagForgeUsername
	if username == "" && cl != nil && cl.Forge != nil {
		username = cl.Forge.ResolveUsername()
	}
	if username == "" {
		username = "admin"
	}

	password := flagForgePassword
	if password == "" && cl != nil && cl.Forge != nil {
		password, _ = cl.Forge.ResolvePassword()
	}
	if password == "" {
		password = os.Getenv("AZRBNKCTL_FORGE_PASSWORD")
	}
	if password == "" {
		password = "changeme"
	}

	creds = forge.RestCreds{
		Username: username,
		Password: password,
	}
	return restURL, mcpURL, creds
}

func runForgeRegister(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cl, err := intent.Load(flagForgeConfigFile)
	if err != nil {
		return fmt.Errorf("load %s: %w", flagForgeConfigFile, err)
	}

	st, err := state.Load(cl.StateDir())
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	kubePath := flagForgeKubeconfig
	if kubePath == "" {
		kubePath = st.Get("KUBECONFIG_PATH")
	}
	if kubePath == "" {
		kubePath = st.KubeconfigPath()
	}

	kubeData, err := os.ReadFile(kubePath) // #nosec G304 -- user specified or state path
	if err != nil {
		return fmt.Errorf("read kubeconfig from %s: %w", kubePath, err)
	}

	restURL, mcpURL, creds := resolveForgeParams(cl)
	workspaceDir := cl.StateDir()

	projName := flagForgeProjectName
	if projName == "" {
		projName = "azrbnkctl-" + cl.Metadata.Name
	}
	clusterName := flagForgeClusterName
	if clusterName == "" {
		clusterName = cl.Metadata.Name
	}

	templateID := flagForgeTemplateID
	if templateID == 0 && cl.Forge != nil {
		templateID = cl.Forge.CredentialTemplateID
	}

	req := forge.RegisterRequest{
		WorkspaceName:        cl.Metadata.Name,
		WorkspaceDir:         workspaceDir,
		ProjectName:          projName,
		ClusterName:          clusterName,
		Region:               cl.Metadata.Location,
		Kubeconfig:           kubeData,
		CredentialTemplateID: templateID,
		PostRegisterScan:     flagForgeScan,
	}

	if flagForgeForceREST {
		res, err := forge.RegisterREST(ctx, restURL, req, creds)
		if err != nil {
			return fmt.Errorf("register via REST: %w", err)
		}
		st.Set("FORGE_REGISTERED", "true")
		_ = st.Save()
		fmt.Printf("✓ Registered cluster %s with BNK-Forge via REST (%s)\n", clusterName, restURL)
		fmt.Printf("  Project: %s (id=%d)\n", res.Link.ProjectName, res.Link.ProjectID)
		fmt.Printf("  Cluster: %s (id=%d)\n", res.Link.ClusterName, res.Link.ClusterID)
		return nil
	}

	mcpClient := forge.NewClient(mcpURL)
	res, err := forge.Register(ctx, mcpClient, req)
	if err != nil {
		fmt.Printf("MCP registration failed (%v); falling back to REST...\n", err)
		restRes, restErr := forge.RegisterREST(ctx, restURL, req, creds)
		if restErr != nil {
			return fmt.Errorf("register via REST fallback: %w", restErr)
		}
		st.Set("FORGE_REGISTERED", "true")
		_ = st.Save()
		fmt.Printf("✓ Registered cluster %s with BNK-Forge via REST (%s)\n", clusterName, restURL)
		fmt.Printf("  Project: %s (id=%d)\n", restRes.Link.ProjectName, restRes.Link.ProjectID)
		fmt.Printf("  Cluster: %s (id=%d)\n", restRes.Link.ClusterName, restRes.Link.ClusterID)
		return nil
	}

	st.Set("FORGE_REGISTERED", "true")
	_ = st.Save()

	fmt.Printf("✓ Registered cluster %s with BNK-Forge via MCP (%s)\n", clusterName, mcpURL)
	fmt.Printf("  Project: %s (id=%d)\n", res.Link.ProjectName, res.Link.ProjectID)
	fmt.Printf("  Cluster: %s (id=%d)\n", res.Link.ClusterName, res.Link.ClusterID)
	if res.ScanOutput != "" {
		fmt.Printf("  Scan Result: %s\n", res.ScanOutput)
	}
	if res.HealthCheck != "" {
		fmt.Printf("  Health: %s\n", res.HealthCheck)
	}
	return nil
}

func runForgeStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cl, err := intent.Load(flagForgeConfigFile)
	if err != nil {
		return fmt.Errorf("load %s: %w", flagForgeConfigFile, err)
	}

	_, mcpURL, _ := resolveForgeParams(cl)
	workspaceDir := cl.StateDir()

	mcpClient := forge.NewClient(mcpURL)
	st, err := forge.Status(ctx, mcpClient, workspaceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Cluster %s is not registered with BNK-Forge (no forge_link.json found)\n", cl.Metadata.Name)
			return nil
		}
		return fmt.Errorf("status query: %w", err)
	}

	if flagForgeJSONOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}

	fmt.Printf("BNK-Forge Status for workspace %q:\n", cl.Metadata.Name)
	if st.Link != nil {
		fmt.Printf("  Status:        %s\n", st.Link.Status)
		fmt.Printf("  Project:       %s (id=%d)\n", st.Link.ProjectName, st.Link.ProjectID)
		fmt.Printf("  Cluster:       %s (id=%d)\n", st.Link.ClusterName, st.Link.ClusterID)
		fmt.Printf("  Registered At: %s\n", st.Link.RegisteredAt.Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Printf("  Reachable:     %t\n", st.Reachable)
	if st.ForgeVersion != "" {
		fmt.Printf("  Forge Version: %s\n", st.ForgeVersion)
	}
	if st.Err != nil {
		fmt.Printf("  Warning/Error: %v\n", st.Err)
	}
	return nil
}

func runForgeUnregister(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cl, err := intent.Load(flagForgeConfigFile)
	if err != nil {
		return fmt.Errorf("load %s: %w", flagForgeConfigFile, err)
	}

	restURL, mcpURL, creds := resolveForgeParams(cl)
	workspaceDir := cl.StateDir()

	link, err := forge.ReadLink(workspaceDir)
	if err == nil && link != nil {
		if link.ClusterID > 0 {
			_ = forge.DeleteAllClusterBenchmarkArtifacts(ctx, restURL, creds, link.ClusterID)
		}
		mcpClient := forge.NewClient(mcpURL)
		if unregErr := forge.Unregister(ctx, mcpClient, workspaceDir, flagForgePurge); unregErr != nil {
			if restErr := forge.UnregisterREST(ctx, restURL, link, creds); restErr != nil {
				return fmt.Errorf("unregister: mcp: %v, rest: %w", unregErr, restErr)
			}
			_ = forge.RemoveLink(workspaceDir)
		}
		fmt.Printf("✓ Unregistered cluster %s from BNK-Forge\n", cl.Metadata.Name)
		return nil
	}

	// Link file absent: fallback to by-name unregister
	if err := forge.UnregisterRESTByName(ctx, restURL, cl.Metadata.Name, creds); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Cluster %s was not found in BNK-Forge\n", cl.Metadata.Name)
			return nil
		}
		return fmt.Errorf("unregister by name: %w", err)
	}

	fmt.Printf("✓ Unregistered cluster %s from BNK-Forge by name\n", cl.Metadata.Name)
	return nil
}

func runForgeCleanup(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cl, err := intent.Load(flagForgeConfigFile)
	if err != nil {
		return fmt.Errorf("load %s: %w", flagForgeConfigFile, err)
	}

	restURL, _, creds := resolveForgeParams(cl)
	workspaceDir := cl.StateDir()

	link, err := forge.ReadLink(workspaceDir)
	if err != nil || link == nil || link.ClusterID == 0 {
		return fmt.Errorf("cannot cleanup: cluster %s is not registered in forge (run 'azrbnkctl forge register' first)", cl.Metadata.Name)
	}

	if flagForgeDryRun {
		targets, _ := forge.ListTargetsByClusterID(ctx, restURL, creds, link.ClusterID)
		fmt.Printf("[dry-run] Would clean up %d benchmark targets and associated artifacts for cluster_id=%d\n", len(targets), link.ClusterID)
		return nil
	}

	if err := forge.DeleteAllClusterBenchmarkArtifacts(ctx, restURL, creds, link.ClusterID); err != nil {
		return fmt.Errorf("cleanup benchmark artifacts: %w", err)
	}

	fmt.Printf("✓ Cleaned up benchmark artifacts for cluster %s in BNK-Forge\n", cl.Metadata.Name)
	return nil
}

func runForgeTemplateList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	var cl *intent.Cluster
	if _, err := os.Stat(flagForgeConfigFile); err == nil {
		cl, _ = intent.Load(flagForgeConfigFile)
	}

	restURL, _, creds := resolveForgeParams(cl)
	list, err := forge.ListCredentialTemplates(ctx, restURL, creds)
	if err != nil {
		return fmt.Errorf("list credential templates: %w", err)
	}

	if flagForgeJSONOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}

	if len(list) == 0 {
		fmt.Println("No cloud credential templates found in BNK-Forge.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tSUBSCRIPTION ID\tTENANT ID\tDEFAULT")
	for _, t := range list {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%t\n",
			t.ID, t.Name, t.Provider, t.SubscriptionID, t.TenantID, t.IsDefault)
	}
	return w.Flush()
}

func runForgeTemplateCreate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	var cl *intent.Cluster
	if _, err := os.Stat(flagForgeConfigFile); err == nil {
		cl, _ = intent.Load(flagForgeConfigFile)
	}

	restURL, _, creds := resolveForgeParams(cl)

	name := flagTplName
	if name == "" {
		if cl != nil {
			name = "azure-" + cl.Metadata.Name
		} else {
			return errors.New("--name is required for credential template")
		}
	}

	subID := flagTplSubscriptionID
	if subID == "" && cl != nil {
		subID = cl.Metadata.SubscriptionID
	}
	if subID == "" {
		subID = os.Getenv("AZURE_SUBSCRIPTION_ID")
	}
	if subID == "" {
		return errors.New("--subscription-id is required")
	}

	tenantID := flagTplTenantID
	if tenantID == "" {
		tenantID = os.Getenv("AZURE_TENANT_ID")
	}

	clientSecret := flagTplCredentials
	if clientSecret == "" {
		clientSecret = os.Getenv("AZURE_CLIENT_SECRET")
	}

	opts := forge.AzureCredentialTemplateOptions{
		Name:           name,
		Description:    flagTplDescription,
		SubscriptionID: subID,
		TenantID:       tenantID,
		Credentials:    clientSecret,
		IsDefault:      flagTplDefault,
	}

	tpl, err := forge.CreateAzureCredentialTemplate(ctx, restURL, creds, opts)
	if err != nil {
		return fmt.Errorf("create Azure credential template: %w", err)
	}

	fmt.Printf("✓ Saved Azure Cloud Credential Template in BNK-Forge:\n")
	fmt.Printf("  ID:              %d\n", tpl.ID)
	fmt.Printf("  Name:            %s\n", tpl.Name)
	fmt.Printf("  Provider:        %s\n", tpl.Provider)
	fmt.Printf("  Subscription ID: %s\n", tpl.SubscriptionID)
	return nil
}

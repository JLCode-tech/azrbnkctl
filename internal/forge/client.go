package forge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/JLCode-tech/azrbnkctl/internal/forge/mcp"
)

// DefaultMCPURL is the localhost endpoint for the forge MCP server's
// Streamable HTTP transport, matching FastMCP's default.
const DefaultMCPURL = "http://localhost:8081/mcp/"

// DefaultForgeRESTURL is the fallback REST base when forge.url is not set.
const DefaultForgeRESTURL = "http://localhost:8000"

// DefaultTimeout is the per-MCP-call timeout.
const DefaultTimeout = 30 * time.Second

// Client is the high-level forge API azrbnkctl uses. It wraps the
// low-level MCP client with typed Go signatures for each tool we touch.
type Client struct {
	mcp *mcp.Client
	url string
}

// NewClient constructs a Client targeting the given MCP endpoint.
// Pass "" to use [DefaultMCPURL], or the AZRBNKCTL_FORGE_MCP_URL env var when set.
func NewClient(endpoint string) *Client {
	if endpoint == "" {
		endpoint = os.Getenv("AZRBNKCTL_FORGE_MCP_URL")
	}
	if endpoint == "" {
		endpoint = DefaultMCPURL
	}
	return &Client{
		mcp: mcp.New(endpoint, DefaultTimeout),
		url: endpoint,
	}
}

// URL reports the MCP endpoint the client targets.
func (c *Client) URL() string { return c.url }

// SystemHealth pings forge's system_health MCP tool.
func (c *Client) SystemHealth(ctx context.Context) (string, error) {
	r, err := c.mcp.CallTool(ctx, "system_health", nil)
	if err != nil {
		return "", err
	}
	return r.Text(), nil
}

// SystemVersion returns the forge version string.
func (c *Client) SystemVersion(ctx context.Context) (string, error) {
	r, err := c.mcp.CallTool(ctx, "system_version", nil)
	if err != nil {
		return "", err
	}
	return r.Text(), nil
}

// CreateProjectRequest mirrors the create_project MCP tool's argument shape.
type CreateProjectRequest struct {
	Name                  string `json:"name"`
	Description           string `json:"description,omitempty"`
	ProjectType           string `json:"project_type,omitempty"`   // e.g. "cloud-azure"
	CloudProvider         string `json:"cloud_provider,omitempty"` // e.g. "azure"
	Environment           string `json:"environment,omitempty"`    // default "dev"
	Region                string `json:"region,omitempty"`
	BackendType           string `json:"backend_type,omitempty"` // default "local"
	Color                 string `json:"color,omitempty"`
	Icon                  string `json:"icon,omitempty"`
	CredentialTemplateID  int    `json:"credential_template_id,omitempty"`
	TargetPlatformProfile string `json:"target_platform_profile,omitempty"`
}

// CreateProjectResponse captures the fields azrbnkctl actually reads.
type CreateProjectResponse struct {
	Project struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// UnmarshalJSON handles flat and nested envelopes from FastMCP.
func (r *CreateProjectResponse) UnmarshalJSON(b []byte) error {
	type alias CreateProjectResponse
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = CreateProjectResponse(a)

	var flat struct {
		ProjectID int    `json:"project_id"`
		ID        int    `json:"id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(b, &flat); err == nil {
		if flat.ProjectID > 0 {
			r.Project.ID = flat.ProjectID
		} else if flat.ID > 0 {
			r.Project.ID = flat.ID
		}
		if flat.Name != "" {
			r.Project.Name = flat.Name
		}
	}
	return nil
}

// CreateProject creates a project in forge.
func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (CreateProjectResponse, error) {
	args := map[string]any{
		"name": req.Name,
	}
	if req.Description != "" {
		args["description"] = req.Description
	}
	if req.ProjectType != "" {
		args["project_type"] = req.ProjectType
	} else {
		args["project_type"] = "cloud-azure"
	}
	if req.CloudProvider != "" {
		args["cloud_provider"] = req.CloudProvider
	} else {
		args["cloud_provider"] = "azure"
	}
	if req.Environment != "" {
		args["environment"] = req.Environment
	} else {
		args["environment"] = "dev"
	}
	if req.Region != "" {
		args["region"] = req.Region
	}
	if req.BackendType != "" {
		args["backend_type"] = req.BackendType
	}
	if req.Color != "" {
		args["color"] = req.Color
	}
	if req.Icon != "" {
		args["icon"] = req.Icon
	}
	if req.CredentialTemplateID > 0 {
		args["credential_template_id"] = req.CredentialTemplateID
	}
	if req.TargetPlatformProfile != "" {
		args["target_platform_profile"] = req.TargetPlatformProfile
	}

	res, err := c.mcp.CallTool(ctx, "create_project", args)
	if err != nil {
		return CreateProjectResponse{}, err
	}

	var out CreateProjectResponse
	if err := json.Unmarshal([]byte(res.Text()), &out); err != nil {
		return CreateProjectResponse{}, fmt.Errorf("decode create_project: %w (raw: %s)", err, res.Text())
	}
	var errCheck struct {
		Ok     *bool  `json:"ok"`
		Error  any    `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal([]byte(res.Text()), &errCheck)
	if (errCheck.Ok != nil && !*errCheck.Ok) || (errCheck.Error != nil && out.Project.ID == 0) || out.Project.ID == 0 {
		return CreateProjectResponse{}, fmt.Errorf("create_project failed: %s", res.Text())
	}
	return out, nil
}

// DeleteProject deletes a project in forge.
func (c *Client) DeleteProject(ctx context.Context, projectID int, force bool) error {
	_, err := c.mcp.CallTool(ctx, "delete_project", map[string]any{
		"project_id": projectID,
		"force":      force,
	})
	return err
}

// CreateClusterRequest mirrors the create_cluster tool arguments.
type CreateClusterRequest struct {
	ProjectID     int    `json:"project_id"`
	Name          string `json:"name"`
	CloudProvider string `json:"cloud_provider"`
	Region        string `json:"region"`
}

// CreateClusterResponse is what create_cluster returns.
type CreateClusterResponse struct {
	Cluster struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		ProjectID int    `json:"project_id"`
	} `json:"cluster"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// UnmarshalJSON handles flat and nested envelopes.
func (r *CreateClusterResponse) UnmarshalJSON(b []byte) error {
	type alias CreateClusterResponse
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = CreateClusterResponse(a)

	var flat struct {
		ClusterID int    `json:"cluster_id"`
		ID        int    `json:"id"`
		Name      string `json:"name"`
		ProjectID int    `json:"project_id"`
	}
	if err := json.Unmarshal(b, &flat); err == nil {
		if flat.ClusterID > 0 {
			r.Cluster.ID = flat.ClusterID
		} else if flat.ID > 0 {
			r.Cluster.ID = flat.ID
		}
		if flat.Name != "" {
			r.Cluster.Name = flat.Name
		}
		if flat.ProjectID > 0 {
			r.Cluster.ProjectID = flat.ProjectID
		}
	}
	return nil
}

// CreateCluster registers an AKS cluster with forge.
func (c *Client) CreateCluster(ctx context.Context, req CreateClusterRequest, kubeconfig []byte) (CreateClusterResponse, error) {
	provider := req.CloudProvider
	if provider == "" {
		provider = "azure"
	}
	args := map[string]any{
		"project_id":     req.ProjectID,
		"name":           req.Name,
		"cloud_provider": provider,
		"region":         req.Region,
		"kubeconfig":     base64.StdEncoding.EncodeToString(kubeconfig),
	}

	res, err := c.mcp.CallTool(ctx, "create_cluster", args)
	if err != nil {
		return CreateClusterResponse{}, err
	}

	var out CreateClusterResponse
	if err := json.Unmarshal([]byte(res.Text()), &out); err != nil {
		return CreateClusterResponse{}, fmt.Errorf("decode create_cluster: %w (raw: %s)", err, res.Text())
	}
	var errCheck struct {
		Ok     *bool  `json:"ok"`
		Error  any    `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal([]byte(res.Text()), &errCheck)
	if (errCheck.Ok != nil && !*errCheck.Ok) || (errCheck.Error != nil && out.Cluster.ID == 0) || out.Cluster.ID == 0 {
		return CreateClusterResponse{}, fmt.Errorf("create_cluster failed: %s", res.Text())
	}
	return out, nil
}

// GetCluster reads back cluster details from forge.
func (c *Client) GetCluster(ctx context.Context, clusterID int) (string, error) {
	res, err := c.mcp.CallTool(ctx, "get_cluster", map[string]any{
		"cluster_id": clusterID,
	})
	if err != nil {
		return "", err
	}
	return res.Text(), nil
}

// ScanCluster triggers an active in-cluster scan forge-side.
func (c *Client) ScanCluster(ctx context.Context, clusterID int) (string, error) {
	res, err := c.mcp.CallTool(ctx, "scan_cluster", map[string]any{
		"cluster_id": clusterID,
	})
	if err != nil {
		return "", err
	}
	return res.Text(), nil
}

// BNKHealth calls forge's bnk_health MCP tool.
func (c *Client) BNKHealth(ctx context.Context, clusterID int) (string, error) {
	res, err := c.mcp.CallTool(ctx, "bnk_health", map[string]any{
		"cluster_id": clusterID,
	})
	if err != nil {
		return "", err
	}
	return res.Text(), nil
}

// DeleteCluster unregisters a cluster from forge.
func (c *Client) DeleteCluster(ctx context.Context, clusterID int) error {
	_, err := c.mcp.CallTool(ctx, "delete_cluster", map[string]any{
		"cluster_id": clusterID,
	})
	return err
}

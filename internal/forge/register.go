package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// RegisterRequest carries everything `forge register` needs from the caller.
type RegisterRequest struct {
	WorkspaceName        string
	WorkspaceDir         string
	ProjectName          string
	ClusterName          string
	Region               string
	Kubeconfig           []byte
	CredentialTemplateID int
	PostRegisterScan     bool
}

// RegisterResult is what forge register returns to the caller for display.
type RegisterResult struct {
	Link        *Link
	ScanOutput  string
	HealthCheck string
	ForgeURL    string
}

// Register executes the azrbnkctl→forge handoff:
//  1. Reuse an existing link if one exists and is registered.
//  2. Otherwise: create_project → create_cluster.
//  3. Optionally: scan_cluster + bnk_health smoke check.
//  4. Write forge_link.json so status and unregister work.
func Register(ctx context.Context, c *Client, req RegisterRequest) (RegisterResult, error) {
	if req.WorkspaceName == "" {
		return RegisterResult{}, errors.New("forge.Register: workspace name is required")
	}
	if req.WorkspaceDir == "" {
		return RegisterResult{}, errors.New("forge.Register: workspace dir is required")
	}
	if req.ClusterName == "" {
		return RegisterResult{}, errors.New("forge.Register: cluster name is required")
	}
	if len(req.Kubeconfig) == 0 {
		return RegisterResult{}, errors.New("forge.Register: kubeconfig is empty")
	}
	if req.ProjectName == "" {
		req.ProjectName = defaultProjectName(req.WorkspaceName)
	}

	// Idempotency: existing link wins when fully registered.
	if existing, err := ReadLink(req.WorkspaceDir); err == nil {
		if existing.IsRegistered() {
			if _, gerr := c.GetCluster(ctx, existing.ClusterID); gerr != nil {
				return RegisterResult{Link: existing, ForgeURL: c.URL()},
					fmt.Errorf("workspace already linked to forge cluster_id=%d but get_cluster failed: %w",
						existing.ClusterID, gerr)
			}
			return RegisterResult{Link: existing, ForgeURL: c.URL()}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RegisterResult{}, fmt.Errorf("read existing link: %w", err)
	}

	// 1) create project
	proj, err := c.CreateProject(ctx, CreateProjectRequest{
		Name:                 req.ProjectName,
		ProjectType:          "cloud-azure",
		CloudProvider:        "azure",
		Region:               req.Region,
		Environment:          "dev",
		Description:          fmt.Sprintf("Created by azrbnkctl for workspace %q", req.WorkspaceName),
		CredentialTemplateID: req.CredentialTemplateID,
	})
	if err != nil {
		return RegisterResult{}, fmt.Errorf("create_project: %w", err)
	}

	// 2) create cluster
	cluster, err := c.CreateCluster(ctx, CreateClusterRequest{
		ProjectID:     proj.Project.ID,
		Name:          req.ClusterName,
		CloudProvider: "azure",
		Region:        req.Region,
	}, req.Kubeconfig)
	if err != nil {
		_ = c.DeleteProject(ctx, proj.Project.ID, true)
		return RegisterResult{}, fmt.Errorf("create_cluster: %w", err)
	}

	link := &Link{
		ForgeMCPURL:  c.URL(),
		ProjectID:    proj.Project.ID,
		ProjectName:  proj.Project.Name,
		ClusterID:    cluster.Cluster.ID,
		ClusterName:  cluster.Cluster.Name,
		RegisteredAt: time.Now().UTC(),
		Workspace:    req.WorkspaceName,
		Status:       "registered",
	}
	if err := WriteLink(req.WorkspaceDir, link); err != nil {
		return RegisterResult{Link: link, ForgeURL: c.URL()},
			fmt.Errorf("registration succeeded but writing forge_link.json failed: %w", err)
	}

	out := RegisterResult{Link: link, ForgeURL: c.URL()}

	// 3) optional post-register scan + smoke check
	if req.PostRegisterScan {
		if s, err := c.ScanCluster(ctx, cluster.Cluster.ID); err != nil {
			out.ScanOutput = "<scan_cluster failed: " + err.Error() + ">"
		} else {
			out.ScanOutput = s
		}
		if h, err := c.BNKHealth(ctx, cluster.Cluster.ID); err != nil {
			out.HealthCheck = "<bnk_health failed: " + err.Error() + ">"
		} else {
			out.HealthCheck = h
		}
	}

	return out, nil
}

// Unregister tears down the forge-side registration for a workspace.
func Unregister(ctx context.Context, c *Client, workspaceDir string, purge bool) error {
	link, err := ReadLink(workspaceDir)
	if err != nil {
		return err
	}

	if err := c.DeleteCluster(ctx, link.ClusterID); err != nil {
		if !is404(err) {
			return fmt.Errorf("delete_cluster: %w", err)
		}
	}
	if purge {
		if err := c.DeleteProject(ctx, link.ProjectID, true); err != nil && !is404(err) {
			return fmt.Errorf("delete_project: %w", err)
		}
	}
	if err := RemoveLink(workspaceDir); err != nil {
		return fmt.Errorf("remove forge_link.json: %w", err)
	}
	return nil
}

// StatusResult is what `forge status` returns.
type StatusResult struct {
	Link          *Link
	Reachable     bool
	ForgeVersion  string
	ClusterDetail string
	Err           error
}

// Status returns the current state of the workspace's forge link.
func Status(ctx context.Context, c *Client, workspaceDir string) (StatusResult, error) {
	link, err := ReadLink(workspaceDir)
	if err != nil {
		return StatusResult{}, err
	}
	out := StatusResult{Link: link}

	if v, err := c.SystemVersion(ctx); err == nil {
		out.Reachable = true
		out.ForgeVersion = v
	} else {
		out.Err = fmt.Errorf("forge unreachable at %s: %w", c.URL(), err)
		return out, nil
	}
	if d, err := c.GetCluster(ctx, link.ClusterID); err == nil {
		out.ClusterDetail = d
	} else {
		out.Err = fmt.Errorf("get_cluster(%d): %w", link.ClusterID, err)
	}
	return out, nil
}

func is404(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not_found") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "http 404") ||
		strings.Contains(s, "\"status_code\": 404")
}

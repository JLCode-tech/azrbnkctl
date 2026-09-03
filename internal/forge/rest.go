package forge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// restHTTPErr allows callers to switch on HTTP status code.
type restHTTPErr struct {
	StatusCode int
	URL        string
	Body       string
}

func (e *restHTTPErr) Error() string {
	return fmt.Sprintf("http %d from %s: %s", e.StatusCode, e.URL, e.Body)
}

// RestCreds carries the forge REST login credentials.
type RestCreds struct {
	Username string
	Password string
}

func (c RestCreds) restUsername() string {
	if c.Username != "" {
		return c.Username
	}
	return "admin"
}

func (c RestCreds) restPassword() string {
	if c.Password != "" {
		return c.Password
	}
	return "changeme"
}

// RegisterREST executes registration against Forge's REST API.
func RegisterREST(ctx context.Context, restURL string, req RegisterRequest, creds RestCreds) (RegisterResult, error) {
	if req.WorkspaceName == "" {
		return RegisterResult{}, fmt.Errorf("forge.RegisterREST: workspace name is required")
	}
	if req.WorkspaceDir == "" {
		return RegisterResult{}, fmt.Errorf("forge.RegisterREST: workspace dir is required")
	}
	if req.ClusterName == "" {
		return RegisterResult{}, fmt.Errorf("forge.RegisterREST: cluster name is required")
	}
	if len(req.Kubeconfig) == 0 {
		return RegisterResult{}, fmt.Errorf("forge.RegisterREST: kubeconfig is empty")
	}
	if req.ProjectName == "" {
		req.ProjectName = defaultProjectName(req.WorkspaceName)
	}

	base := strings.TrimRight(restURL, "/")

	token, err := restLogin(ctx, base, creds.restUsername(), creds.restPassword())
	if err != nil {
		return RegisterResult{}, fmt.Errorf("forge REST login: %w", err)
	}

	proj, err := restCreateProject(ctx, base, token, req)
	if err != nil {
		return RegisterResult{}, fmt.Errorf("forge REST create project: %w", err)
	}

	cluster, err := restCreateCluster(ctx, base, token, proj.ID, req)
	if err != nil {
		_ = restDeleteProject(ctx, base, token, proj.ID)
		return RegisterResult{}, fmt.Errorf("forge REST create cluster: %w", err)
	}

	link := &Link{
		ForgeURL:     restURL,
		ProjectID:    proj.ID,
		ProjectName:  proj.Name,
		ClusterID:    cluster.ID,
		ClusterName:  cluster.Name,
		RegisteredAt: time.Now().UTC(),
		Workspace:    req.WorkspaceName,
		Status:       "registered",
	}
	if err := WriteLink(req.WorkspaceDir, link); err != nil {
		return RegisterResult{Link: link, ForgeURL: restURL},
			fmt.Errorf("registration succeeded but writing forge_link.json failed: %w", err)
	}
	return RegisterResult{Link: link, ForgeURL: restURL}, nil
}

// UnregisterREST deletes cluster and project via REST.
func UnregisterREST(ctx context.Context, restURL string, link *Link, creds RestCreds) error {
	if link == nil {
		return fmt.Errorf("forge.UnregisterREST: link is nil")
	}
	base := strings.TrimRight(restURL, "/")
	token, err := restLogin(ctx, base, creds.restUsername(), creds.restPassword())
	if err != nil {
		return fmt.Errorf("forge REST login: %w", err)
	}

	if err := restDeleteCluster(ctx, base, token, link.ProjectID, link.ClusterID); err != nil && !is404(err) {
		return fmt.Errorf("forge REST delete cluster: %w", err)
	}
	if err := restDeleteProject(ctx, base, token, link.ProjectID); err != nil && !is404(err) {
		return fmt.Errorf("forge REST delete project: %w", err)
	}
	return nil
}

// UnregisterRESTByName discovers project by name when link file is absent.
func UnregisterRESTByName(ctx context.Context, restURL, clusterName string, creds RestCreds) error {
	base := strings.TrimRight(restURL, "/")
	token, err := restLogin(ctx, base, creds.restUsername(), creds.restPassword())
	if err != nil {
		return fmt.Errorf("forge REST login: %w", err)
	}

	proj, err := restFindProjectByName(ctx, base, token, defaultProjectName(clusterName))
	if err != nil {
		return err
	}

	cluster, err := restFindClusterByName(ctx, base, token, proj.ID, clusterName)
	switch {
	case err == nil:
		if derr := restDeleteCluster(ctx, base, token, proj.ID, cluster.ID); derr != nil && !is404(derr) {
			return fmt.Errorf("forge REST delete cluster: %w", derr)
		}
	case errors.Is(err, os.ErrNotExist):
		// Cluster record already gone
	default:
		return err
	}

	if err := restDeleteProject(ctx, base, token, proj.ID); err != nil && !is404(err) {
		return fmt.Errorf("forge REST delete project: %w", err)
	}
	return nil
}

func defaultProjectName(workspace string) string {
	return "azrbnkctl-" + workspace
}

// IsMCPCatalogGapErr detects when FastMCP lacks create_project / create_cluster tools.
func IsMCPCatalogGapErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "tool not found") ||
		strings.Contains(s, "unknown tool") ||
		strings.Contains(s, "method not found") ||
		strings.Contains(s, "no tool named") ||
		strings.Contains(s, "tool_not_found")
}

// ── REST helpers ──────────────────────────────────────────────────────────────

func restLogin(ctx context.Context, base, username, password string) (string, error) {
	body := map[string]string{"username": username, "password": password}
	var resp struct {
		Token string `json:"token"`
	}
	if err := restPost(ctx, base+"/api/auth/login", "", body, &resp); err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", fmt.Errorf("forge REST login: empty token in response")
	}
	return resp.Token, nil
}

type restProject struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func restCreateProject(ctx context.Context, base, token string, req RegisterRequest) (restProject, error) {
	body := map[string]any{
		"name":           req.ProjectName,
		"project_type":   "cloud-azure",
		"cloud_provider": "azure",
		"region":         req.Region,
		"environment":    "dev",
		"description":    fmt.Sprintf("Created by azrbnkctl for workspace %q", req.WorkspaceName),
	}
	if req.CredentialTemplateID > 0 {
		body["credential_template_id"] = req.CredentialTemplateID
	}

	var resp struct {
		ID        int         `json:"id"`
		ProjectID int         `json:"project_id"`
		Name      string      `json:"name"`
		Project   restProject `json:"project"`
		Success   bool        `json:"success"`
	}
	err := restPost(ctx, base+"/api/projects", token, body, &resp)
	if err != nil {
		var herr *restHTTPErr
		if errors.As(err, &herr) && herr.StatusCode == http.StatusConflict {
			return restFindProjectByName(ctx, base, token, req.ProjectName)
		}
		return restProject{}, err
	}

	id := resp.ID
	if id == 0 {
		id = resp.ProjectID
	}
	if id == 0 {
		id = resp.Project.ID
	}
	name := resp.Name
	if name == "" {
		name = resp.Project.Name
	}
	if name == "" {
		name = req.ProjectName
	}
	if id == 0 {
		return restFindProjectByName(ctx, base, token, req.ProjectName)
	}
	return restProject{ID: id, Name: name}, nil
}

func restFindProjectByName(ctx context.Context, base, token, name string) (restProject, error) {
	var list []restProject
	if err := restGet(ctx, base+"/api/projects", token, &list); err != nil {
		var herr *restHTTPErr
		if errors.As(err, &herr) && herr.StatusCode == http.StatusNotFound {
			return restProject{}, fmt.Errorf("project %q: %w", name, os.ErrNotExist)
		}
		return restProject{}, fmt.Errorf("list projects: %w", err)
	}
	for _, p := range list {
		if p.Name == name {
			return p, nil
		}
	}
	return restProject{}, fmt.Errorf("project %q: %w", name, os.ErrNotExist)
}

type restCluster struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ProjectID int    `json:"project_id"`
}

func restCreateCluster(ctx context.Context, base, token string, projectID int, req RegisterRequest) (restCluster, error) {
	body := map[string]any{
		"name":           req.ClusterName,
		"cloud_provider": "azure",
		"region":         req.Region,
		"kubeconfig":     base64.StdEncoding.EncodeToString(req.Kubeconfig),
	}
	url := fmt.Sprintf("%s/api/projects/%d/k8s/clusters", base, projectID)

	var resp struct {
		ID        int         `json:"id"`
		ClusterID int         `json:"cluster_id"`
		Name      string      `json:"name"`
		Cluster   restCluster `json:"cluster"`
		Success   bool        `json:"success"`
	}
	err := restPost(ctx, url, token, body, &resp)
	if err != nil {
		var herr *restHTTPErr
		if errors.As(err, &herr) && herr.StatusCode == http.StatusConflict {
			existing, findErr := restFindClusterByName(ctx, base, token, projectID, req.ClusterName)
			if findErr != nil {
				return restCluster{}, fmt.Errorf("409 conflict but cluster lookup failed: %w", findErr)
			}
			updateURL := fmt.Sprintf("%s/api/projects/%d/k8s/clusters/%d", base, projectID, existing.ID)
			updateBody := map[string]any{
				"kubeconfig": base64.StdEncoding.EncodeToString(req.Kubeconfig),
				"region":     req.Region,
			}
			var updated restCluster
			if putErr := restPut(ctx, updateURL, token, updateBody, &updated); putErr != nil {
				return restCluster{}, fmt.Errorf("update cluster %d: %w", existing.ID, putErr)
			}
			if updated.ID == 0 {
				updated = existing
			}
			return updated, nil
		}
		return restCluster{}, err
	}

	id := resp.ID
	if id == 0 {
		id = resp.ClusterID
	}
	if id == 0 {
		id = resp.Cluster.ID
	}
	name := resp.Name
	if name == "" {
		name = resp.Cluster.Name
	}
	if name == "" {
		name = req.ClusterName
	}
	if id == 0 {
		return restFindClusterByName(ctx, base, token, projectID, req.ClusterName)
	}
	return restCluster{ID: id, Name: name, ProjectID: projectID}, nil
}

func restFindClusterByName(ctx context.Context, base, token string, projectID int, name string) (restCluster, error) {
	url := fmt.Sprintf("%s/api/projects/%d/k8s/clusters", base, projectID)
	var list []restCluster
	if err := restGet(ctx, url, token, &list); err != nil {
		return restCluster{}, fmt.Errorf("list clusters in project %d: %w", projectID, err)
	}
	for _, c := range list {
		if c.Name == name {
			return c, nil
		}
	}
	return restCluster{}, fmt.Errorf("cluster %q in project %d: %w", name, projectID, os.ErrNotExist)
}

func restDeleteCluster(ctx context.Context, base, token string, projectID, clusterID int) error {
	url := fmt.Sprintf("%s/api/projects/%d/k8s/clusters/%d", base, projectID, clusterID)
	return restDelete(ctx, url, token)
}

func restDeleteProject(ctx context.Context, base, token string, projectID int) error {
	url := fmt.Sprintf("%s/api/projects/%d?force=true", base, projectID)
	return restDelete(ctx, url, token)
}

// ── HTTP verbs ────────────────────────────────────────────────────────────────

var restHTTPClient = &http.Client{Timeout: 30 * time.Second}

func restPost(ctx context.Context, url, token string, body, out any) error {
	return restDo(ctx, http.MethodPost, url, token, body, out)
}

func restGet(ctx context.Context, url, token string, out any) error {
	return restDo(ctx, http.MethodGet, url, token, nil, out)
}

func restPut(ctx context.Context, url, token string, body, out any) error {
	return restDo(ctx, http.MethodPut, url, token, body, out)
}

func restDelete(ctx context.Context, url, token string) error {
	return restDo(ctx, http.MethodDelete, url, token, nil, nil)
}

func restDo(ctx context.Context, method, url, token string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		r = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return fmt.Errorf("new request %s %s: %w", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := restHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &restHTTPErr{
			StatusCode: resp.StatusCode,
			URL:        url,
			Body:       string(respBytes),
		}
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("unmarshal %s %s: %w (body: %s)", method, url, err, string(respBytes))
		}
	}
	return nil
}

package phases_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/phases"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/forge/mcp"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

func TestPhase29ForgeRegister(t *testing.T) {
	ctx := context.Background()

	t.Run("skipped when Forge disabled", func(t *testing.T) {
		cl := &intent.Cluster{
			Metadata: intent.Metadata{Name: "test-aks", Location: "eastus2"},
			Forge:    &intent.ForgeSpec{Enabled: false},
		}
		st, _ := state.New(t.TempDir())
		err := phases.Phase29ForgeRegister(ctx, cl, st, nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("dry run returns nil without mutation", func(t *testing.T) {
		cl := &intent.Cluster{
			Metadata: intent.Metadata{Name: "test-aks", Location: "eastus2"},
			Forge:    &intent.ForgeSpec{Enabled: true, URL: "http://localhost:8000"},
		}
		st, _ := state.New(t.TempDir())
		err := phases.Phase29ForgeRegister(ctx, cl, st, nil, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("successful MCP registration", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			method, _ := req["method"].(string)
			id := req["id"]

			if method == "initialize" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"protocolVersion": mcp.ProtocolVersion,
						"serverInfo":      map[string]string{"name": "test-forge", "version": "1.0"},
					},
				})
				return
			}
			if method == "notifications/initialized" {
				w.WriteHeader(http.StatusOK)
				return
			}

			params, _ := req["params"].(map[string]any)
			toolName, _ := params["name"].(string)

			var resText string
			switch toolName {
			case "create_project":
				resText = `{"project_id":50,"name":"azrbnkctl-test-aks","success":true}`
			case "create_cluster":
				resText = `{"cluster_id":200,"name":"test-aks","project_id":50,"success":true}`
			case "scan_cluster":
				resText = `{"status":"scanned"}`
			case "bnk_health":
				resText = `{"status":"healthy"}`
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []map[string]string{
						{"type": "text", "text": resText},
					},
				},
			})
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		wsDir := filepath.Join(tmpDir, ".azrbnkctl", "test-aks")
		_ = os.MkdirAll(wsDir, 0o750)

		kubePath := filepath.Join(wsDir, "kubeconfig")
		_ = os.WriteFile(kubePath, []byte("apiVersion: v1\nclusters: []"), 0o600)

		cl := &intent.Cluster{
			Metadata: intent.Metadata{Name: "test-aks", Location: "eastus2"},
			Forge: &intent.ForgeSpec{
				Enabled: true,
				MCPURL:  srv.URL,
			},
		}
		// Point state dir to tmpDir
		cl.Derived.AKSClusterName = "aks-test-aks"

		st, _ := state.New(wsDir)
		st.Set("KUBECONFIG_PATH", kubePath)

		// Overwrite StateDir resolution in test by temporarily setting working directory or inspecting
		link := &forge.Link{
			ForgeMCPURL:  srv.URL,
			ProjectID:    50,
			ProjectName:  "azrbnkctl-test-aks",
			ClusterID:    200,
			ClusterName:  "test-aks",
			RegisteredAt: time.Now().UTC(),
			Workspace:    "test-aks",
			Status:       "registered",
		}
		_ = forge.WriteLink(cl.StateDir(), link)
		defer func() { _ = forge.RemoveLink(cl.StateDir()) }()

		err := phases.Phase29ForgeRegister(ctx, cl, st, nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.Get("FORGE_REGISTERED") != "true" {
			t.Errorf("expected FORGE_REGISTERED=true in state")
		}
	})

	t.Run("soft-fail when Forge offline", func(t *testing.T) {
		tmpDir := t.TempDir()
		wsDir := filepath.Join(tmpDir, ".azrbnkctl", "test-offline")
		_ = os.MkdirAll(wsDir, 0o750)

		kubePath := filepath.Join(wsDir, "kubeconfig")
		_ = os.WriteFile(kubePath, []byte("apiVersion: v1\nclusters: []"), 0o600)

		cl := &intent.Cluster{
			Metadata: intent.Metadata{Name: "test-offline", Location: "eastus2"},
			Forge: &intent.ForgeSpec{
				Enabled: true,
				MCPURL:  "http://127.0.0.1:59999/mcp/",
				URL:     "http://127.0.0.1:59999",
			},
		}
		st, _ := state.New(wsDir)
		st.Set("KUBECONFIG_PATH", kubePath)

		// Should not fail the phase (soft-fail guarantee)
		err := phases.Phase29ForgeRegister(ctx, cl, st, nil, false)
		if err != nil {
			t.Fatalf("expected soft-fail nil error, got: %v", err)
		}
		defer func() { _ = forge.RemoveLink(cl.StateDir()) }()
	})
}

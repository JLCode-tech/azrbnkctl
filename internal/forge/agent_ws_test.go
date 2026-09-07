package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestBenchmarkAgentWorker_LoopAndDispatch(t *testing.T) {
	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var mu sync.Mutex
	var receivedHeartbeats int
	var receivedCompleted map[string]any
	var wsConn *websocket.Conn

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "fake-jwt-token",
			})
		case r.URL.Path == "/api/benchmarks/agents":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     42,
				"name":   "test-agent",
				"status": "connected",
			})
		case strings.HasPrefix(r.URL.Path, "/ws/benchmarks/agents/"):
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("WebSocket upgrade failed: %v", err)
				return
			}
			mu.Lock()
			wsConn = conn
			mu.Unlock()

			go func() {
				defer conn.Close() // #nosec G104
				for {
					_, msg, err := conn.ReadMessage()
					if err != nil {
						return
					}
					var payload map[string]any
					if err := json.Unmarshal(msg, &payload); err != nil {
						continue
					}
					mu.Lock()
					if payload["type"] == "heartbeat" {
						receivedHeartbeats++
					} else if payload["type"] == "run_completed" {
						receivedCompleted = payload
					}
					mu.Unlock()
				}
			}()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runExecuted := make(chan struct{})

	worker, err := NewBenchmarkAgentWorker(AgentWorkerOptions{
		RestURL:           server.URL,
		Creds:             RestCreds{Username: "admin", Password: "changeme"},
		AgentName:         "test-agent",
		Hostname:          "testhost",
		IPAddress:         "10.0.0.1",
		HeartbeatInterval: 50 * time.Millisecond,
		RunHandler: func(ctx context.Context, runID int, config map[string]any) (map[string]any, error) {
			close(runExecuted)
			return map[string]any{
				"throughput_rps": 45.5,
				"mean_ttft_ms":   12.3,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewBenchmarkAgentWorker failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx)
	}()

	// Wait for connection
	time.Sleep(150 * time.Millisecond)
	if !worker.IsConnected() {
		t.Errorf("expected worker to be connected")
	}

	// Dispatch a run from the server
	mu.Lock()
	conn := wsConn
	mu.Unlock()

	if conn == nil {
		t.Fatalf("server did not receive WebSocket connection")
	}

	err = conn.WriteJSON(map[string]any{
		"type":   "run",
		"run_id": 101,
		"config": map[string]any{
			"model": "meta-llama/Llama-3-8B-Instruct",
			"url":   "http://10.10.10.100:8000/v1/chat/completions",
		},
	})
	if err != nil {
		t.Fatalf("server write run command failed: %v", err)
	}

	// Wait for run execution
	select {
	case <-runExecuted:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for run execution callback")
	}

	// Wait for completion message to reach server
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	hbCount := receivedHeartbeats
	completed := receivedCompleted
	mu.Unlock()

	if hbCount == 0 {
		t.Errorf("expected at least 1 heartbeat, got 0")
	}

	if completed == nil {
		t.Fatalf("expected run_completed payload, got nil")
	}
	if completed["run_id"] != float64(101) {
		t.Errorf("expected run_id 101, got %v", completed["run_id"])
	}

	// Cancel context to cleanly shut down
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected error on shutdown: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Errorf("worker did not shut down cleanly")
	}
}

package bnkforge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

func TestBNKForge_Client(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"name":"test-cluster","provider":"aks","status":"ACTIVE"}`))
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL)

	cl := &intent.Cluster{
		Metadata: intent.Metadata{
			Name:           "test-cluster",
			Location:       "eastus2",
			SubscriptionID: "00000000-0000-0000-0000-000000000000",
		},
		Network: intent.Network{
			VNetCIDR: "10.0.0.0/16",
			Subnets: intent.SubnetsSpec{
				External: "10.0.17.0/24",
			},
		},
	}

	payload := BuildPayload(cl, "ACTIVE")
	if err := client.Register(context.Background(), payload); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	st, err := client.GetStatus(context.Background(), "test-cluster")
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if st.Status != "ACTIVE" {
		t.Fatalf("expected ACTIVE status, got %s", st.Status)
	}

	if err := client.Unregister(context.Background(), "test-cluster"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
}

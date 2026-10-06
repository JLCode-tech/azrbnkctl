package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/bnkscan"
)

// A programmed 2.4 cluster with one MCP route, as forge scan reads it.
const scanFixture = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: infras.gateway.k8s.f5.com}
spec: {group: gateway.k8s.f5.com}
---
apiVersion: gateway.k8s.f5.com/v1alpha1
kind: Infra
metadata: {name: infra, namespace: f5-cne-system}
status: {conditions: [{type: Programmed, status: "True"}]}
---
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata: {name: demo-gatewayclass}
spec: {controllerName: f5.com/f5-cne-system-f5-cne-controller}
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: gw, namespace: default}
spec:
  gatewayClassName: demo-gatewayclass
  addresses: [{type: IPAddress, value: 10.0.10.150}]
  listeners: [{name: http, protocol: HTTP, port: 80}]
status: {conditions: [{type: Accepted, status: "True"}, {type: Programmed, status: "True"}]}
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: {name: tools, namespace: default}
spec:
  parentRefs: [{name: gw}]
  hostnames: [tools.demo.internal]
  rules:
  - matches: [{path: {type: PathPrefix, value: /mcp}}]
    backendRefs: [{name: tool-server, port: 8000}]
`

var scanGVRs = map[string]schema.GroupVersionResource{
	"CustomResourceDefinition": bnkscan.CRDGVR,
	"Infra":                    bnkscan.InfraGVR,
	"GatewaySettings":          bnkscan.GatewaySettingsGVR,
	"EgressGateway":            bnkscan.EgressGatewayGVR,
	"SecPolicy":                bnkscan.SecPolicyGVR,
	"NetPolicy":                bnkscan.NetPolicyGVR,
	"F5BigPersistenceProfile":  bnkscan.PersistenceProfileGVR,
	"GatewayClass":             bnkscan.GatewayClassGVR,
	"Gateway":                  bnkscan.GatewayGVR,
	"HTTPRoute":                bnkscan.HTTPRouteGVR,
	"F5BnkGateway":             bnkscan.BnkGatewayGVR,
	"BNKSecPolicy":             bnkscan.BNKSecPolGVR,
	"BNKNetPolicy":             bnkscan.BNKNetPolGVR,
	"F5SPKVlan":                bnkscan.VlanGVR,
	"F5SPKEgress":              bnkscan.EgressGVR,
}

func fakeScanClients(t *testing.T, fixture string, available int32) (dynamic.Interface, kubernetes.Interface) {
	t.Helper()
	s := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{}
	for kind, gvr := range scanGVRs {
		s.AddKnownTypeWithName(gvr.GroupVersion().WithKind(kind), &unstructured.Unstructured{})
		s.AddKnownTypeWithName(gvr.GroupVersion().WithKind(kind+"List"), &unstructured.UnstructuredList{})
		listKinds[gvr] = kind + "List"
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(s, listKinds)
	for _, doc := range strings.Split(fixture, "\n---\n") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatal(err)
		}
		obj := &unstructured.Unstructured{Object: m}
		ri := dyn.Resource(scanGVRs[obj.GetKind()])
		var err error
		if obj.GetNamespace() != "" {
			_, err = ri.Namespace(obj.GetNamespace()).Create(context.Background(), obj, metav1.CreateOptions{})
		} else {
			_, err = ri.Create(context.Background(), obj, metav1.CreateOptions{})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	one := int32(1)
	cs := k8sfake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: bnkscan.DefaultControllerName, Namespace: bnkscan.DefaultControllerNamespace},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: available},
	})
	return dyn, cs
}

func resetForgeScanFlags() {
	flagForgeConfigFile = ""
	flagForgeMCPURL = ""
	flagForgeScanKubeconfig = ""
	flagForgeScanProbe = false
	flagForgeScanBearerEnv = ""
	flagForgeScanRegister = false
	flagForgeScanRemote = false
	flagForgeScanRestURL = ""
	flagForgeScanUser = ""
	flagForgeScanPass = ""
	flagForgeScanCtrlNS = bnkscan.DefaultControllerNamespace
	flagForgeScanTimeout = 30e9
	flagOutput = "text"
}

func withScanSeams(t *testing.T, dyn dynamic.Interface, cs kubernetes.Interface, link *forge.Link) {
	t.Helper()
	origClients, origLink, origProbe := scanClients, forgeScanLink, forgeScanProbe
	scanClients = func(string) (dynamic.Interface, kubernetes.Interface, error) { return dyn, cs, nil }
	forgeScanLink = func(*intent.Cluster) *forge.Link { return link }
	t.Cleanup(func() {
		scanClients, forgeScanLink, forgeScanProbe = origClients, origLink, origProbe
		resetForgeScanFlags()
	})
	resetForgeScanFlags()
}

func TestForgeScanRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, sub := range forgeCmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"scan", "telemetry", "register", "status"} {
		if !names[want] {
			t.Errorf("forge %s not registered (have %v)", want, names)
		}
	}
	for _, flag := range []string{"kubeconfig", "probe", "bearer-env", "register-targets", "remote", "forge-rest-url", "forge-user", "forge-pass", "controller-namespace", "timeout"} {
		if forgeScanCmd.Flags().Lookup(flag) == nil {
			t.Errorf("forge scan --%s missing", flag)
		}
	}
}

func TestForgeScan_TextReadyCluster(t *testing.T) {
	dyn, cs := fakeScanClients(t, scanFixture, 1)
	withScanSeams(t, dyn, cs, nil)

	var out bytes.Buffer
	forgeScanCmd.SetOut(&out)
	if err := runForgeScan(forgeScanCmd, nil); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	for _, want := range []string{
		"BNK API generation: 2.4",
		"controller f5-cne-system/f5-cne-controller: 1/1 available",
		"MCP endpoints (1)",
		"url:         http://10.0.10.150/mcp",
		"hostnames:   tools.demo.internal",
		"ready: every 2.4 readiness check passed",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestForgeScan_NotReadyExitsNonZero(t *testing.T) {
	dyn, cs := fakeScanClients(t, scanFixture, 0)
	withScanSeams(t, dyn, cs, nil)

	var out bytes.Buffer
	forgeScanCmd.SetOut(&out)
	err := runForgeScan(forgeScanCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "0/1 replicas available") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestForgeScan_JSONProbeAndRegisterTargets(t *testing.T) {
	dyn, cs := fakeScanClients(t, scanFixture, 1)
	withScanSeams(t, dyn, cs, &forge.Link{ClusterID: 23, ClusterName: "demo", ProjectID: 1, Status: "registered"})

	// Fake forge REST: login + target create.
	var posted map[string]any
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "t"})
		case r.Method == http.MethodPost && r.URL.Path == forge.BenchmarkTargetEndpoint:
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 5, "name": posted["name"], "cluster_id": 23})
		default:
			http.NotFound(w, r)
		}
	}))
	defer rest.Close()

	var probedURL string
	var probedHost string
	forgeScanProbe = func(_ context.Context, _ *http.Client, url string, headers map[string]string) (bnkscan.ProbeResult, error) {
		probedURL, probedHost = url, headers["Host"]
		if headers["Authorization"] != "Bearer secret-token" {
			t.Errorf("Authorization = %q", headers["Authorization"])
		}
		return bnkscan.ProbeResult{Tools: []bnkscan.ToolDef{{Name: "forecast"}}, SessionID: "s1"}, nil
	}
	t.Setenv("MCP_TOKEN", "secret-token")

	flagOutput = "json"
	flagForgeScanProbe = true
	flagForgeScanBearerEnv = "MCP_TOKEN"
	flagForgeScanRegister = true
	flagForgeScanRestURL = rest.URL
	flagForgeScanUser, flagForgeScanPass = "u", "p"

	var out bytes.Buffer
	forgeScanCmd.SetOut(&out)
	if err := runForgeScan(forgeScanCmd, nil); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if probedURL != "http://10.0.10.150/mcp" || probedHost != "tools.demo.internal" {
		t.Errorf("probe url=%q host=%q", probedURL, probedHost)
	}

	var doc forgeScanOutput
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if doc.Index == nil || doc.Index.Generation != bnkscan.Gen24 || len(doc.Index.MCP) != 1 {
		t.Fatalf("index = %+v", doc.Index)
	}
	if names := doc.Index.MCP[0].ToolNames(); strings.Join(names, ",") != "forecast" {
		t.Errorf("tools from probe = %v", names)
	}
	if len(doc.Targets) != 1 || doc.Targets[0].ID != 5 || doc.Targets[0].Name != "mcp-default-tools" || doc.Targets[0].Error != "" {
		t.Errorf("targets = %+v", doc.Targets)
	}
	if posted["llm_base_url"] != "http://10.0.10.150/mcp" || posted["cluster_id"] != float64(23) {
		t.Errorf("posted = %v", posted)
	}
	tags, _ := posted["tags"].(map[string]any)
	if tags["protocol"] != "mcp" || tags["tools"] != "forecast" || tags["cluster"] != "demo" {
		t.Errorf("tags = %v", tags)
	}
}

func TestForgeScan_RegisterWithoutLinkFails(t *testing.T) {
	dyn, cs := fakeScanClients(t, scanFixture, 1)
	withScanSeams(t, dyn, cs, nil)
	flagForgeScanRegister = true
	var out bytes.Buffer
	forgeScanCmd.SetOut(&out)
	err := runForgeScan(forgeScanCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not registered with Forge") {
		t.Errorf("err = %v", err)
	}
}

func TestForgeScanSummaries(t *testing.T) {
	scan := `{"success":true,"scan_results":{"bnk_install":{"status":"installed","crds":{"groups":["gateway.k8s.f5net.com","k8s.f5net.com"]},"flo":{"version":"v2.21.13-0.0.64"},"tmm":{"pods":1,"running":1},"controller":{"pods":1,"running":1}}}}`
	got := forgeScanSummary(scan)
	if !strings.Contains(got, "api=2.3") || !strings.Contains(got, "bnk upgrade") {
		t.Errorf("scan summary = %q", got)
	}
	if got := forgeScanSummary("<scan_cluster failed: boom>"); got != "<scan_cluster failed: boom>" {
		t.Errorf("raw passthrough = %q", got)
	}
	if got := forgeHealthSummary(`{"overall":"healthy","platform":{"severity":"healthy"},"dataPlane":{"severity":"healthy"}}`); got != "overall=healthy platform=healthy dataPlane=healthy" {
		t.Errorf("health summary = %q", got)
	}
	if got := forgeHealthSummary("<bnk_health failed: x>"); got != "<bnk_health failed: x>" {
		t.Errorf("raw passthrough = %q", got)
	}
}

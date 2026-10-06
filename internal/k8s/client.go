// Package k8s provides Kubernetes client construction, dynamic Server-Side Apply,
// and readiness polling for azrbnkctl.
package k8s

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// InClusterKubeconfigSentinel is the magic value for kubeconfigPath that
// triggers rest.InClusterConfig() lookup. Used when azrbnkctl runs inside
// a cluster (the test --backend k8s probe Job) and gets its credentials
// from the projected service account.
const InClusterKubeconfigSentinel = "in-cluster"

// Client wraps a Kubernetes clientset and the REST config used to build
// it. One Client per command invocation; not safe for concurrent reuse.
type Client struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
}

// Clientset returns the underlying client-go clientset.
func (c *Client) Clientset() *kubernetes.Clientset { return c.clientset }

// RESTConfig returns the rest.Config used to construct the clientset.
func (c *Client) RESTConfig() *rest.Config { return c.config }

// DefaultKubeconfigPath returns the default kubeconfig location ($KUBECONFIG or ~/.kube/config).
func DefaultKubeconfigPath() string {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		for _, p := range filepath.SplitList(v) {
			// #nosec G304 G703 -- reading standard kubeconfig paths
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	def := filepath.Join(home, ".kube", "config")
	if _, err := os.Stat(def); err == nil {
		return def
	}
	return ""
}

// BuildRESTConfig builds a rest.Config from a kubeconfig path or in-cluster sentinel.
func BuildRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == InClusterKubeconfigSentinel {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
		return cfg, nil
	}
	if kubeconfigPath == "" {
		kubeconfigPath = DefaultKubeconfigPath()
	}
	if kubeconfigPath == "" {
		return nil, errors.New("no kubeconfig found: set $KUBECONFIG or configure AKS credentials")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig %s: %w", kubeconfigPath, err)
	}
	return cfg, nil
}

// BuildClientset returns a typed client for core + apps + batch + etc.
func BuildClientset(kubeconfigPath string) (kubernetes.Interface, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}
	return cs, nil
}

// BuildDynamicClient returns a dynamic.Interface for unstructured access.
func BuildDynamicClient(kubeconfigPath string) (dynamic.Interface, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}
	return dc, nil
}

// BuildRESTConfigFromKubeconfig builds a rest.Config from a local kubeconfig file + context.
func BuildRESTConfigFromKubeconfig(kubeconfigPath, contextName string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		kubeconfigPath = DefaultKubeconfigPath()
	}
	if kubeconfigPath == "" {
		return nil, errors.New("k8s: no kubeconfig found")
	}
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	loader.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("k8s: loading kubeconfig %s: %w", kubeconfigPath, err)
	}
	return cfg, nil
}

// NewClientsetFromConfig creates a typed kubernetes.Interface from rest.Config.
func NewClientsetFromConfig(cfg *rest.Config) (kubernetes.Interface, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: creating clientset: %w", err)
	}
	return cs, nil
}

// NewDynamicFromConfig creates a dynamic.Interface from rest.Config.
func NewDynamicFromConfig(cfg *rest.Config) (dynamic.Interface, error) {
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: creating dynamic client: %w", err)
	}
	return dc, nil
}

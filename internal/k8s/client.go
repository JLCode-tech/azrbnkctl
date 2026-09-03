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

// DefaultKubeconfigPath returns the default kubeconfig location ($KUBECONFIG or ~/.kube/config).
func DefaultKubeconfigPath() string {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		for _, p := range filepath.SplitList(v) {
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

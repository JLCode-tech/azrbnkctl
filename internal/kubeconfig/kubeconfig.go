package kubeconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

// BackupSuffix is the extension appended to the original ~/.kube/config during sync.
const BackupSuffix = ".azrbnkctl-bak"

// Config represents a standard Kubernetes kubeconfig structure.
type Config struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	CurrentContext string         `yaml:"current-context"`
	Clusters       []NamedCluster `yaml:"clusters"`
	Contexts       []NamedContext `yaml:"contexts"`
	Users          []NamedUser    `yaml:"users"`
	Preferences    map[string]any `yaml:"preferences,omitempty"`
}

type NamedCluster struct {
	Name    string         `yaml:"name"`
	Cluster ClusterDetails `yaml:"cluster"`
}

type ClusterDetails struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
}

type NamedContext struct {
	Name    string         `yaml:"name"`
	Context ContextDetails `yaml:"context"`
}

type ContextDetails struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type NamedUser struct {
	Name string      `yaml:"name"`
	User UserDetails `yaml:"user"`
}

type UserDetails struct {
	ClientCertificateData string `yaml:"client-certificate-data,omitempty"`
	ClientKeyData         string `yaml:"client-key-data,omitempty"`
	Token                 string `yaml:"token,omitempty"`
}

// FetchAndGenerate retrieves the cluster kubeconfig from AKS.
func FetchAndGenerate(ctx context.Context, cl *intent.Cluster) ([]byte, error) {
	clients, err := azure.NewClientsWithDefault(cl.Metadata.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("init Azure clients: %w", err)
	}

	rgName := cl.Derived.ResourceGroupName
	clusterName := cl.Derived.AKSClusterName

	resp, err := clients.ManagedClusters.ListClusterAdminCredentials(ctx, rgName, clusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch cluster credentials %s: %w", clusterName, err)
	}

	if len(resp.Kubeconfigs) == 0 || resp.Kubeconfigs[0].Value == nil {
		return nil, fmt.Errorf("no kubeconfig returned for cluster %s", clusterName)
	}

	return resp.Kubeconfigs[0].Value, nil
}

// WriteFile writes the kubeconfig bytes to the target path.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return os.WriteFile(path, data, 0o600)
}

// GlobalKubeconfigPath returns the path to ~/.kube/config.
func GlobalKubeconfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// SyncGlobal safely writes data to ~/.kube/config, backing up any existing file.
func SyncGlobal(data []byte) (string, error) {
	globalPath, err := GlobalKubeconfigPath()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	backupPath := globalPath + BackupSuffix

	// If global kubeconfig exists, create a backup if not already present
	if _, err := os.Stat(globalPath); err == nil {
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			orig, err := os.ReadFile(globalPath) // #nosec G304 -- path is standard user home kubeconfig
			if err != nil {
				return "", fmt.Errorf("read existing kubeconfig: %w", err)
			}
			if err := os.WriteFile(backupPath, orig, 0o600); err != nil { // #nosec G304 G703 -- backup path in user home
				return "", fmt.Errorf("write backup %s: %w", backupPath, err)
			}
		}
	}

	if err := WriteFile(globalPath, data); err != nil {
		return "", fmt.Errorf("write global kubeconfig: %w", err)
	}

	return backupPath, nil
}

// RestoreGlobal restores ~/.kube/config from ~/.kube/config.azrbnkctl-bak if it exists.
func RestoreGlobal() (bool, error) {
	globalPath, err := GlobalKubeconfigPath()
	if err != nil {
		return false, fmt.Errorf("get home dir: %w", err)
	}

	backupPath := globalPath + BackupSuffix
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return false, nil
	}

	backupData, err := os.ReadFile(backupPath) // #nosec G304 -- path is standard user home kubeconfig backup
	if err != nil {
		return false, fmt.Errorf("read backup: %w", err)
	}

	if err := os.WriteFile(globalPath, backupData, 0o600); err != nil { // #nosec G304 G703 -- global kubeconfig path in user home
		return false, fmt.Errorf("restore kubeconfig: %w", err)
	}

	_ = os.Remove(backupPath)
	return true, nil
}

// FormatConfig parses and re-encodes kubeconfig data.
func FormatConfig(data []byte) ([]byte, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	return yaml.Marshal(&cfg)
}

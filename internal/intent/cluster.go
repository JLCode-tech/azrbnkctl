// Package intent holds the cluster.yaml schema (azrbnkctl/v1) and loader.
package intent

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/JLCode-tech/azrbnkctl/internal/netcalc"
)

// nameRE enforces the Azure AKS cluster & resource naming rules used as the
// metadata.name pattern: lowercase alphanumeric + hyphens, 2-40 chars.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,38}[a-z0-9]$`)

// vmSizeRE is a sanity check for Azure VM size strings (e.g. Standard_D4s_v5).
var vmSizeRE = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// APIVersion and Kind are the accepted values for cluster.yaml.
const (
	APIVersion = "azrbnkctl/v1"
	Kind       = "Cluster"

	// MinKubernetesVersion is the mandated floor across the *bnkctl family.
	MinKubernetesVersion = "1.32"

	// MaxTestedKubernetesMinor is the highest minor version validated with BNK CRDs.
	MaxTestedKubernetesMinor = 35

	PatternHostDevice    = "host-device"
	PatternExternalOnly  = "external-only"
	PatternDualInterface = "dual-interface"

	DefaultLocation           = "eastus"
	DefaultManifestVersion    = "2.3.2"
	DefaultDeploymentSize     = "Small"
	DefaultTmmCpu             = "4"
	DefaultTmmMemory          = "10Gi"
	DefaultTmmMtu             = 1500
	DefaultSystemVMSize       = "Standard_D4s_v5"
	DefaultTMMVMSize          = "Standard_D8s_v5"
	DefaultNetworkPlugin      = "azure"
	DefaultNetworkPluginMode  = "Overlay"
	DefaultVNetCIDR           = "10.0.0.0/16"
	DefaultSystemSubnetCIDR   = "10.0.0.0/20"
	DefaultPublicSubnetCIDR   = "10.0.16.0/28"
	DefaultExternalSubnetCIDR = "10.0.17.0/24"
	DefaultInternalSubnetCIDR = "10.0.18.0/24"
)

// Metadata defines the cluster name, target region/location, and Azure subscription/resource group.
type Metadata struct {
	Name           string `yaml:"name"`
	Location       string `yaml:"location,omitempty"`
	SubscriptionID string `yaml:"subscriptionId,omitempty"`
	ResourceGroup  string `yaml:"resourceGroup,omitempty"`
}

// SubnetsSpec defines the CIDRs for subnets in the VNet.
type SubnetsSpec struct {
	System   string `yaml:"system"`
	Public   string `yaml:"public,omitempty"`
	External string `yaml:"external"`
	Internal string `yaml:"internal,omitempty"`
}

// Network defines the VNet CIDR and subnet topology.
type Network struct {
	VNetCIDR   string      `yaml:"vnetCidr"`
	Subnets    SubnetsSpec `yaml:"subnets"`
	NATGateway *bool       `yaml:"natGateway,omitempty"`
}

// NodePoolSpec defines an AKS Agent Pool configuration.
type NodePoolSpec struct {
	Name                  string `yaml:"name"`
	VMSize                string `yaml:"vmSize"`
	Count                 int    `yaml:"count"`
	OSDiskSizeGB          int    `yaml:"osDiskSizeGB,omitempty"`
	AcceleratedNetworking *bool  `yaml:"acceleratedNetworking,omitempty"`
}

// ClusterSpec defines the AKS Managed Cluster settings.
type ClusterSpec struct {
	KubernetesVersion string       `yaml:"kubernetesVersion,omitempty"`
	NetworkPlugin     string       `yaml:"networkPlugin,omitempty"`
	NetworkPluginMode string       `yaml:"networkPluginMode,omitempty"`
	SystemNodePool    NodePoolSpec `yaml:"systemNodePool,omitempty"`
	TMMNodePool       NodePoolSpec `yaml:"tmmNodePool,omitempty"`
}

// BnkSpec defines the supply-chain credentials and BNK 2.3.x parameters.
type BnkSpec struct {
	FARArchive      string `yaml:"farArchive"`
	JWT             string `yaml:"jwt"`
	ManifestVersion string `yaml:"manifestVersion,omitempty"`
	DeploymentSize  string `yaml:"deploymentSize,omitempty"`
	TmmCpu          string `yaml:"tmmCpu,omitempty"`
	TmmMemory       string `yaml:"tmmMemory,omitempty"`
	TmmMtu          int    `yaml:"tmmMtu,omitempty"`
	Telemetry       bool   `yaml:"telemetry,omitempty"`
}

// EgressSpec defines egress/NAT settings.
type EgressSpec struct {
	Enabled   bool   `yaml:"enabled"`
	Namespace string `yaml:"namespace,omitempty"`
	Port      int    `yaml:"port,omitempty"`
}

// TestingSpec defines automated testing parameters.
type TestingSpec struct {
	Enabled    bool   `yaml:"enabled"`
	TargetHost string `yaml:"targetHost,omitempty"`
}

// ForgeSpec captures the operator-declared forge integration.
type ForgeSpec struct {
	// Enabled is the master switch. Default false (omitted block = disabled).
	Enabled bool `yaml:"enabled"`
	// URL is the forge REST base. Default http://localhost:8000.
	// Override via AZRBNKCTL_FORGE_URL env (env > yaml > default).
	URL string `yaml:"url,omitempty"`
	// MCPURL is the forge MCP endpoint. Default http://localhost:8081/mcp/.
	MCPURL string `yaml:"mcpUrl,omitempty"`
	// Username is the forge REST login username. Default "admin".
	Username string `yaml:"username,omitempty"`
	// Password is the forge REST login password.
	// Resolution order: AZRBNKCTL_FORGE_PASSWORD env > this field > "changeme".
	Password string `yaml:"password,omitempty"`
	// CredentialTemplateID is the forge credential template to attach to the project.
	CredentialTemplateID int `yaml:"credentialTemplateId,omitempty"`
}

// DefaultForgeRESTURL is the fallback REST base when forge.url is not set.
const DefaultForgeRESTURL = "http://localhost:8000"

// ResolveURL returns the forge REST URL to use, in priority order:
//  1. AZRBNKCTL_FORGE_URL environment variable
//  2. f.URL (cluster.yaml forge.url)
//  3. DefaultForgeRESTURL ("http://localhost:8000")
func (f *ForgeSpec) ResolveURL() string {
	if v := os.Getenv("AZRBNKCTL_FORGE_URL"); v != "" {
		return v
	}
	if f != nil && f.URL != "" {
		return f.URL
	}
	return DefaultForgeRESTURL
}

// ResolveUsername returns the forge REST login username, in priority order:
//  1. f.Username (cluster.yaml forge.username)
//  2. default "admin"
func (f *ForgeSpec) ResolveUsername() string {
	if f != nil && f.Username != "" {
		return f.Username
	}
	return "admin"
}

// ResolvePassword returns the forge REST login password and whether the
// built-in default was used. Resolution order:
//  1. AZRBNKCTL_FORGE_PASSWORD environment variable
//  2. f.Password (cluster.yaml forge.password)
//  3. "changeme" (back-compat default; usingDefault=true signals callers to warn)
func (f *ForgeSpec) ResolvePassword() (password string, usingDefault bool) {
	if v := os.Getenv("AZRBNKCTL_FORGE_PASSWORD"); v != "" {
		return v, false
	}
	if f != nil && f.Password != "" {
		return f.Password, false
	}
	return "changeme", true
}

// Derived holds computed names and IP endpoints derived from cluster.yaml.
type Derived struct {
	ResourceGroupName      string `yaml:"-"`
	VNetName               string `yaml:"-"`
	AKSClusterName         string `yaml:"-"`
	NSGName                string `yaml:"-"`
	SystemSubnetName       string `yaml:"-"`
	PublicSubnetName       string `yaml:"-"`
	ExternalSubnetName     string `yaml:"-"`
	InternalSubnetName     string `yaml:"-"`
	NatGatewayName         string `yaml:"-"`
	NatGatewayPublicIPName string `yaml:"-"`
	PublicIPName           string `yaml:"-"`
	ExternalGatewayIP      string `yaml:"-"`
	InternalGatewayIP      string `yaml:"-"`
	FARArchiveFullPath     string `yaml:"-"`
	JWTFullPath            string `yaml:"-"`
}

// Cluster is the top-level Go representation of cluster.yaml.
type Cluster struct {
	APIVersion  string            `yaml:"apiVersion"`
	Kind        string            `yaml:"kind"`
	Metadata    Metadata          `yaml:"metadata"`
	Pattern     string            `yaml:"pattern"`
	Network     Network           `yaml:"network"`
	ClusterSpec ClusterSpec       `yaml:"cluster,omitempty"`
	Bnk         BnkSpec           `yaml:"bnk"`
	Egress      EgressSpec        `yaml:"egress,omitempty"`
	Testing     TestingSpec       `yaml:"testing,omitempty"`
	Forge       *ForgeSpec        `yaml:"forge,omitempty"`
	Tags        map[string]string `yaml:"tags,omitempty"`

	Derived Derived `yaml:"-"`
}

// Load reads and validates a cluster.yaml file.
func Load(path string) (*Cluster, error) {
	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath) // #nosec G304 -- path provided by operator
	if err != nil {
		return nil, fmt.Errorf("intent: read %s: %w", path, err)
	}

	baseDir := filepath.Dir(cleanPath)
	return LoadBytes(data, baseDir)
}

// LoadBytes decodes and validates YAML bytes with strict unmarshaling.
func LoadBytes(data []byte, baseDir string) (*Cluster, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)

	var c Cluster
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("intent: empty document")
		}
		return nil, fmt.Errorf("intent: decode cluster.yaml: %w", err)
	}

	c.setDefaults()

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("intent: validate cluster.yaml: %w", err)
	}

	if err := c.derive(baseDir); err != nil {
		return nil, fmt.Errorf("intent: derive fields: %w", err)
	}

	return &c, nil
}

func (c *Cluster) setDefaults() {
	if c.Metadata.Location == "" {
		c.Metadata.Location = DefaultLocation
	}
	if c.Metadata.ResourceGroup == "" && c.Metadata.Name != "" {
		c.Metadata.ResourceGroup = fmt.Sprintf("azrbnkctl-%s-rg", c.Metadata.Name)
	}
	if c.Pattern == "" {
		c.Pattern = PatternHostDevice
	}
	if c.Network.VNetCIDR == "" {
		c.Network.VNetCIDR = DefaultVNetCIDR
	}
	if c.Network.Subnets.System == "" {
		c.Network.Subnets.System = DefaultSystemSubnetCIDR
	}
	if c.Network.Subnets.Public == "" {
		c.Network.Subnets.Public = DefaultPublicSubnetCIDR
	}
	if c.Network.Subnets.External == "" {
		c.Network.Subnets.External = DefaultExternalSubnetCIDR
	}
	if c.Pattern == PatternDualInterface && c.Network.Subnets.Internal == "" {
		c.Network.Subnets.Internal = DefaultInternalSubnetCIDR
	}
	if c.Network.NATGateway == nil {
		t := true
		c.Network.NATGateway = &t
	}

	// ClusterSpec defaults
	if c.ClusterSpec.KubernetesVersion == "" {
		c.ClusterSpec.KubernetesVersion = MinKubernetesVersion
	}
	if c.ClusterSpec.NetworkPlugin == "" {
		c.ClusterSpec.NetworkPlugin = DefaultNetworkPlugin
	}
	if c.ClusterSpec.NetworkPluginMode == "" {
		c.ClusterSpec.NetworkPluginMode = DefaultNetworkPluginMode
	}

	if c.ClusterSpec.SystemNodePool.Name == "" {
		c.ClusterSpec.SystemNodePool.Name = "system"
	}
	if c.ClusterSpec.SystemNodePool.VMSize == "" {
		c.ClusterSpec.SystemNodePool.VMSize = DefaultSystemVMSize
	}
	if c.ClusterSpec.SystemNodePool.Count == 0 {
		c.ClusterSpec.SystemNodePool.Count = 2
	}
	if c.ClusterSpec.SystemNodePool.OSDiskSizeGB == 0 {
		c.ClusterSpec.SystemNodePool.OSDiskSizeGB = 128
	}

	if c.ClusterSpec.TMMNodePool.Name == "" {
		c.ClusterSpec.TMMNodePool.Name = "tmm"
	}
	if c.ClusterSpec.TMMNodePool.VMSize == "" {
		c.ClusterSpec.TMMNodePool.VMSize = DefaultTMMVMSize
	}
	if c.ClusterSpec.TMMNodePool.Count == 0 {
		c.ClusterSpec.TMMNodePool.Count = 2
	}
	if c.ClusterSpec.TMMNodePool.OSDiskSizeGB == 0 {
		c.ClusterSpec.TMMNodePool.OSDiskSizeGB = 128
	}
	if c.ClusterSpec.TMMNodePool.AcceleratedNetworking == nil {
		t := true
		c.ClusterSpec.TMMNodePool.AcceleratedNetworking = &t
	}

	// BNK defaults
	if c.Bnk.ManifestVersion == "" {
		c.Bnk.ManifestVersion = DefaultManifestVersion
	}
	if c.Bnk.DeploymentSize == "" {
		c.Bnk.DeploymentSize = DefaultDeploymentSize
	}
	if c.Bnk.TmmCpu == "" {
		c.Bnk.TmmCpu = DefaultTmmCpu
	}
	if c.Bnk.TmmMemory == "" {
		c.Bnk.TmmMemory = DefaultTmmMemory
	}
	if c.Bnk.TmmMtu == 0 {
		c.Bnk.TmmMtu = DefaultTmmMtu
	}

	if c.Egress.Enabled && c.Egress.Namespace == "" {
		c.Egress.Namespace = "egress-demo"
	}
	if c.Egress.Enabled && c.Egress.Port == 0 {
		c.Egress.Port = 4789
	}
}

// Validate checks all schema constraints.
func (c *Cluster) Validate() error {
	var errs []string

	if c.APIVersion != APIVersion {
		errs = append(errs, fmt.Sprintf("apiVersion must be %q, got %q", APIVersion, c.APIVersion))
	}
	if c.Kind != Kind {
		errs = append(errs, fmt.Sprintf("kind must be %q, got %q", Kind, c.Kind))
	}

	if !nameRE.MatchString(c.Metadata.Name) {
		errs = append(errs, fmt.Sprintf("metadata.name %q must match %s (lowercase alphanumeric + hyphens, 2-40 chars)", c.Metadata.Name, nameRE.String()))
	}

	switch c.Pattern {
	case PatternHostDevice, PatternExternalOnly, PatternDualInterface:
	default:
		errs = append(errs, fmt.Sprintf("pattern %q unsupported: must be %q, %q, or %q", c.Pattern, PatternHostDevice, PatternExternalOnly, PatternDualInterface))
	}

	// Validate VNet & Subnet CIDRs
	_, vnetNet, err := net.ParseCIDR(c.Network.VNetCIDR)
	if err != nil {
		errs = append(errs, fmt.Sprintf("network.vnetCidr %q is invalid: %v", c.Network.VNetCIDR, err))
	}

	validateSubnet := func(name, cidr string, required bool) {
		if cidr == "" {
			if required {
				errs = append(errs, fmt.Sprintf("network.subnets.%s is required", name))
			}
			return
		}
		_, subNet, subErr := net.ParseCIDR(cidr)
		if subErr != nil {
			errs = append(errs, fmt.Sprintf("network.subnets.%s %q is invalid: %v", name, cidr, subErr))
			return
		}
		if vnetNet != nil && !vnetNet.Contains(subNet.IP) {
			errs = append(errs, fmt.Sprintf("network.subnets.%s %q must be contained within vnetCidr %q", name, cidr, c.Network.VNetCIDR))
		}
	}

	validateSubnet("system", c.Network.Subnets.System, true)
	validateSubnet("public", c.Network.Subnets.Public, false)
	validateSubnet("external", c.Network.Subnets.External, true)
	if c.Pattern == PatternDualInterface {
		validateSubnet("internal", c.Network.Subnets.Internal, true)
	}

	// Validate Kubernetes Version Floor
	if err := validateK8sVersion(c.ClusterSpec.KubernetesVersion); err != nil {
		errs = append(errs, err.Error())
	}

	// Validate VM sizes
	if !vmSizeRE.MatchString(c.ClusterSpec.SystemNodePool.VMSize) {
		errs = append(errs, fmt.Sprintf("cluster.systemNodePool.vmSize %q is invalid", c.ClusterSpec.SystemNodePool.VMSize))
	}
	if !vmSizeRE.MatchString(c.ClusterSpec.TMMNodePool.VMSize) {
		errs = append(errs, fmt.Sprintf("cluster.tmmNodePool.vmSize %q is invalid", c.ClusterSpec.TMMNodePool.VMSize))
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

func validateK8sVersion(ver string) error {
	parts := strings.Split(ver, ".")
	if len(parts) < 2 {
		return fmt.Errorf("cluster.kubernetesVersion %q must be at least major.minor (e.g. 1.32)", ver)
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return fmt.Errorf("cluster.kubernetesVersion %q contains non-integer components", ver)
	}
	if major < 1 || (major == 1 && minor < 32) {
		return fmt.Errorf("cluster.kubernetesVersion %q is below the mandated floor %s (set 1.32 or newer)", ver, MinKubernetesVersion)
	}
	return nil
}

// StateDir returns the path to the workspace state directory.
func (c *Cluster) StateDir() string {
	return filepath.Join(".azrbnkctl", c.Metadata.Name)
}

// DefaultVIP returns the default Gateway VIP for this cluster.
// Convention: <subnets.external network>.100 — e.g. 10.0.17.0/24 -> 10.0.17.100.
// Returns an error when network.subnets.external is not set.
func (c *Cluster) DefaultVIP() (string, error) {
	if c.Network.Subnets.External == "" {
		return "", errors.New("network.subnets.external not set; pass --vip explicitly")
	}
	return netcalc.Nth(c.Network.Subnets.External, 100)
}

// SyntheticAIEnabled reports whether synthetic/simulated AI inference is configured.
func (c *Cluster) SyntheticAIEnabled() bool {
	return false
}

func (c *Cluster) derive(baseDir string) error {
	c.Derived.ResourceGroupName = c.Metadata.ResourceGroup
	c.Derived.VNetName = fmt.Sprintf("vnet-%s", c.Metadata.Name)
	c.Derived.AKSClusterName = fmt.Sprintf("aks-%s", c.Metadata.Name)
	c.Derived.NSGName = fmt.Sprintf("nsg-%s", c.Metadata.Name)
	c.Derived.SystemSubnetName = "snet-system"
	c.Derived.PublicSubnetName = "snet-public"
	c.Derived.ExternalSubnetName = "snet-external"
	c.Derived.InternalSubnetName = "snet-internal"
	c.Derived.NatGatewayName = fmt.Sprintf("natgw-%s", c.Metadata.Name)
	c.Derived.NatGatewayPublicIPName = fmt.Sprintf("pip-natgw-%s", c.Metadata.Name)
	c.Derived.PublicIPName = fmt.Sprintf("pip-ingress-%s", c.Metadata.Name)

	extGw, err := netcalc.Nth(c.Network.Subnets.External, 1)
	if err != nil {
		return fmt.Errorf("derive external gateway: %w", err)
	}
	c.Derived.ExternalGatewayIP = extGw

	if c.Network.Subnets.Internal != "" {
		intGw, err := netcalc.Nth(c.Network.Subnets.Internal, 1)
		if err != nil {
			return fmt.Errorf("derive internal gateway: %w", err)
		}
		c.Derived.InternalGatewayIP = intGw
	}

	if c.Bnk.FARArchive != "" {
		if filepath.IsAbs(c.Bnk.FARArchive) {
			c.Derived.FARArchiveFullPath = c.Bnk.FARArchive
		} else {
			c.Derived.FARArchiveFullPath = filepath.Join(baseDir, c.Bnk.FARArchive)
		}
	}

	if c.Bnk.JWT != "" {
		if filepath.IsAbs(c.Bnk.JWT) {
			c.Derived.JWTFullPath = c.Bnk.JWT
		} else {
			c.Derived.JWTFullPath = filepath.Join(baseDir, c.Bnk.JWT)
		}
	}

	return nil
}

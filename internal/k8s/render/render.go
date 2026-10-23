// Package render executes text/template on embedded manifests for azrbnkctl.
package render

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

// CNEInstanceData holds parameters for rendering cneinstance.yaml.tmpl.
type CNEInstanceData struct {
	ManifestVersion    string
	DeploymentSize     string
	TmmCpu             string
	TmmMemory          string
	TmmMtu             int
	ExtPCI             string
	IntPCI             string
	Telemetry          bool
	AIGateway          bool
	ImageRegistry      string
	PalCpuSet          string
	NetworkAttachments []string
	SkipNics           string
	TmmK8sRoutes       string
}

// VlansRoutesData holds parameters for rendering vlans-routes.yaml.tmpl.
type VlansRoutesData struct {
	TmmMtu            int
	ExtPrefixLen      int
	ExtSelfIP         string
	ExtGwIP           string
	IntPrefixLen      int
	IntSelfIP         string
	IntGwIP           string
	ServiceCidrDest   string
	ServiceCidrPrefix int
	PodCidrDest       string
	PodCidrPrefix     int
}

// InfraData holds parameters for rendering infra.yaml.tmpl (BNK 2.4).
type InfraData struct {
	TmmMtu            int
	ExtSelfIP         string
	ExtGwIP           string
	IntSelfIP         string
	IntGwIP           string
	ListenerStart     string
	ListenerEnd       string
	ServiceCidrDest   string
	ServiceCidrPrefix int
	PodCidrDest       string
	PodCidrPrefix     int
}

// EgressData holds parameters for rendering egress.yaml.tmpl.
type EgressData struct {
	Namespace        string
	GatewayClassName string
	NetworkRef       string
}

// LicenseData holds parameters for rendering license-cr.yaml.tmpl.
type LicenseData struct {
	JWT string
}

// NADsData holds parameters for rendering nads.yaml.tmpl.
type NADsData struct {
	Namespace string
	ExtPCI    string
	IntPCI    string
}

func renderTemplate(path string, data interface{}) ([]byte, error) {
	tmplBytes, err := manifests.FS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("render: read embedded template %s: %w", path, err)
	}

	tmpl, err := template.New(path).Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("render: parse template %s: %w", path, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render: execute template %s: %w", path, err)
	}

	return buf.Bytes(), nil
}

func normalizeDeploymentSize(s string) string {
	switch strings.ToLower(s) {
	case "small":
		return "Small"
	case "medium":
		return "Medium"
	case "large":
		return "Large"
	case "max":
		return "Max"
	default:
		if s == "" {
			return "Small"
		}
		return s
	}
}

func isAIGatewayManifestVersion(manifestVersion string) bool {
	v := strings.TrimPrefix(strings.TrimSpace(manifestVersion), "v")
	if v == "" {
		return false
	}
	base := strings.SplitN(v, "-", 2)[0]
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if maj > 2 {
		return true
	}
	if maj < 2 {
		return false
	}
	if min > 4 {
		return true
	}
	if min < 4 {
		return false
	}
	if len(parts) < 3 {
		return false
	}
	patch, err3 := strconv.Atoi(parts[2])
	return err3 == nil && patch >= 1
}

// RenderCNEInstance renders the CNEInstance custom resource.
func RenderCNEInstance(data CNEInstanceData) ([]byte, error) {
	data.DeploymentSize = normalizeDeploymentSize(data.DeploymentSize)
	if isAIGatewayManifestVersion(data.ManifestVersion) {
		data.AIGateway = true
	}
	return renderTemplate("templates/cneinstance.yaml.tmpl", data)
}

// RenderLicenseCR renders the License custom resource.
func RenderLicenseCR(jwt string) ([]byte, error) {
	return renderTemplate("templates/license-cr.yaml.tmpl", LicenseData{JWT: jwt})
}

// RenderVlansRoutes renders the F5SPKVlan and F5SPKStaticRoute custom resources.
func RenderVlansRoutes(data VlansRoutesData) ([]byte, error) {
	if data.ServiceCidrDest == "" {
		data.ServiceCidrDest = "10.240.0.0"
	}
	if data.ServiceCidrPrefix == 0 {
		data.ServiceCidrPrefix = 16
	}
	if data.PodCidrDest == "" {
		data.PodCidrDest = "10.244.0.0"
	}
	if data.PodCidrPrefix == 0 {
		data.PodCidrPrefix = 16
	}
	return renderTemplate("templates/vlans-routes.yaml.tmpl", data)
}

// RenderInfra renders the BNK 2.4 Infra custom resource.
func RenderInfra(data InfraData) ([]byte, error) {
	if data.TmmMtu == 0 {
		data.TmmMtu = 1500
	}
	if data.ServiceCidrDest == "" {
		data.ServiceCidrDest = "10.240.0.0"
	}
	if data.ServiceCidrPrefix == 0 {
		data.ServiceCidrPrefix = 16
	}
	if data.PodCidrDest == "" {
		data.PodCidrDest = "10.244.0.0"
	}
	if data.PodCidrPrefix == 0 {
		data.PodCidrPrefix = 16
	}
	if data.ListenerStart == "" {
		data.ListenerStart = "10.0.17.100"
	}
	if data.ListenerEnd == "" {
		data.ListenerEnd = "10.0.17.199"
	}
	return renderTemplate("templates/infra.yaml.tmpl", data)
}

// RenderEgress renders the legacy BNK 2.3 F5SPKEgress custom resource.
func RenderEgress(namespace string) ([]byte, error) {
	return renderTemplate("templates/egress.yaml.tmpl", EgressData{Namespace: namespace})
}

// RenderEgressGateway renders the BNK 2.4 GatewaySettings and EgressGateway custom resources.
func RenderEgressGateway(data EgressData) ([]byte, error) {
	if data.Namespace == "" {
		data.Namespace = "bnk-demo"
	}
	if data.GatewayClassName == "" {
		data.GatewayClassName = "f5-cne-gateway-class"
	}
	if data.NetworkRef == "" {
		data.NetworkRef = InfraIntNetwork
	}
	return renderTemplate("templates/egress.yaml.tmpl", data)
}

// RenderNADs renders the NetworkAttachmentDefinition custom resources.
func RenderNADs(data NADsData) ([]byte, error) {
	return renderTemplate("templates/nads.yaml.tmpl", data)
}

const (
	InfraName         = "infra"
	InfraExtNetwork   = "external"
	InfraIntNetwork   = "internal"
	InfraListenerPool = "listener-pool"
	InfraEgressSubnet = "192.168.0.0/16"
	InfraEgressPort   = 4789
	InfraRouteVNet    = "vnet"
	InfraRouteDefault = "default"
	TMMK8sRoutesEnv   = "TMM_K8S_ROUTES"
)

// InfraTunnelNetwork is the Infra network the egress tunnel terminates on:
// the internal VLAN on dual-interface clusters, the external VLAN when it is the only one.
func InfraTunnelNetwork(hasInternal bool) string {
	if hasInternal {
		return InfraIntNetwork
	}
	return InfraExtNetwork
}

// CNEControllerServiceAccount is the ServiceAccount the F5 cne-controller runs as.
const CNEControllerServiceAccount = "f5-cne-controller"

// CNEControllerRBACName is the ClusterRole/ClusterRoleBinding azrbnkctl adds
// so the controller can read EndpointSlices (see shared/cne-controller-rbac.yaml.tmpl).
func CNEControllerRBACName(cl *intent.Cluster) string {
	return cl.Metadata.Name + "-cne-controller-endpointslices"
}

// CNEControllerRBACVars holds the substitution variables for
// shared/cne-controller-rbac.yaml.tmpl.
type CNEControllerRBACVars struct {
	Name           string // <cluster>-cne-controller-endpointslices
	LabName        string // cl.Metadata.Name
	InstanceNS     string // f5-cne-system
	ServiceAccount string // f5-cne-controller
}

// RenderCNEControllerRBAC renders the EndpointSlice + NAD ClusterRole + binding for
// the cne-controller ServiceAccount.
func RenderCNEControllerRBAC(tmpl []byte, cl *intent.Cluster) ([]byte, error) {
	return RenderCNEControllerRBACNamespace(tmpl, cl, "f5-cne-system")
}

// RenderCNEControllerRBACNamespace renders the RBAC with an explicit namespace.
func RenderCNEControllerRBACNamespace(tmpl []byte, cl *intent.Cluster, ns string) ([]byte, error) {
	if cl == nil || cl.Metadata.Name == "" {
		return nil, fmt.Errorf("render cne-controller rbac: cluster name is required")
	}
	if ns == "" {
		ns = "f5-cne-system"
	}
	return renderTemplateString(string(tmpl), CNEControllerRBACVars{
		Name:           CNEControllerRBACName(cl),
		LabName:        cl.Metadata.Name,
		InstanceNS:     ns,
		ServiceAccount: CNEControllerServiceAccount,
	})
}

func renderTemplateString(tmplStr string, data interface{}) ([]byte, error) {
	t, err := template.New("tmpl").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}
	return buf.Bytes(), nil
}

// FLOValuesVars holds the substitution variables for flo-values.yaml.tmpl.
type FLOValuesVars struct {
	CAIssuer      string // <cluster>-ca-cluster-issuer
	FARSecretName string // far-secret
	JWT           string // raw JWT token contents
	ClusterName   string // cl.Metadata.Name
}

// FLOValuesVarsFromCluster derives the FLO values template variables from the cluster intent.
func FLOValuesVarsFromCluster(cl *intent.Cluster, jwt string) FLOValuesVars {
	return FLOValuesVars{
		CAIssuer:      cl.Metadata.Name + "-ca-cluster-issuer",
		FARSecretName: "far-secret",
		JWT:           jwt,
		ClusterName:   cl.Metadata.Name,
	}
}

// RenderFLOValues renders the FLO values template with vars derived from the cluster intent.
func RenderFLOValues(tmpl []byte, cl *intent.Cluster, jwt string) ([]byte, error) {
	vars := FLOValuesVarsFromCluster(cl, jwt)
	return renderTemplateString(string(tmpl), vars)
}

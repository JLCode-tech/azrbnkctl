// Package render executes text/template on embedded manifests for azrbnkctl.
package render

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

// CNEInstanceData holds parameters for rendering cneinstance.yaml.tmpl.
type CNEInstanceData struct {
	ManifestVersion string
	DeploymentSize  string
	TmmCpu          string
	TmmMemory       string
	TmmMtu          int
	ExtPCI          string
	IntPCI          string
	Telemetry       bool
	ImageRegistry   string
}

// VlansRoutesData holds parameters for rendering vlans-routes.yaml.tmpl.
type VlansRoutesData struct {
	TmmMtu       int
	ExtPrefixLen int
	ExtSelfIP    string
	ExtGwIP      string
	IntPrefixLen int
	IntSelfIP    string
	IntGwIP      string
}

// EgressData holds parameters for rendering egress.yaml.tmpl.
type EgressData struct {
	Namespace string
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

// RenderCNEInstance renders the CNEInstance custom resource.
func RenderCNEInstance(data CNEInstanceData) ([]byte, error) {
	data.DeploymentSize = normalizeDeploymentSize(data.DeploymentSize)
	return renderTemplate("templates/cneinstance.yaml.tmpl", data)
}

// RenderLicenseCR renders the License custom resource.
func RenderLicenseCR(jwt string) ([]byte, error) {
	return renderTemplate("templates/license-cr.yaml.tmpl", LicenseData{JWT: jwt})
}

// RenderVlansRoutes renders the F5SPKVlan and F5SPKStaticRoute custom resources.
func RenderVlansRoutes(data VlansRoutesData) ([]byte, error) {
	return renderTemplate("templates/vlans-routes.yaml.tmpl", data)
}

// RenderEgress renders the F5SPKEgress custom resource.
func RenderEgress(namespace string) ([]byte, error) {
	return renderTemplate("templates/egress.yaml.tmpl", EgressData{Namespace: namespace})
}

// RenderNADs renders the NetworkAttachmentDefinition custom resources.
func RenderNADs(data NADsData) ([]byte, error) {
	return renderTemplate("templates/nads.yaml.tmpl", data)
}

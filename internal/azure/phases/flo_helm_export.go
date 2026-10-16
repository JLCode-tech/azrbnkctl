package phases

import "github.com/JLCode-tech/azrbnkctl/internal/k8s"

const (
	FLOReleaseName        = k8s.FLOReleaseName
	FLONamespace          = k8s.FLONamespace
	FLOChartRef           = k8s.FLOChartRef
	FLOValuesTemplatePath = k8s.FLOValuesTemplatePath
)

// HelmInstaller is the Helm SDK surface phase 32 and bnk upgrade share.
type HelmInstaller = k8s.HelmInstaller

// NewHelmInstaller logs in to repo.f5.com with the FAR key and returns a
// HelmInstaller bound to the FLO namespace.
func NewHelmInstaller(kubeconfigPath, farKeyB64 string) (HelmInstaller, error) {
	return k8s.NewHelmInstaller(kubeconfigPath, farKeyB64)
}

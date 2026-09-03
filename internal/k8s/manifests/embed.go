package manifests

import "embed"

// FS is the embedded manifest filesystem.
//
//go:embed all:static all:cert-manager all:metrics-server all:flo all:templates
var FS embed.FS

package k8s

import "embed"

//go:embed templates/*.yaml
var TemplatesFS embed.FS

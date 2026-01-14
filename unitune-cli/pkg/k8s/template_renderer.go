package k8s

import (
	"bytes"
	"fmt"
	"text/template"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"
)

// BuildKitJobParams contains all parameters needed to render the BuildKit job template
type BuildKitJobParams struct {
	JobName            string // unitune-build-<timestamp>
	Namespace          string // unitune-build
	BuildID            string // <timestamp>
	ServiceAccountName string // unitune-builder
	S3Bucket           string // unitune-buildctx-{accountId}-{region}
	S3Key              string // contexts/<timestamp>.tar
	ECRRegistry        string // {accountId}.dkr.ecr.{region}.amazonaws.com
	ImageName          string // Inferred from directory name
	ImageTag           string // Always "latest"
	AWSRegion          string // From AWS config
}

// RenderBuildKitJob renders the BuildKit job template with the given parameters
func RenderBuildKitJob(params BuildKitJobParams) (*batchv1.Job, error) {
	templateContent, err := TemplatesFS.ReadFile("templates/buildkit_job.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read buildkit job template: %w", err)
	}

	tmpl, err := template.New("buildkit-job").Parse(string(templateContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse buildkit job template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return nil, fmt.Errorf("failed to render buildkit job template: %w", err)
	}

	var job batchv1.Job
	if err := yaml.Unmarshal(rendered.Bytes(), &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rendered job yaml: %w", err)
	}

	return &job, nil
}

// RenderBuildKitJobYAML renders the BuildKit job template and returns the raw YAML string
func RenderBuildKitJobYAML(params BuildKitJobParams) (string, error) {
	templateContent, err := TemplatesFS.ReadFile("templates/buildkit_job.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to read buildkit job template: %w", err)
	}

	tmpl, err := template.New("buildkit-job").Parse(string(templateContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse buildkit job template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return "", fmt.Errorf("failed to render buildkit job template: %w", err)
	}

	return rendered.String(), nil
}

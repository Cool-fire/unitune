package k8s

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// K8sClient wraps the Kubernetes clientset for generic operations
type K8sClient struct {
	clientset *kubernetes.Clientset
	namespace string
}

// NewK8sClient creates a generic Kubernetes client from a REST config
func NewK8sClient(restConfig *rest.Config, namespace string) (*K8sClient, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &K8sClient{
		clientset: clientset,
		namespace: namespace,
	}, nil
}

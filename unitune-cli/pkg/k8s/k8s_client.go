package k8s

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

// K8sClient wraps the Kubernetes clientset for job operations
type K8sClient struct {
	clientset *kubernetes.Clientset
	namespace string
}

// NewK8sClientForEKS creates a K8s client that connects to an EKS cluster
func NewK8sClientForEKS(cfg aws.Config, clusterName string) (*K8sClient, error) {
	eksClient := eks.NewFromConfig(cfg)

	// Describe the cluster to get endpoint and CA
	describeOutput, err := eksClient.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster %s: %w", clusterName, err)
	}

	cluster := describeOutput.Cluster
	if cluster == nil {
		return nil, fmt.Errorf("cluster %s not found", clusterName)
	}

	// Decode the CA certificate
	caData, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cluster CA: %w", err)
	}

	// Generate a token using aws-iam-authenticator
	gen, err := token.NewGenerator(true, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create token generator: %w", err)
	}

	tok, err := gen.GetWithOptions(&token.GetTokenOptions{
		ClusterID: clusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get EKS token: %w", err)
	}

	// Create the rest config
	restConfig := &rest.Config{
		Host:        *cluster.Endpoint,
		BearerToken: tok.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caData,
		},
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &K8sClient{
		clientset: clientset,
		namespace: "unitune-build",
	}, nil
}

// CreateJob creates a Kubernetes Job
func (k *K8sClient) CreateJob(ctx context.Context, job *batchv1.Job) error {
	_, err := k.clientset.BatchV1().Jobs(k.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create job %s: %w", job.Name, err)
	}
	return nil
}

// WaitForJobCompletion waits for a job to complete or fail
func (k *K8sClient) WaitForJobCompletion(ctx context.Context, jobName string, timeout time.Duration) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return fmt.Errorf("timeout waiting for job %s to complete", jobName)
		case <-ticker.C:
			job, err := k.clientset.BatchV1().Jobs(k.namespace).Get(ctx, jobName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			// Check for completion
			for _, condition := range job.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil
				}
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("job %s failed: %s", jobName, condition.Message)
				}
			}
		}
	}
}

// StreamJobLogs streams the logs from a job's pod to the provided writer
func (k *K8sClient) StreamJobLogs(ctx context.Context, jobName string, out io.Writer) error {
	// Wait for pod to be created
	var podName string
	for i := 0; i < 60; i++ {
		pods, err := k.clientset.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("job-name=%s", jobName),
		})
		if err != nil {
			return fmt.Errorf("failed to list pods for job: %w", err)
		}

		if len(pods.Items) > 0 {
			podName = pods.Items[0].Name
			break
		}

		time.Sleep(2 * time.Second)
	}

	if podName == "" {
		return fmt.Errorf("no pod found for job %s", jobName)
	}

	// Wait for pod to be running or completed
	for i := 0; i < 60; i++ {
		pod, err := k.clientset.CoreV1().Pods(k.namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod: %w", err)
		}

		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			break
		}

		time.Sleep(2 * time.Second)
	}

	// Stream logs
	req := k.clientset.CoreV1().Pods(k.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow:    true,
		Container: "buildkit",
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream logs: %w", err)
	}
	defer stream.Close()

	_, err = io.Copy(out, stream)
	return err
}

// DeleteJob deletes a job and its pods
func (k *K8sClient) DeleteJob(ctx context.Context, jobName string) error {
	propagationPolicy := metav1.DeletePropagationForeground
	err := k.clientset.BatchV1().Jobs(k.namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to delete job %s: %w", jobName, err)
	}
	return nil
}

package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildJobConfig holds configuration for a build job
type BuildJobConfig struct {
	JobName           string
	InitContainerName string
	MainContainerName string
	Timeout           time.Duration
}

// BuildJob represents a Kubernetes build job with its configuration
type BuildJob struct {
	config    BuildJobConfig
	k8sClient *K8sClient
	jobSpec   *batchv1.Job
}

// NewBuildJob creates a new BuildJob with the given configuration and Kubernetes client
func NewBuildJob(config BuildJobConfig, k8sClient *K8sClient, jobSpec *batchv1.Job) *BuildJob {
	return &BuildJob{
		config:    config,
		k8sClient: k8sClient,
		jobSpec:   jobSpec,
	}
}

// Create creates the Kubernetes Job
func (b *BuildJob) Create(ctx context.Context) error {
	_, err := b.k8sClient.clientset.BatchV1().Jobs(b.k8sClient.namespace).Create(ctx, b.jobSpec, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create job %s: %w", b.config.JobName, err)
	}
	return nil
}

// WaitForCompletion waits for the job to complete or fail
func (b *BuildJob) WaitForCompletion(ctx context.Context) error {
	timeout := b.config.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute // default timeout
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return fmt.Errorf("timeout waiting for job %s to complete", b.config.JobName)
		case <-ticker.C:
			job, err := b.k8sClient.clientset.BatchV1().Jobs(b.k8sClient.namespace).Get(ctx, b.config.JobName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			// Check for completion
			for _, condition := range job.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil
				}
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("job %s failed: %s", b.config.JobName, condition.Message)
				}
			}
		}
	}
}

// StreamLogs streams the logs from the job's pod to the provided writer
func (b *BuildJob) StreamLogs(ctx context.Context, out io.Writer) error {
	// Wait for pod to be created (up to 10 minutes)
	var podName string
	fmt.Fprintln(out, "Waiting for pod to be scheduled...")
	for i := 0; i < 300; i++ {
		pods, err := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("job-name=%s", b.config.JobName),
		})
		if err != nil {
			return fmt.Errorf("failed to list pods for job: %w", err)
		}

		if len(pods.Items) > 0 {
			podName = pods.Items[0].Name
			fmt.Fprintf(out, "Pod created: %s\n", podName)
			break
		}

		time.Sleep(2 * time.Second)
	}

	if podName == "" {
		return fmt.Errorf("no pod found for job %s after 10 minutes", b.config.JobName)
	}

	// Wait for init container to start (up to 10 minutes)
	if b.config.InitContainerName != "" {
		fmt.Fprintln(out, "Waiting for init container to start...")
		for i := 0; i < 300; i++ {
			pod, err := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get pod: %w", err)
			}

			// Check if init container is running or completed
			if len(pod.Status.InitContainerStatuses) > 0 {
				initStatus := pod.Status.InitContainerStatuses[0]
				if initStatus.State.Running != nil || initStatus.State.Terminated != nil {
					break
				}
			}

			// Also break if pod is already running (init completed)
			if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				break
			}

			time.Sleep(2 * time.Second)
		}

		// Stream init container logs
		fmt.Fprintf(out, "--- Init Container Logs (%s) ---\n", b.config.InitContainerName)
		initReq := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).GetLogs(podName, &corev1.PodLogOptions{
			Follow:    true,
			Container: b.config.InitContainerName,
		})

		initStream, err := initReq.Stream(ctx)
		if err == nil {
			io.Copy(out, initStream)
			initStream.Close()
		}
	}

	// Wait for main container to start
	fmt.Fprintln(out, "Waiting for main container to start...")
	for i := 0; i < 60; i++ {
		pod, err := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod: %w", err)
		}

		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			break
		}

		time.Sleep(2 * time.Second)
	}

	// Stream main container logs
	if b.config.MainContainerName != "" {
		fmt.Fprintf(out, "--- Main Container Logs (%s) ---\n", b.config.MainContainerName)
		req := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).GetLogs(podName, &corev1.PodLogOptions{
			Follow:    true,
			Container: b.config.MainContainerName,
		})

		stream, err := req.Stream(ctx)
		if err != nil {
			return fmt.Errorf("failed to stream logs: %w", err)
		}
		defer stream.Close()

		_, err = io.Copy(out, stream)
		return err
	}

	return nil
}

// Delete deletes the job and its pods
func (b *BuildJob) Delete(ctx context.Context) error {
	propagationPolicy := metav1.DeletePropagationForeground
	err := b.k8sClient.clientset.BatchV1().Jobs(b.k8sClient.namespace).Delete(ctx, b.config.JobName, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to delete job %s: %w", b.config.JobName, err)
	}
	return nil
}

// Name returns the job name
func (b *BuildJob) Name() string {
	return b.config.JobName
}

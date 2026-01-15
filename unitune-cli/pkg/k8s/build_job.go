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
	JobSpec           *batchv1.Job
}

// BuildJob represents a Kubernetes build job with its configuration
type BuildJob struct {
	config    BuildJobConfig
	k8sClient *K8sClient
}

// NewBuildJob creates a new BuildJob with the given configuration and Kubernetes client
func NewBuildJob(config BuildJobConfig, k8sClient *K8sClient) *BuildJob {
	return &BuildJob{
		config:    config,
		k8sClient: k8sClient,
	}
}

// Create creates the Kubernetes Job
func (b *BuildJob) Create(ctx context.Context) error {
	_, err := b.k8sClient.clientset.BatchV1().Jobs(b.k8sClient.namespace).Create(ctx, b.config.JobSpec, metav1.CreateOptions{})
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
// StreamLogs streams the logs from the job's pod to the provided writer
func (b *BuildJob) StreamLogs(ctx context.Context, out io.Writer) error {
	// 1. Get the pod associated with the job
	fmt.Fprintln(out, "Waiting for pod to be scheduled...")
	pod, err := b.getJobPod(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Pod scheduled: %s\n", pod.Name)

	// 2. Handle Init Container
	if b.config.InitContainerName != "" {
		fmt.Fprintf(out, "Waiting for init container %s to start...\n", b.config.InitContainerName)
		if err := b.waitForContainer(ctx, pod.Name, b.config.InitContainerName, true); err != nil {
			return err
		}
		fmt.Fprintf(out, "--- Init Container Logs (%s) ---\n", b.config.InitContainerName)
		if err := b.streamContainerLogs(ctx, pod.Name, b.config.InitContainerName, out); err != nil {
			fmt.Fprintf(out, "Warning: failed to stream init logs: %v\n", err)
		}
	}

	// 3. Handle Main Container
	fmt.Fprintf(out, "Waiting for main container %s to start...\n", b.config.MainContainerName)
	if err := b.waitForContainer(ctx, pod.Name, b.config.MainContainerName, false); err != nil {
		return err
	}
	fmt.Fprintf(out, "--- Main Container Logs (%s) ---\n", b.config.MainContainerName)
	return b.streamContainerLogs(ctx, pod.Name, b.config.MainContainerName, out)
}

// getJobPod retrieves the pod associated with the build job, polling until it exists
func (b *BuildJob) getJobPod(ctx context.Context) (*corev1.Pod, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			pods, err := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job-name=%s", b.config.JobName),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}

			if len(pods.Items) > 0 {
				return &pods.Items[0], nil
			}
		}
	}
}

// waitForContainer waits until the specified container is in a state where logs can be streamed
func (b *BuildJob) waitForContainer(ctx context.Context, podName, containerName string, isInit bool) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pod, err := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get pod: %w", err)
			}

			var containerStatus *corev1.ContainerStatus
			statuses := pod.Status.ContainerStatuses
			if isInit {
				statuses = pod.Status.InitContainerStatuses
			}

			for _, s := range statuses {
				if s.Name == containerName {
					containerStatus = &s
					break
				}
			}

			if containerStatus != nil {
				// Container is running or finished, we can stream logs
				if containerStatus.State.Running != nil || containerStatus.State.Terminated != nil {
					return nil
				}
			}

			// Fail if pod itself failed
			if pod.Status.Phase == corev1.PodFailed {
				return fmt.Errorf("pod %s failed while waiting for container %s", podName, containerName)
			}
		}
	}
}

// streamContainerLogs streams the logs of a specific container to the writer
func (b *BuildJob) streamContainerLogs(ctx context.Context, podName, containerName string, out io.Writer) error {
	req := b.k8sClient.clientset.CoreV1().Pods(b.k8sClient.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow:    true,
		Container: containerName,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to open log stream for %s: %w", containerName, err)
	}
	defer stream.Close()

	if _, err := io.Copy(out, stream); err != nil && err != io.EOF {
		return fmt.Errorf("error copying logs for %s: %w", containerName, err)
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

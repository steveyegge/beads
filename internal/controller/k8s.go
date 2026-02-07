package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient wraps the Kubernetes client for pod operations.
type K8sClient struct {
	clientset kubernetes.Interface
	namespace string
}

// NewK8sClient creates a K8s client. It tries in-cluster config first,
// then falls back to kubeconfig file.
func NewK8sClient(namespace, kubeconfig string) (*K8sClient, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig location
			config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build K8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s clientset: %w", err)
	}

	return &K8sClient{
		clientset: clientset,
		namespace: namespace,
	}, nil
}

// NewK8sClientFromClientset creates a K8sClient from an existing clientset (for testing).
func NewK8sClientFromClientset(clientset kubernetes.Interface, namespace string) *K8sClient {
	return &K8sClient{
		clientset: clientset,
		namespace: namespace,
	}
}

// ListAgentPods lists all pods with the gastown-agent app label.
func (k *K8sClient) ListAgentPods(ctx context.Context) ([]corev1.Pod, error) {
	podList, err := k.clientset.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelApp + "=" + LabelAppValue,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list agent pods: %w", err)
	}
	return podList.Items, nil
}

// GetPod gets a specific pod by name.
func (k *K8sClient) GetPod(ctx context.Context, name string) (*corev1.Pod, error) {
	pod, err := k.clientset.CoreV1().Pods(k.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", name, err)
	}
	return pod, nil
}

// CreatePod creates a new pod.
func (k *K8sClient) CreatePod(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	created, err := k.clientset.CoreV1().Pods(k.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod %s: %w", pod.Name, err)
	}
	return created, nil
}

// DeletePod deletes a pod by name with graceful termination.
func (k *K8sClient) DeletePod(ctx context.Context, name string) error {
	gracePeriod := int64(30)
	err := k.clientset.CoreV1().Pods(k.namespace).Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil {
		return fmt.Errorf("failed to delete pod %s: %w", name, err)
	}
	return nil
}

// PodPhase returns the phase of a pod (Pending, Running, Succeeded, Failed, Unknown).
func PodPhase(pod *corev1.Pod) corev1.PodPhase {
	return pod.Status.Phase
}

// IsPodCrashLooping checks if any container in the pod is in CrashLoopBackOff.
func IsPodCrashLooping(pod *corev1.Pod) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			return true
		}
	}
	return false
}

// PodIP returns the pod's IP address.
func PodIP(pod *corev1.Pod) string {
	return pod.Status.PodIP
}

// PodNode returns the node the pod is scheduled on.
func PodNode(pod *corev1.Pod) string {
	return pod.Spec.NodeName
}

// AgentIDFromPod extracts the agent ID from pod labels.
func AgentIDFromPod(pod *corev1.Pod) string {
	if pod.Labels == nil {
		return ""
	}
	return pod.Labels[LabelAgent]
}

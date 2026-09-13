package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

const (
	restartAnnotation = "kubectl.kubernetes.io/restartedAt"
	fieldManager      = "homelab-k3s-mcp"
)

// KubeService is the live kubernetes-backed implementation of Service.
type KubeService struct {
	clientset kubernetes.Interface
	config    *rest.Config
	mappers   mapperCache
}

// New builds a KubeService from the in-cluster config, falling back to the
// local kubeconfig when not running inside a cluster.
func New() (*KubeService, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, &clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, unavailableErr(fmt.Sprintf("init kube client: %v", err))
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, unavailableErr(fmt.Sprintf("init kube client: %v", err))
	}
	return &KubeService{clientset: clientset, config: config}, nil
}

func (s *KubeService) RolloutRestart(ctx context.Context, kind WorkloadKind, namespace, name string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`,
		restartAnnotation, now,
	)
	opts := metav1.PatchOptions{FieldManager: fieldManager}
	var err error
	switch kind {
	case Deployment:
		_, err = s.clientset.AppsV1().Deployments(namespace).
			Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), opts)
	case StatefulSet:
		_, err = s.clientset.AppsV1().StatefulSets(namespace).
			Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), opts)
	case DaemonSet:
		_, err = s.clientset.AppsV1().DaemonSets(namespace).
			Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), opts)
	}
	if err != nil {
		return "", APIError(err.Error())
	}
	return now, nil
}

func (s *KubeService) ScaleWorkload(ctx context.Context, kind WorkloadKind, namespace, name string, replicas int32) (int32, error) {
	if kind == DaemonSet {
		return 0, APIError("DaemonSet does not have replicas; cannot scale")
	}
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	opts := metav1.PatchOptions{FieldManager: fieldManager}
	var err error
	switch kind {
	case Deployment:
		_, err = s.clientset.AppsV1().Deployments(namespace).
			Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), opts)
	case StatefulSet:
		_, err = s.clientset.AppsV1().StatefulSets(namespace).
			Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), opts)
	}
	if err != nil {
		return 0, APIError(err.Error())
	}
	return replicas, nil
}

func (s *KubeService) ExecInPod(ctx context.Context, namespace, labelSelector string, container *string, command []string) (*ExecOutcome, error) {
	list, err := s.clientset.CoreV1().Pods(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, APIError(err.Error())
	}

	var pod *corev1.Pod
	for i := range list.Items {
		if list.Items[i].Status.Phase == corev1.PodRunning {
			pod = &list.Items[i]
			break
		}
	}
	if pod == nil {
		return nil, apiErrorf("no Running pod matched selector %q in namespace %q", labelSelector, namespace)
	}

	execOpts := &corev1.PodExecOptions{
		Command: command,
		Stdout:  true,
		Stderr:  true,
	}
	if container != nil {
		execOpts.Container = *container
	}

	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(execOpts, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.config, "POST", req.URL())
	if err != nil {
		return nil, APIError(err.Error())
	}

	var stdout, stderr bytes.Buffer
	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	var exitCode int32
	if streamErr != nil {
		var codeErr utilexec.CodeExitError
		if errors.As(streamErr, &codeErr) {
			exitCode = int32(codeErr.Code)
		} else {
			return nil, APIError(streamErr.Error())
		}
	}

	code := exitCode
	return &ExecOutcome{
		Pod:      pod.Name,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: &code,
		Success:  exitCode == 0,
	}, nil
}

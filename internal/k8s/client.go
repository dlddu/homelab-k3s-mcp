package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
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

func formatTime(t metav1.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// labelSelectorToString renders a LabelSelector's matchLabels as a
// comma-joined "k=v" selector, sorted for determinism. ok is false when there
// are no usable matchLabels.
func labelSelectorToString(sel *metav1.LabelSelector) (string, bool) {
	if sel == nil || len(sel.MatchLabels) == 0 {
		return "", false
	}
	keys := make([]string, 0, len(sel.MatchLabels))
	for k := range sel.MatchLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+sel.MatchLabels[k])
	}
	return joinComma(parts), true
}

func joinComma(parts []string) string {
	var b bytes.Buffer
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(p)
	}
	return b.String()
}

func (s *KubeService) ListNamespaces(ctx context.Context) ([]any, error) {
	list, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, APIError(err.Error())
	}
	items := make([]any, 0, len(list.Items))
	for i := range list.Items {
		n := &list.Items[i]
		items = append(items, namespaceSummary{
			Name:              n.Name,
			Phase:             strPtr(string(n.Status.Phase)),
			CreationTimestamp: formatTime(n.CreationTimestamp),
		})
	}
	return items, nil
}

func (s *KubeService) ListWorkloads(ctx context.Context, kind WorkloadKind, namespace *string) ([]any, error) {
	ns := metav1.NamespaceAll
	if namespace != nil {
		ns = *namespace
	}

	switch kind {
	case Deployment:
		list, err := s.clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, APIError(err.Error())
		}
		items := make([]any, 0, len(list.Items))
		for i := range list.Items {
			d := &list.Items[i]
			items = append(items, deploymentSummary{
				Name:              d.Name,
				Namespace:         d.Namespace,
				Replicas:          derefInt32(d.Spec.Replicas),
				ReadyReplicas:     d.Status.ReadyReplicas,
				UpdatedReplicas:   d.Status.UpdatedReplicas,
				AvailableReplicas: d.Status.AvailableReplicas,
				CreationTimestamp: formatTime(d.CreationTimestamp),
			})
		}
		return items, nil
	case StatefulSet:
		list, err := s.clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, APIError(err.Error())
		}
		items := make([]any, 0, len(list.Items))
		for i := range list.Items {
			ss := &list.Items[i]
			items = append(items, statefulSetSummary{
				Name:              ss.Name,
				Namespace:         ss.Namespace,
				Replicas:          derefInt32(ss.Spec.Replicas),
				ReadyReplicas:     ss.Status.ReadyReplicas,
				UpdatedReplicas:   ss.Status.UpdatedReplicas,
				CurrentReplicas:   ss.Status.CurrentReplicas,
				CreationTimestamp: formatTime(ss.CreationTimestamp),
			})
		}
		return items, nil
	case DaemonSet:
		list, err := s.clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, APIError(err.Error())
		}
		items := make([]any, 0, len(list.Items))
		for i := range list.Items {
			d := &list.Items[i]
			items = append(items, daemonSetSummary{
				Name:                   d.Name,
				Namespace:              d.Namespace,
				DesiredNumberScheduled: d.Status.DesiredNumberScheduled,
				CurrentNumberScheduled: d.Status.CurrentNumberScheduled,
				NumberReady:            d.Status.NumberReady,
				NumberAvailable:        d.Status.NumberAvailable,
				UpdatedNumberScheduled: d.Status.UpdatedNumberScheduled,
				CreationTimestamp:      formatTime(d.CreationTimestamp),
			})
		}
		return items, nil
	default:
		return nil, apiErrorf("unknown workload kind")
	}
}

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
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

func (s *KubeService) workloadPodSelector(ctx context.Context, kind WorkloadKind, namespace, name string) (string, error) {
	var selector *metav1.LabelSelector
	switch kind {
	case Deployment:
		d, err := s.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", APIError(err.Error())
		}
		selector = d.Spec.Selector
	case StatefulSet:
		ss, err := s.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", APIError(err.Error())
		}
		selector = ss.Spec.Selector
	case DaemonSet:
		d, err := s.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", APIError(err.Error())
		}
		selector = d.Spec.Selector
	}
	s2, ok := labelSelectorToString(selector)
	if !ok {
		return "", apiErrorf("%s %s/%s has no usable spec.selector.matchLabels", kind, namespace, name)
	}
	return s2, nil
}

// firstPodMatching returns the first Running pod matching selector, falling
// back to any matching pod when none is Running.
func (s *KubeService) firstPodMatching(ctx context.Context, namespace, selector string) (*corev1.Pod, error) {
	list, err := s.clientset.CoreV1().Pods(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, APIError(err.Error())
	}
	pod := pickPod(list.Items)
	if pod == nil {
		return nil, apiErrorf("no pod matched selector %q in namespace %q", selector, namespace)
	}
	return pod, nil
}

// pickPod prefers the first Running pod, else the first pod, else nil.
func pickPod(pods []corev1.Pod) *corev1.Pod {
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			return &pods[i]
		}
	}
	if len(pods) > 0 {
		return &pods[0]
	}
	return nil
}

func (s *KubeService) WorkloadLogs(ctx context.Context, kind WorkloadKind, namespace, name string, opts LogOptions) (*LogResult, error) {
	selector, err := s.workloadPodSelector(ctx, kind, namespace, name)
	if err != nil {
		return nil, err
	}
	list, err := s.clientset.CoreV1().Pods(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, APIError(err.Error())
	}
	pod := pickPod(list.Items)
	if pod == nil {
		return nil, apiErrorf("no pod matched selector %q for %s %s/%s", selector, kind, namespace, name)
	}

	logOpts := &corev1.PodLogOptions{
		Previous:   opts.Previous,
		Timestamps: opts.Timestamps,
	}
	if opts.Container != nil {
		logOpts.Container = *opts.Container
	}
	if opts.TailLines != nil {
		logOpts.TailLines = opts.TailLines
	}
	if opts.SinceSeconds != nil {
		logOpts.SinceSeconds = opts.SinceSeconds
	}

	raw, err := s.clientset.CoreV1().Pods(namespace).
		GetLogs(pod.Name, logOpts).DoRaw(ctx)
	if err != nil {
		return nil, APIError(err.Error())
	}

	return &LogResult{
		Pod:       pod.Name,
		Container: opts.Container,
		Logs:      string(raw),
	}, nil
}

func (s *KubeService) DescribePod(ctx context.Context, namespace string, target PodTarget) (*PodDescription, error) {
	var pod *corev1.Pod
	switch target.Mode {
	case TargetName:
		got, err := s.clientset.CoreV1().Pods(namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, APIError(err.Error())
		}
		pod = got
	case TargetSelector:
		got, err := s.firstPodMatching(ctx, namespace, target.Selector)
		if err != nil {
			return nil, err
		}
		pod = got
	case TargetWorkload:
		selector, err := s.workloadPodSelector(ctx, target.Kind, namespace, target.WorkloadName)
		if err != nil {
			return nil, err
		}
		got, err := s.firstPodMatching(ctx, namespace, selector)
		if err != nil {
			return nil, err
		}
		pod = got
	}

	// Events are best-effort: if listing fails (e.g. RBAC), the rest of the
	// description still comes through.
	var events []corev1.Event
	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s", pod.Name, namespace)
	if list, err := s.clientset.CoreV1().Events(namespace).
		List(ctx, metav1.ListOptions{FieldSelector: fieldSelector}); err == nil {
		events = list.Items
	}

	return buildPodDescription(pod, events), nil
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

func buildPodDescription(pod *corev1.Pod, events []corev1.Event) *PodDescription {
	specImages := map[string]string{}
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		specImages[c.Name] = c.Image
	}
	initSpecImages := map[string]string{}
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		initSpecImages[c.Name] = c.Image
	}

	containers := buildContainerInfos(pod.Status.ContainerStatuses, specImages)
	initContainers := buildContainerInfos(pod.Status.InitContainerStatuses, initSpecImages)

	conditions := make([]PodConditionInfo, 0, len(pod.Status.Conditions))
	for _, c := range pod.Status.Conditions {
		conditions = append(conditions, PodConditionInfo{
			Type:               string(c.Type),
			Status:             string(c.Status),
			Reason:             strPtr(c.Reason),
			Message:            strPtr(c.Message),
			LastTransitionTime: formatTime(c.LastTransitionTime),
		})
	}

	eventInfos := make([]PodEventInfo, 0, len(events))
	for i := range events {
		e := &events[i]
		count := e.Count
		if count == 0 {
			count = 1
		}
		eventInfos = append(eventInfos, PodEventInfo{
			Type:           e.Type,
			Reason:         e.Reason,
			Message:        e.Message,
			Count:          count,
			FirstTimestamp: formatTime(e.FirstTimestamp),
			LastTimestamp:  formatTime(e.LastTimestamp),
			Source:         strPtr(e.Source.Component),
		})
	}
	sort.SliceStable(eventInfos, func(i, j int) bool {
		return lessOptString(eventInfos[i].LastTimestamp, eventInfos[j].LastTimestamp)
	})

	owners := make([]any, 0, len(pod.OwnerReferences))
	for _, r := range pod.OwnerReferences {
		owners = append(owners, map[string]any{
			"apiVersion": r.APIVersion,
			"kind":       r.Kind,
			"name":       r.Name,
			"uid":        string(r.UID),
			"controller": r.Controller,
		})
	}

	desc := &PodDescription{
		Name:              pod.Name,
		Namespace:         pod.Namespace,
		Node:              strPtr(pod.Spec.NodeName),
		Phase:             strPtr(string(pod.Status.Phase)),
		PodIP:             strPtr(pod.Status.PodIP),
		HostIP:            strPtr(pod.Status.HostIP),
		ServiceAccount:    strPtr(pod.Spec.ServiceAccountName),
		Priority:          pod.Spec.Priority,
		PriorityClassName: strPtr(pod.Spec.PriorityClassName),
		QOSClass:          strPtr(string(pod.Status.QOSClass)),
		StartTime:         startTimePtr(pod.Status.StartTime),
		CreationTimestamp: formatTime(pod.CreationTimestamp),
		Labels:            nonNilMap(pod.Labels),
		Annotations:       nonNilMap(pod.Annotations),
		NodeSelector:      nonNilMap(pod.Spec.NodeSelector),
		OwnerReferences:   owners,
		Conditions:        conditions,
		InitContainers:    initContainers,
		Containers:        containers,
		Events:            eventInfos,
	}
	return desc
}

func startTimePtr(t *metav1.Time) *string {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func lessOptString(a, b *string) bool {
	if a == nil {
		return b != nil
	}
	if b == nil {
		return false
	}
	return *a < *b
}

func buildContainerInfos(statuses []corev1.ContainerStatus, specImages map[string]string) []ContainerInfo {
	infos := make([]ContainerInfo, 0, len(statuses))
	for i := range statuses {
		cs := &statuses[i]
		image := cs.Image
		if image == "" {
			image = specImages[cs.Name]
		}
		started := cs.Started
		info := ContainerInfo{
			Name:         cs.Name,
			Image:        image,
			Ready:        cs.Ready,
			Started:      started,
			RestartCount: cs.RestartCount,
		}
		switch {
		case cs.State.Running != nil:
			info.State = ptrStr("running")
			info.StartedAt = formatTime(cs.State.Running.StartedAt)
		case cs.State.Waiting != nil:
			info.State = ptrStr("waiting")
			info.Reason = strPtr(cs.State.Waiting.Reason)
			info.Message = strPtr(cs.State.Waiting.Message)
		case cs.State.Terminated != nil:
			info.State = ptrStr("terminated")
			info.Reason = strPtr(cs.State.Terminated.Reason)
			info.Message = strPtr(cs.State.Terminated.Message)
			ec := cs.State.Terminated.ExitCode
			info.ExitCode = &ec
			info.StartedAt = formatTime(cs.State.Terminated.StartedAt)
			info.FinishedAt = formatTime(cs.State.Terminated.FinishedAt)
		}
		switch {
		case cs.LastTerminationState.Running != nil:
			info.LastState = ptrStr("running")
		case cs.LastTerminationState.Waiting != nil:
			info.LastState = ptrStr("waiting")
			info.LastReason = strPtr(cs.LastTerminationState.Waiting.Reason)
		case cs.LastTerminationState.Terminated != nil:
			info.LastState = ptrStr("terminated")
			info.LastReason = strPtr(cs.LastTerminationState.Terminated.Reason)
			lec := cs.LastTerminationState.Terminated.ExitCode
			info.LastExitCode = &lec
		}
		infos = append(infos, info)
	}
	return infos
}

func ptrStr(s string) *string { return &s }

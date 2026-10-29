package cluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodProblem describes an unhealthy pod.
type PodProblem struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Node      string    `json:"node"`
	Phase     string    `json:"phase"`
	Reason    string    `json:"reason"`
	Restarts  int32     `json:"restarts"`
	Since     time.Time `json:"since"`
}

// WorkloadStatus summarises a controller (deployment/statefulset/daemonset).
type WorkloadStatus struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Desired   int32  `json:"desired"`
	Ready     int32  `json:"ready"`
	Healthy   bool   `json:"healthy"`
}

// Event is a Kubernetes warning event.
type Event struct {
	Namespace string    `json:"namespace"`
	Object    string    `json:"object"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Count     int32     `json:"count"`
	LastSeen  time.Time `json:"lastSeen"`
}

// Workloads is the workloads overview.
type Workloads struct {
	PodsTotal   int              `json:"podsTotal"`
	PodsRunning int              `json:"podsRunning"`
	Problems    []PodProblem     `json:"problems"`
	Controllers []WorkloadStatus `json:"controllers"`
	Events      []Event          `json:"events"`
	Namespaces  int              `json:"namespaces"`
}

// Workloads gathers pod/controller health and recent warning events.
func (c *Cluster) Workloads(ctx context.Context) (*Workloads, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	out := &Workloads{Problems: []PodProblem{}, Controllers: []WorkloadStatus{}, Events: []Event{}}

	pods, err := kc.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	nsSeen := map[string]struct{}{}

	for _, p := range pods.Items {
		nsSeen[p.Namespace] = struct{}{}
		out.PodsTotal++

		if prob, ok := podProblem(&p); ok {
			out.Problems = append(out.Problems, prob)
		} else if p.Status.Phase == corev1.PodRunning || p.Status.Phase == corev1.PodSucceeded {
			out.PodsRunning++
		}
	}

	out.Namespaces = len(nsSeen)

	if deps, err := kc.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range deps.Items {
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}

			out.Controllers = append(out.Controllers, WorkloadStatus{"Deployment", d.Namespace, d.Name, desired, d.Status.ReadyReplicas, d.Status.ReadyReplicas >= desired})
		}
	}

	if sts, err := kc.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{}); err == nil {
		for _, s := range sts.Items {
			desired := int32(1)
			if s.Spec.Replicas != nil {
				desired = *s.Spec.Replicas
			}

			out.Controllers = append(out.Controllers, WorkloadStatus{"StatefulSet", s.Namespace, s.Name, desired, s.Status.ReadyReplicas, s.Status.ReadyReplicas >= desired})
		}
	}

	if dss, err := kc.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range dss.Items {
			out.Controllers = append(out.Controllers, WorkloadStatus{"DaemonSet", d.Namespace, d.Name, d.Status.DesiredNumberScheduled, d.Status.NumberReady, d.Status.NumberReady >= d.Status.DesiredNumberScheduled})
		}
	}

	sort.Slice(out.Controllers, func(i, j int) bool {
		a, b := out.Controllers[i], out.Controllers[j]
		if a.Healthy != b.Healthy {
			return !a.Healthy
		}

		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}

		return a.Name < b.Name
	})

	if evs, err := kc.CoreV1().Events("").List(ctx, metav1.ListOptions{FieldSelector: "type=Warning"}); err == nil {
		cutoff := time.Now().Add(-2 * time.Hour)

		for _, e := range evs.Items {
			last := e.LastTimestamp.Time
			if last.IsZero() {
				last = e.EventTime.Time
			}

			if last.IsZero() {
				last = e.CreationTimestamp.Time
			}

			if last.Before(cutoff) {
				continue
			}

			out.Events = append(out.Events, Event{
				Namespace: e.Namespace,
				Object:    e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
				Reason:    e.Reason,
				Message:   e.Message,
				Count:     e.Count,
				LastSeen:  last,
			})
		}

		sort.Slice(out.Events, func(i, j int) bool { return out.Events[i].LastSeen.After(out.Events[j].LastSeen) })

		if len(out.Events) > 100 {
			out.Events = out.Events[:100]
		}
	}

	return out, nil
}

func podProblem(p *corev1.Pod) (PodProblem, bool) {
	prob := PodProblem{
		Namespace: p.Namespace,
		Name:      p.Name,
		Node:      p.Spec.NodeName,
		Phase:     string(p.Status.Phase),
		Since:     p.CreationTimestamp.Time,
	}

	for _, cs := range p.Status.ContainerStatuses {
		prob.Restarts += cs.RestartCount
	}

	switch p.Status.Phase {
	case corev1.PodSucceeded:
		return prob, false
	case corev1.PodFailed:
		prob.Reason = p.Status.Reason
		if prob.Reason == "" {
			prob.Reason = "Failed"
		}

		return prob, true
	case corev1.PodPending:
		if time.Since(p.CreationTimestamp.Time) > 5*time.Minute {
			prob.Reason = "Pending"

			for _, cond := range p.Status.Conditions {
				if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
					prob.Reason = "Unschedulable: " + cond.Message
				}
			}

			for _, cs := range p.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
					prob.Reason = cs.State.Waiting.Reason
				}
			}

			return prob, true
		}

		return prob, false
	}

	for _, cs := range append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CreateContainerError", "InvalidImageName":
				prob.Reason = w.Reason

				return prob, true
			}
		}
	}

	if p.Status.Phase == corev1.PodRunning {
		for _, cond := range p.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionFalse &&
				time.Since(cond.LastTransitionTime.Time) > 5*time.Minute {
				prob.Reason = "NotReady"
				prob.Since = cond.LastTransitionTime.Time

				return prob, true
			}
		}
	}

	return prob, false
}

// KubernetesVersion returns the API server version.
func (c *Cluster) KubernetesVersion(ctx context.Context) (string, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return "", err
	}

	v, err := kc.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}

	return v.GitVersion, nil
}

package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Read-only browsing of Kubernetes resources. An empty namespace means all.

// KubeDeployment is a deployment row.
type KubeDeployment struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Desired   int32     `json:"desired"`
	Ready     int32     `json:"ready"`
	Updated   int32     `json:"updated"`
	Available int32     `json:"available"`
	Images    []string  `json:"images"`
	Created   time.Time `json:"created"`
}

// KubePod is a pod row with optional live usage.
type KubePod struct {
	Namespace  string    `json:"namespace"`
	Name       string    `json:"name"`
	Node       string    `json:"node"`
	Phase      string    `json:"phase"`
	Status     string    `json:"status"`
	Ready      string    `json:"ready"`
	Restarts   int32     `json:"restarts"`
	Containers []string  `json:"containers"`
	Created    time.Time `json:"created"`
	// Usage from metrics-server; nil if unavailable for this pod.
	CPUMilli *int64 `json:"cpuMilli"`
	MemBytes *int64 `json:"memBytes"`
	// Requests / limits summed over containers (0 = unset).
	CPURequestMilli int64 `json:"cpuRequestMilli"`
	CPULimitMilli   int64 `json:"cpuLimitMilli"`
	MemRequestBytes int64 `json:"memRequestBytes"`
	MemLimitBytes   int64 `json:"memLimitBytes"`
}

// KubePods is the pod list plus whether metrics-server answered.
type KubePods struct {
	Pods             []KubePod `json:"pods"`
	MetricsAvailable bool      `json:"metricsAvailable"`
	MetricsError     string    `json:"metricsError,omitempty"`
}

// KubeService is a service row.
type KubeService struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	ClusterIP   string            `json:"clusterIP"`
	ExternalIPs []string          `json:"externalIPs"`
	Ports       []string          `json:"ports"`
	Selector    map[string]string `json:"selector"`
	Created     time.Time         `json:"created"`
}

// KubeHTTPRoute is a Gateway API HTTPRoute row.
type KubeHTTPRoute struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Hostnames []string  `json:"hostnames"`
	Gateways  []string  `json:"gateways"`
	Rules     []string  `json:"rules"`
	Accepted  string    `json:"accepted"`
	Created   time.Time `json:"created"`
}

// KubeKeyed is a configmap or secret listing entry (keys and sizes only).
type KubeKeyed struct {
	Namespace string         `json:"namespace"`
	Name      string         `json:"name"`
	Type      string         `json:"type,omitempty"`
	Keys      map[string]int `json:"keys"` // key -> size in bytes
	Created   time.Time      `json:"created"`
}

// KubeConfigMap is a single configmap with its values.
type KubeConfigMap struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Data      map[string]string `json:"data"`
	Binary    map[string]int    `json:"binary"` // key -> size
}

// KubeNamespaces lists namespace names.
func (c *Cluster) KubeNamespaces(ctx context.Context) ([]string, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	l, err := kc.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(l.Items))
	for _, n := range l.Items {
		out = append(out, n.Name)
	}

	sort.Strings(out)

	return out, nil
}

// KubeDeployments lists deployments.
func (c *Cluster) KubeDeployments(ctx context.Context, ns string) ([]KubeDeployment, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	l, err := kc.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make([]KubeDeployment, 0, len(l.Items))

	for _, d := range l.Items {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}

		var images []string
		for _, ct := range d.Spec.Template.Spec.Containers {
			images = append(images, ct.Image)
		}

		out = append(out, KubeDeployment{
			Namespace: d.Namespace, Name: d.Name, Desired: desired, Ready: d.Status.ReadyReplicas,
			Updated: d.Status.UpdatedReplicas, Available: d.Status.AvailableReplicas,
			Images: images, Created: d.CreationTimestamp.Time,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})

	return out, nil
}

// KubePods lists pods with usage from metrics-server when available.
func (c *Cluster) KubePods(ctx context.Context, ns string) (*KubePods, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	l, err := kc.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	usage, mErr := podMetrics(ctx, kc, ns)
	out := &KubePods{Pods: make([]KubePod, 0, len(l.Items)), MetricsAvailable: mErr == nil}

	if mErr != nil {
		out.MetricsError = mErr.Error()
	}

	for i := range l.Items {
		p := &l.Items[i]
		kp := KubePod{
			Namespace: p.Namespace, Name: p.Name, Node: p.Spec.NodeName,
			Phase: string(p.Status.Phase), Status: podStatus(p), Created: p.CreationTimestamp.Time,
		}

		ready := 0

		for _, cs := range p.Status.ContainerStatuses {
			kp.Restarts += cs.RestartCount

			if cs.Ready {
				ready++
			}
		}

		kp.Ready = fmt.Sprintf("%d/%d", ready, len(p.Spec.Containers))

		for _, ct := range p.Spec.Containers {
			kp.Containers = append(kp.Containers, ct.Name)
			kp.CPURequestMilli += ct.Resources.Requests.Cpu().MilliValue()
			kp.CPULimitMilli += ct.Resources.Limits.Cpu().MilliValue()
			kp.MemRequestBytes += ct.Resources.Requests.Memory().Value()
			kp.MemLimitBytes += ct.Resources.Limits.Memory().Value()
		}

		if u, ok := usage[p.Namespace+"/"+p.Name]; ok {
			cpu, mem := u[0], u[1]
			kp.CPUMilli, kp.MemBytes = &cpu, &mem
		}

		out.Pods = append(out.Pods, kp)
	}

	sort.Slice(out.Pods, func(i, j int) bool {
		return out.Pods[i].Namespace+"/"+out.Pods[i].Name < out.Pods[j].Namespace+"/"+out.Pods[j].Name
	})

	return out, nil
}

// podStatus mimics the STATUS column of kubectl get pods.
func podStatus(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}

	for _, cs := range append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
		if w := cs.State.Waiting; w != nil && w.Reason != "" && w.Reason != "PodInitializing" {
			return w.Reason
		}

		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 && t.Reason != "" && p.Status.Phase != corev1.PodSucceeded {
			return t.Reason
		}
	}

	if p.Status.Reason != "" {
		return p.Status.Reason
	}

	return string(p.Status.Phase)
}

type podMetricsList struct {
	Items []struct {
		Metadata   metav1.ObjectMeta `json:"metadata"`
		Containers []struct {
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// podMetrics returns "ns/name" -> [cpu millicores, memory bytes].
func podMetrics(ctx context.Context, kc kubernetes.Interface, ns string) (map[string][2]int64, error) {
	path := "/apis/metrics.k8s.io/v1beta1/pods"
	if ns != "" {
		path = "/apis/metrics.k8s.io/v1beta1/namespaces/" + ns + "/pods"
	}

	raw, err := kc.CoreV1().RESTClient().Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
			return nil, errors.New("metrics-server is not installed or not ready")
		}

		return nil, fmt.Errorf("metrics: %w", err)
	}

	var l podMetricsList
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}

	out := make(map[string][2]int64, len(l.Items))

	for _, it := range l.Items {
		var cpu, mem int64

		for _, ct := range it.Containers {
			if q, err := resource.ParseQuantity(ct.Usage["cpu"]); err == nil {
				cpu += q.MilliValue()
			}

			if q, err := resource.ParseQuantity(ct.Usage["memory"]); err == nil {
				mem += q.Value()
			}
		}

		out[it.Metadata.Namespace+"/"+it.Metadata.Name] = [2]int64{cpu, mem}
	}

	return out, nil
}

// KubePodLogs returns the last `tail` lines of a container's log.
func (c *Cluster) KubePodLogs(ctx context.Context, ns, pod, container string, tail int64, previous bool) (string, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return "", err
	}

	tail = max(1, min(tail, 5000))

	rc, err := kc.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Container: container, TailLines: &tail, Previous: previous, Timestamps: true,
	}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	b, err := io.ReadAll(io.LimitReader(rc, 4<<20))

	return string(b), err
}

// KubeServices lists services.
func (c *Cluster) KubeServices(ctx context.Context, ns string) ([]KubeService, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	l, err := kc.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make([]KubeService, 0, len(l.Items))

	for _, s := range l.Items {
		ks := KubeService{
			Namespace: s.Namespace, Name: s.Name, Type: string(s.Spec.Type), ClusterIP: s.Spec.ClusterIP,
			Selector: s.Spec.Selector, Created: s.CreationTimestamp.Time, ExternalIPs: append([]string{}, s.Spec.ExternalIPs...),
		}

		for _, ing := range s.Status.LoadBalancer.Ingress {
			ks.ExternalIPs = append(ks.ExternalIPs, ing.IP+ing.Hostname)
		}

		for _, p := range s.Spec.Ports {
			str := fmt.Sprintf("%d/%s", p.Port, p.Protocol)
			if p.NodePort != 0 {
				str = fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol)
			}

			ks.Ports = append(ks.Ports, str)
		}

		out = append(out, ks)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})

	return out, nil
}

type httpRouteList struct {
	Items []struct {
		Metadata metav1.ObjectMeta `json:"metadata"`
		Spec     struct {
			Hostnames  []string `json:"hostnames"`
			ParentRefs []struct {
				Name        string `json:"name"`
				Namespace   string `json:"namespace"`
				SectionName string `json:"sectionName"`
			} `json:"parentRefs"`
			Rules []struct {
				Matches []struct {
					Path *struct {
						Type  string `json:"type"`
						Value string `json:"value"`
					} `json:"path"`
				} `json:"matches"`
				BackendRefs []struct {
					Name string `json:"name"`
					Port int    `json:"port"`
				} `json:"backendRefs"`
			} `json:"rules"`
		} `json:"spec"`
		Status struct {
			Parents []struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"parents"`
		} `json:"status"`
	} `json:"items"`
}

// KubeHTTPRoutes lists Gateway API HTTPRoutes. A cluster without the CRD
// yields an empty list and no error.
func (c *Cluster) KubeHTTPRoutes(ctx context.Context, ns string) ([]KubeHTTPRoute, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	var raw []byte

	for _, ver := range []string{"v1", "v1beta1"} {
		path := "/apis/gateway.networking.k8s.io/" + ver + "/httproutes"
		if ns != "" {
			path = "/apis/gateway.networking.k8s.io/" + ver + "/namespaces/" + ns + "/httproutes"
		}

		raw, err = kc.CoreV1().RESTClient().Get().AbsPath(path).DoRaw(ctx)
		if err == nil {
			break
		}

		if !apierrors.IsNotFound(err) {
			return nil, err
		}
	}

	if err != nil {
		return []KubeHTTPRoute{}, nil //nolint:nilerr // Gateway API not installed
	}

	var l httpRouteList
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}

	out := make([]KubeHTTPRoute, 0, len(l.Items))

	for _, it := range l.Items {
		r := KubeHTTPRoute{
			Namespace: it.Metadata.Namespace, Name: it.Metadata.Name, Created: it.Metadata.CreationTimestamp.Time,
			Hostnames: it.Spec.Hostnames, Gateways: []string{}, Rules: []string{},
		}

		for _, p := range it.Spec.ParentRefs {
			g := p.Name
			if p.Namespace != "" {
				g = p.Namespace + "/" + g
			}

			if p.SectionName != "" {
				g += ":" + p.SectionName
			}

			r.Gateways = append(r.Gateways, g)
		}

		for _, rule := range it.Spec.Rules {
			paths := []string{}

			for _, m := range rule.Matches {
				if m.Path != nil {
					paths = append(paths, m.Path.Value)
				}
			}

			if len(paths) == 0 {
				paths = append(paths, "/")
			}

			var backends []string
			for _, b := range rule.BackendRefs {
				backends = append(backends, fmt.Sprintf("%s:%d", b.Name, b.Port))
			}

			r.Rules = append(r.Rules, strings.Join(paths, ", ")+" → "+strings.Join(backends, ", "))
		}

		r.Accepted = "Unknown"

		for _, p := range it.Status.Parents {
			for _, cond := range p.Conditions {
				if cond.Type == "Accepted" {
					if cond.Status == "True" && r.Accepted != "False" {
						r.Accepted = "True"
					} else if cond.Status == "False" {
						r.Accepted = "False"
					}
				}
			}
		}

		out = append(out, r)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})

	return out, nil
}

// KubeConfigMaps lists configmaps (keys and sizes).
func (c *Cluster) KubeConfigMaps(ctx context.Context, ns string) ([]KubeKeyed, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	l, err := kc.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make([]KubeKeyed, 0, len(l.Items))

	for _, cm := range l.Items {
		k := KubeKeyed{Namespace: cm.Namespace, Name: cm.Name, Keys: map[string]int{}, Created: cm.CreationTimestamp.Time}
		for key, v := range cm.Data {
			k.Keys[key] = len(v)
		}

		for key, v := range cm.BinaryData {
			k.Keys[key] = len(v)
		}

		out = append(out, k)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})

	return out, nil
}

// KubeConfigMap returns one configmap including text values.
func (c *Cluster) KubeConfigMap(ctx context.Context, ns, name string) (*KubeConfigMap, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	cm, err := kc.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	out := &KubeConfigMap{Namespace: ns, Name: name, Data: cm.Data, Binary: map[string]int{}}
	for k, v := range cm.BinaryData {
		out.Binary[k] = len(v)
	}

	if out.Data == nil {
		out.Data = map[string]string{}
	}

	return out, nil
}

// KubeSecrets lists secrets. Values are never read into the response: only
// key names and sizes are returned.
func (c *Cluster) KubeSecrets(ctx context.Context, ns string) ([]KubeKeyed, error) {
	kc, err := c.Kube(ctx)
	if err != nil {
		return nil, err
	}

	l, err := kc.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make([]KubeKeyed, 0, len(l.Items))

	for _, s := range l.Items {
		k := KubeKeyed{Namespace: s.Namespace, Name: s.Name, Type: string(s.Type), Keys: map[string]int{}, Created: s.CreationTimestamp.Time}
		for key, v := range s.Data {
			k.Keys[key] = len(v)
		}

		out = append(out, k)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})

	return out, nil
}

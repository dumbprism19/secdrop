// Package kube reads pod health from the user's current kubectl context.
// It is optional: without kubectl or a context, secdrop just shows no pods.
package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ErrGcloudAuth means kubectl's gcloud credential plugin has no valid login.
var ErrGcloudAuth = errors.New("kubectl couldn't get credentials from gcloud")

type Pod struct {
	Namespace, Name string
	Workload        string // owning Deployment, StatefulSet, DaemonSet or Job
	Phase           string // Running, Pending, Succeeded, Failed
	Reason          string // CrashLoopBackOff, ImagePullBackOff, OOMKilled…
	Ready, Total    int
	Restarts        int
	Started         time.Time
}

func (p Pod) Healthy() bool {
	return p.Phase == "Succeeded" || (p.Phase == "Running" && p.Ready == p.Total && p.Reason == "")
}

// Status is a short human label: "ready", or the reason it isn't.
func (p Pod) Status() string {
	switch {
	case p.Reason != "":
		return p.Reason
	case p.Phase == "Running" && p.Ready < p.Total:
		return "not ready"
	case p.Phase == "Running":
		return "ready"
	}
	return strings.ToLower(p.Phase)
}

// Snapshot is one read of the cluster.
type Snapshot struct {
	Context string
	Pods    []Pod
}

// Load lists pods in every namespace it is allowed to see, falling back to
// the context's default namespace.
func Load(ctx context.Context) (Snapshot, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return Snapshot{}, errors.New("kubectl not installed")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	name, err := run(ctx, "config", "current-context")
	if err != nil {
		return Snapshot{}, errors.New("no kubectl context")
	}
	snap := Snapshot{Context: strings.TrimSpace(string(name))}

	out, err := run(ctx, "get", "pods", "--all-namespaces", "-o", "json", "--request-timeout=10s")
	if err != nil {
		if out, err = run(ctx, "get", "pods", "-o", "json", "--request-timeout=10s"); err != nil {
			return snap, err
		}
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name, Namespace string
				OwnerReferences []struct {
					Kind, Name string
				} `json:"ownerReferences"`
			} `json:"metadata"`
			Status struct {
				Phase             string    `json:"phase"`
				StartTime         time.Time `json:"startTime"`
				ContainerStatuses []struct {
					Ready        bool `json:"ready"`
					RestartCount int  `json:"restartCount"`
					State        struct {
						Waiting *struct {
							Reason string `json:"reason"`
						} `json:"waiting"`
						Terminated *struct {
							Reason string `json:"reason"`
						} `json:"terminated"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return snap, err
	}
	for _, it := range list.Items {
		p := Pod{Namespace: it.Metadata.Namespace, Name: it.Metadata.Name, Phase: it.Status.Phase, Started: it.Status.StartTime}
		p.Workload = p.Name
		for _, o := range it.Metadata.OwnerReferences {
			p.Workload = o.Name
			if o.Kind == "ReplicaSet" {
				// A Deployment's ReplicaSet is "<deployment>-<pod-template-hash>".
				if i := strings.LastIndexByte(o.Name, '-'); i > 0 {
					p.Workload = o.Name[:i]
				}
			}
		}
		for _, c := range it.Status.ContainerStatuses {
			p.Total++
			if c.Ready {
				p.Ready++
			}
			p.Restarts += c.RestartCount
			if w := c.State.Waiting; w != nil && w.Reason != "" && w.Reason != "ContainerCreating" {
				p.Reason = w.Reason
			} else if t := c.State.Terminated; t != nil && t.Reason != "" && t.Reason != "Completed" && p.Phase != "Succeeded" {
				p.Reason = t.Reason
			}
		}
		snap.Pods = append(snap.Pods, p)
	}
	// Unhealthy pods first, then by name.
	sort.SliceStable(snap.Pods, func(i, j int) bool {
		a, b := snap.Pods[i], snap.Pods[j]
		if a.Healthy() != b.Healthy() {
			return !a.Healthy()
		}
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	return snap, nil
}

func run(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		// Drop klog's "F1028 12:25:50.161351 9634 cred.go:145] " prefix.
		if i := strings.Index(msg, "] "); i > 0 && i < 60 && strings.ContainsAny(msg[:1], "IWEF") {
			msg = msg[i+2:]
		}
		if strings.Contains(msg, "access token") || strings.Contains(msg, "gcloud") || strings.Contains(msg, "credential") {
			return out, ErrGcloudAuth
		}
		return out, errors.New(msg)
	}
	return out, nil
}

// BelongsTo reports whether the pod runs the given service: its workload is
// named after the service, possibly behind a Helm release prefix
// ("web-chart-orders-svc" runs "orders-svc"), or it lives in a namespace
// of that name.
func (p Pod) BelongsTo(service string) bool {
	return p.Workload == service || strings.HasSuffix(p.Workload, "-"+service) || p.Namespace == service
}

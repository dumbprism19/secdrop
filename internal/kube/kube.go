// Package kube reads pod health from the user's current kubectl context.
// It is optional: without kubectl or a context, secdrop just shows no pods.
package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
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

	out, err := getPods(ctx)
	if errors.Is(err, ErrGcloudAuth) {
		// kubectl's GKE plugin reads the gcloud CLI login, which can need
		// reauthentication even while secdrop's Application Default
		// Credentials are fine. Retry with the ADC token.
		if o, e := getPodsWithADC(ctx); e == nil {
			out, err = o, nil
		} else {
			err = fmt.Errorf("%w (retry with Application Default Credentials failed: %v)", ErrGcloudAuth, e)
		}
	}
	if err != nil {
		return snap, err
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

// getPods lists pods in all namespaces, falling back to the context's
// default namespace. extra flags (such as --kubeconfig) go first.
func getPods(ctx context.Context, extra ...string) ([]byte, error) {
	out, err := run(ctx, append(extra, "get", "pods", "--all-namespaces", "-o", "json", "--request-timeout=10s")...)
	if err == nil || errors.Is(err, ErrGcloudAuth) {
		return out, err
	}
	return run(ctx, append(extra, "get", "pods", "-o", "json", "--request-timeout=10s")...)
}

// getPodsWithADC runs getPods against a temporary copy of the current
// context whose user is replaced by an Application Default Credentials
// token. The token lives only in a 0600 file that is removed afterwards,
// so it never shows up in the process list.
func getPodsWithADC(ctx context.Context) ([]byte, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("no Application Default Credentials: %w", err)
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("Application Default Credentials can't mint a token: %w", err)
	}
	raw, err := run(ctx, "config", "view", "--minify", "--raw", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("can't read the current kubectl context: %w", err)
	}
	cfg, err := withToken(raw, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "secdrop-kubeconfig-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Write(cfg); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return getPods(ctx, "--kubeconfig", f.Name())
}

// withToken replaces every user's credentials in a minified kubeconfig with
// a bearer token, keeping clusters and contexts as they are.
func withToken(raw []byte, token string) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("can't parse kubectl config: %w", err)
	}
	users, _ := cfg["users"].([]any)
	if len(users) == 0 {
		return nil, errors.New("current kubectl context has no user")
	}
	for _, u := range users {
		um, ok := u.(map[string]any)
		if !ok {
			return nil, errors.New("unexpected kubectl config user entry")
		}
		um["user"] = map[string]any{"token": token}
	}
	return json.Marshal(cfg)
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

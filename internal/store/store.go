// Package store reads and writes Berglas secrets in Cloud Storage and finds
// the buckets that hold them.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/berglas/v2/pkg/berglas"
	"github.com/GoogleCloudPlatform/berglas/v2/pkg/berglas/logging"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"

	"github.com/dumbprism19/secdrop/internal/secret"
)

var (
	ErrConflict = errors.New("someone else changed this secret since you opened it")
	ErrDenied   = errors.New("permission denied")
)

// Secret is one Berglas object. It never holds plaintext.
type Secret struct {
	Bucket, Object string
	Service, Env   string
	Generation     int64
	Metageneration int64
	Updated        time.Time
}

func (s Secret) Ref() string { return "berglas://" + s.Bucket + "/" + s.Object }

// Bucket is a Cloud Storage bucket that holds Berglas secrets.
type Bucket struct {
	Name    string
	Project string
}

type Store struct {
	bc  *berglas.Client
	gcs *storage.Client
	hc  *http.Client
}

// quiet keeps the Berglas library from printing to stderr, which would draw
// over the TUI. Nothing secdrop does is logged.
func quiet(ctx context.Context) context.Context {
	return logging.WithLogger(ctx, slog.New(slog.DiscardHandler))
}

func New(ctx context.Context) (*Store, error) {
	ctx = quiet(ctx)
	bc, err := berglas.New(ctx)
	if err != nil {
		return nil, err
	}
	gcs, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	hc, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform.read-only")
	if err != nil {
		return nil, err
	}
	return &Store{bc: bc, gcs: gcs, hc: hc}, nil
}

// List returns every live secret in a bucket, sorted by service then environment.
func (s *Store) List(ctx context.Context, bucket string) ([]Secret, error) {
	resp, err := s.bc.List(quiet(ctx), &berglas.StorageListRequest{Bucket: bucket})
	if err != nil {
		return nil, classify(err)
	}
	out := make([]Secret, 0, len(resp.Secrets))
	for _, x := range resp.Secrets {
		svc, env := SplitName(x.Name)
		out = append(out, Secret{Bucket: bucket, Object: x.Name, Service: svc, Env: env,
			Generation: x.Generation, Metageneration: x.Metageneration, Updated: x.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return EnvOrder[out[i].Env] < EnvOrder[out[j].Env]
	})
	return out, nil
}

// Read decrypts the latest version of a secret.
func (s *Store) Read(ctx context.Context, sec Secret) (Secret, secret.Value, error) {
	x, err := s.bc.Read(quiet(ctx), &berglas.StorageReadRequest{Bucket: sec.Bucket, Object: sec.Object})
	if err != nil {
		return sec, secret.Value{}, classify(err)
	}
	v := secret.New(x.Plaintext)
	for i := range x.Plaintext {
		x.Plaintext[i] = 0
	}
	sec.Generation, sec.Metageneration, sec.Updated = x.Generation, x.Metageneration, x.UpdatedAt
	return sec, v, nil
}

// Write replaces a secret's plaintext. It only succeeds if the object is still
// at the generation we read, so a concurrent change is never overwritten.
func (s *Store) Write(ctx context.Context, sec Secret, plaintext []byte) (Secret, error) {
	x, err := s.bc.Update(quiet(ctx), &berglas.StorageUpdateRequest{
		Bucket: sec.Bucket, Object: sec.Object, Plaintext: plaintext,
		Generation: sec.Generation, Metageneration: sec.Metageneration,
	})
	if err != nil {
		return sec, classify(err)
	}
	sec.Generation, sec.Metageneration, sec.Updated = x.Generation, x.Metageneration, x.UpdatedAt
	return sec, nil
}

func classify(err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "conditionNotMet"), strings.Contains(s, "Error 412"), strings.Contains(s, "precondition"):
		return ErrConflict
	case strings.Contains(s, "403"), strings.Contains(s, "PermissionDenied"), strings.Contains(s, "permission"):
		if i := strings.Index(s, "does not have "); i >= 0 {
			rest := s[i+len("does not have "):]
			if j := strings.IndexAny(rest, " ,"); j > 0 {
				return fmt.Errorf("%w: missing %s", ErrDenied, rest[:j])
			}
		}
		return ErrDenied
	}
	return err
}

// CheckBucket confirms a bucket exists and holds at least one Berglas secret.
func (s *Store) CheckBucket(ctx context.Context, name string) error {
	ok, err := s.isBerglas(ctx, name)
	if err != nil {
		return classify(err)
	}
	if !ok {
		return fmt.Errorf("gs://%s has no Berglas secrets", name)
	}
	return nil
}

func (s *Store) isBerglas(ctx context.Context, bucket string) (bool, error) {
	q := &storage.Query{}
	if err := q.SetAttrSelection([]string{"Name", "Metadata"}); err != nil {
		return false, err
	}
	it := s.gcs.Bucket(bucket).Objects(ctx, q)
	for n := 0; n < 25; n++ {
		o, err := it.Next()
		if err == iterator.Done {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if o.Metadata[berglas.MetadataIDKey] == "1" {
			return true, nil
		}
	}
	return false, nil
}

// DiscoverEvent reports progress while searching for Berglas buckets.
type DiscoverEvent struct {
	Found          *Bucket
	Scanned, Total int
	Done           bool
	Err            error
}

// Discover looks through every project the user can see, starting with the
// preferred ones, and reports each bucket that holds Berglas secrets.
func (s *Store) Discover(ctx context.Context, preferred []string) <-chan DiscoverEvent {
	out := make(chan DiscoverEvent, 16)
	go func() {
		defer close(out)
		// Sends stop when the caller cancels; the scan itself also stops after 2 minutes.
		parent := ctx
		send := func(ev DiscoverEvent) {
			select {
			case out <- ev:
			case <-parent.Done():
			}
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		projects := dedupe(preferred)
		listed, err := s.projects(ctx)
		projects = dedupe(append(projects, listed...))
		if len(projects) == 0 {
			if err == nil {
				err = errors.New("no Google Cloud projects are visible to your account")
			}
			send(DiscoverEvent{Done: true, Err: classify(err)})
			return
		}

		var (
			mu      sync.Mutex
			scanned int
			wg      sync.WaitGroup
			jobs    = make(chan string)
		)
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for p := range jobs {
					it := s.gcs.Buckets(ctx, p)
					for {
						b, err := it.Next()
						if err != nil {
							break
						}
						if ok, _ := s.isBerglas(ctx, b.Name); ok {
							send(DiscoverEvent{Found: &Bucket{Name: b.Name, Project: p}})
						}
					}
					mu.Lock()
					scanned++
					ev := DiscoverEvent{Scanned: scanned, Total: len(projects)}
					mu.Unlock()
					send(ev)
				}
			}()
		}
		for _, p := range projects {
			select {
			case jobs <- p:
			case <-ctx.Done():
			}
		}
		close(jobs)
		wg.Wait()
		send(DiscoverEvent{Done: true, Scanned: scanned, Total: len(projects)})
	}()
	return out
}

// projects lists active projects through the Resource Manager REST API.
func (s *Store) projects(ctx context.Context) ([]string, error) {
	var ids []string
	token := ""
	for len(ids) < 500 {
		u := "https://cloudresourcemanager.googleapis.com/v1/projects?pageSize=200&filter=" + url.QueryEscape("lifecycleState:ACTIVE")
		if token != "" {
			u += "&pageToken=" + url.QueryEscape(token)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return ids, err
		}
		resp, err := s.hc.Do(req)
		if err != nil {
			return ids, err
		}
		var page struct {
			Projects []struct {
				ProjectID string `json:"projectId"`
			} `json:"projects"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return ids, fmt.Errorf("listing projects: %s", resp.Status)
		}
		if err != nil {
			return ids, err
		}
		for _, p := range page.Projects {
			ids = append(ids, p.ProjectID)
		}
		if page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}
	return ids, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

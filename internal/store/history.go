package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/berglas/v2/pkg/berglas"
	"google.golang.org/api/iterator"

	"github.com/dumbprism19/secdrop/internal/secret"
)

// Version is one stored generation of a secret. Old generations exist only
// when the bucket has Object Versioning turned on.
type Version struct {
	Generation int64
	Updated    time.Time
	Live       bool
}

// History lists a secret's versions, newest first.
func (s *Store) History(ctx context.Context, sec Secret) ([]Version, error) {
	q := &storage.Query{Prefix: sec.Object, Versions: true}
	if err := q.SetAttrSelection([]string{"Name", "Generation", "Updated", "Deleted", "Metadata"}); err != nil {
		return nil, err
	}
	var out []Version
	it := s.gcs.Bucket(sec.Bucket).Objects(quiet(ctx), q)
	for {
		o, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, classify(err)
		}
		if o.Name != sec.Object || o.Metadata[berglas.MetadataIDKey] != "1" {
			continue
		}
		out = append(out, Version{Generation: o.Generation, Updated: o.Updated, Live: o.Deleted.IsZero() && o.Generation == sec.Generation})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Generation > out[j].Generation })
	if len(out) > 0 && !out[0].Live && out[0].Generation == sec.Generation {
		out[0].Live = true
	}
	return out, nil
}

// ReadVersion decrypts one specific generation.
func (s *Store) ReadVersion(ctx context.Context, sec Secret, gen int64) (secret.Value, error) {
	x, err := s.bc.Read(quiet(ctx), &berglas.StorageReadRequest{Bucket: sec.Bucket, Object: sec.Object, Generation: gen})
	if err != nil {
		return secret.Value{}, classify(err)
	}
	v := secret.New(x.Plaintext)
	for i := range x.Plaintext {
		x.Plaintext[i] = 0
	}
	return v, nil
}

// Event is one audit log entry about a secret.
type Event struct {
	Time   time.Time
	Who    string
	Kind   string // "read", "write", "delete"
	Denied bool
}

var (
	// ErrAuditDenied means the user can't read Data Access audit logs.
	ErrAuditDenied = errors.New("reading these audit logs needs roles/logging.privateLogViewer on the bucket's project")
)

// BucketInfo is what history and audit need to know about a bucket.
type BucketInfo struct {
	ProjectNumber uint64
	Versioning    bool
}

var (
	infoMu sync.Mutex
	infos  = map[string]BucketInfo{}
)

func (s *Store) Bucket(ctx context.Context, bucket string) (BucketInfo, error) {
	infoMu.Lock()
	bi, ok := infos[bucket]
	infoMu.Unlock()
	if ok {
		return bi, nil
	}
	a, err := s.gcs.Bucket(bucket).Attrs(quiet(ctx))
	if err != nil {
		return BucketInfo{}, classify(err)
	}
	bi = BucketInfo{ProjectNumber: a.ProjectNumber, Versioning: a.VersioningEnabled}
	infoMu.Lock()
	infos[bucket] = bi
	infoMu.Unlock()
	return bi, nil
}

// Audit reads the last 30 days of Cloud Audit Logs for a secret: who read
// (decrypted), wrote or deleted it. Cloud Storage records these only when
// Data Access audit logs are on for the project.
func (s *Store) Audit(ctx context.Context, sec Secret) ([]Event, error) {
	bi, err := s.Bucket(ctx, sec.Bucket)
	if err != nil {
		return nil, err
	}
	num := bi.ProjectNumber
	filter := fmt.Sprintf(`logName:"cloudaudit.googleapis.com" AND protoPayload.serviceName="storage.googleapis.com" `+
		`AND protoPayload.resourceName="projects/_/buckets/%s/objects/%s" AND timestamp>="%s"`,
		sec.Bucket, sec.Object, time.Now().Add(-30*24*time.Hour).UTC().Format(time.RFC3339))
	body, _ := json.Marshal(map[string]any{
		"resourceNames": []string{fmt.Sprintf("projects/%d", num)},
		"filter":        filter,
		"orderBy":       "timestamp desc",
		"pageSize":      100,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://logging.googleapis.com/v2/entries:list", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrAuditDenied
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading audit logs: %s", resp.Status)
	}
	var page struct {
		Entries []struct {
			Timestamp    time.Time `json:"timestamp"`
			ProtoPayload struct {
				MethodName         string `json:"methodName"`
				AuthenticationInfo struct {
					PrincipalEmail string `json:"principalEmail"`
				} `json:"authenticationInfo"`
				Status struct {
					Code int `json:"code"`
				} `json:"status"`
			} `json:"protoPayload"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, err
	}
	var out []Event
	for _, e := range page.Entries {
		kind := ""
		switch m := e.ProtoPayload.MethodName; {
		case strings.HasSuffix(m, "objects.get"):
			kind = "read"
		case strings.HasSuffix(m, "objects.create"), strings.HasSuffix(m, "objects.update"):
			kind = "write"
		case strings.HasSuffix(m, "objects.delete"):
			kind = "delete"
		default:
			continue
		}
		who := e.ProtoPayload.AuthenticationInfo.PrincipalEmail
		if who == "" {
			who = "unknown caller"
		}
		out = append(out, Event{Time: e.Timestamp, Who: who, Kind: kind, Denied: e.ProtoPayload.Status.Code != 0})
	}
	return out, nil
}

// FindSecret looks a secret up by service and environment (any alias, such as
// "production"), or by its exact object name.
func FindSecret(secrets []Secret, service, env string) (Secret, error) {
	want := CanonicalEnv(env)
	var names []string
	for _, s := range secrets {
		if (s.Service == service && s.Env == want) || (env == "" && s.Object == service) {
			return s, nil
		}
		if s.Service == service {
			names = append(names, orDash(s.Env))
		}
	}
	if len(names) > 0 {
		return Secret{}, fmt.Errorf("%s has no %q environment; it has %s", service, env, strings.Join(names, ", "))
	}
	return Secret{}, fmt.Errorf("no service called %q in this bucket", service)
}

// CanonicalEnv maps an environment alias ("production", "stg") to the name
// secdrop shows ("prod", "staging").
func CanonicalEnv(env string) string {
	if c, ok := envAliases[strings.ToLower(env)]; ok {
		return c
	}
	return strings.ToLower(env)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

package gcloud_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeGCP serves Cloud Run, Secret Manager and Resource Manager from one
// server — the three base URLs all point at it.
type fakeGCP struct {
	mu            sync.Mutex
	service       map[string]any
	patched       map[string]any
	secrets       map[string]bool
	versions      map[string][]string
	policies      map[string]map[string]any
	setPolicyHits int
	projectHits   int
}

func newFakeGCP(t *testing.T, service string) (*fakeGCP, *httptest.Server) {
	t.Helper()
	f := &fakeGCP{secrets: map[string]bool{}, versions: map[string][]string{}, policies: map[string]map[string]any{}}
	require.NoError(t, json.Unmarshal([]byte(service), &f.service))
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeGCP) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	switch {
	case path == "/v2/"+testServiceName && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(f.service)
	case path == "/v2/"+testServiceName && r.Method == http.MethodPatch:
		_ = json.Unmarshal(body, &f.patched)
		_, _ = w.Write([]byte(`{"name":"operations/1"}`))
	case path == "/v1/projects/demo-project/secrets" && r.Method == http.MethodPost:
		id := r.URL.Query().Get("secretId")
		if f.secrets[id] {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"status":"ALREADY_EXISTS"}}`))
			return
		}
		f.secrets[id] = true
		_, _ = w.Write([]byte(`{}`))
	case strings.HasSuffix(path, ":addVersion"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/projects/demo-project/secrets/"), ":addVersion")
		var req struct {
			Payload struct {
				Data string `json:"data"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(body, &req)
		raw, _ := base64.StdEncoding.DecodeString(req.Payload.Data)
		f.versions[id] = append(f.versions[id], string(raw))
		_, _ = w.Write([]byte(`{}`))
	case strings.HasSuffix(path, ":getIamPolicy") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/projects/demo-project/secrets/"), ":getIamPolicy")
		_ = json.NewEncoder(w).Encode(f.policies[id])
	case strings.HasSuffix(path, ":setIamPolicy"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/projects/demo-project/secrets/"), ":setIamPolicy")
		var req struct {
			Policy map[string]any `json:"policy"`
		}
		_ = json.Unmarshal(body, &req)
		f.policies[id] = req.Policy
		f.setPolicyHits++
		_, _ = w.Write([]byte(`{}`))
	case path == "/v1/projects/demo-project":
		f.projectHits++
		_, _ = w.Write([]byte(`{"projectNumber":"123456"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const runServiceWithEnv = `{
	"name": "` + testServiceName + `",
	"etag": "e1",
	"template": {
		"revision": "api-manual-rev",
		"serviceAccount": "runtime@demo-project.iam.gserviceaccount.com",
		"containers": [
			{"name": "sidecar", "image": "proxy"},
			{"name": "app", "image": "gcr.io/demo/api", "ports": [{"containerPort": 8080}],
			 "env": [{"name": "GITHUB_REPO", "value": "o/r"}, {"name": "OLD_SECRET", "valueSource": {"secretKeyRef": {"secret": "x", "version": "1"}}}]}
		]
	}
}`

func envTestProvider(t *testing.T, srv *httptest.Server) (*gcloud.Provider, domain.CloudCredential) {
	tok := newTokenServer(t)
	p := newTestProvider(t, tok.URL, srv.URL)
	p.SetSecretManagerBaseURL(srv.URL)
	p.SetResourceManagerBaseURL(srv.URL)
	return p, testCredential(t, uuid.New(), tok.URL)
}

func TestCloudRunEnvVarsAreReadFromTheServingContainer(t *testing.T) {
	_, srv := newFakeGCP(t, runServiceWithEnv)
	p, cred := envTestProvider(t, srv)

	vars, err := p.ListEnvVars(context.Background(), cred, testServiceRef())

	require.NoError(t, err)
	require.Len(t, vars, 2)
	assert.Equal(t, "GITHUB_REPO", vars[0].Key)
	assert.Equal(t, "OLD_SECRET", vars[1].Key)
	assert.True(t, vars[0].SetFor(domain.EnvironmentProduction))
}

func TestCloudRunSecretsGoToSecretManagerAndNeverIntoTheService(t *testing.T) {
	f, srv := newFakeGCP(t, runServiceWithEnv)
	p, cred := envTestProvider(t, srv)

	err := p.UpsertEnvVars(context.Background(), cred, testServiceRef(), []domain.CloudEnvWrite{
		{Key: "SESSION_SECRET", Value: "s3cret", Sensitive: true},
		{Key: "GITHUB_REPO", Value: "o/r2"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"s3cret"}, f.versions["tt-api-SESSION_SECRET"])
	policy := f.policies["tt-api-SESSION_SECRET"]
	bindings, _ := json.Marshal(policy["bindings"])
	assert.Contains(t, string(bindings), "roles/secretmanager.secretAccessor")
	assert.Contains(t, string(bindings), "serviceAccount:runtime@demo-project.iam.gserviceaccount.com")
	assert.Zero(t, f.projectHits, "the template's own service account is used")

	patched, _ := json.Marshal(f.patched)
	assert.NotContains(t, string(patched), "s3cret")
	assert.NotContains(t, string(patched), "api-manual-rev", "a named revision is not reused")
	assert.Contains(t, string(patched), `"secretKeyRef":{"secret":"tt-api-SESSION_SECRET","version":"latest"}`)
	assert.Contains(t, string(patched), `"value":"o/r2"`)
	assert.Contains(t, string(patched), "OLD_SECRET", "existing variables are kept")
	assert.Contains(t, string(patched), `"image":"proxy"`, "the sidecar is sent back untouched")
}

func TestCloudRunSecondWriteAddsAVersionAndKeepsTheGrant(t *testing.T) {
	f, srv := newFakeGCP(t, runServiceWithEnv)
	p, cred := envTestProvider(t, srv)
	write := []domain.CloudEnvWrite{{Key: "GITHUB_TOKEN", Value: "ghp_1", Sensitive: true}}

	require.NoError(t, p.UpsertEnvVars(context.Background(), cred, testServiceRef(), write))
	write[0].Value = "ghp_2"
	require.NoError(t, p.UpsertEnvVars(context.Background(), cred, testServiceRef(), write))

	assert.Equal(t, []string{"ghp_1", "ghp_2"}, f.versions["tt-api-GITHUB_TOKEN"])
	assert.Equal(t, 1, f.setPolicyHits, "an existing grant is not written again")
}

func TestCloudRunFallsBackToTheDefaultComputeAccount(t *testing.T) {
	f, srv := newFakeGCP(t, `{"name":"`+testServiceName+`","template":{"containers":[{"image":"x"}]}}`)
	p, cred := envTestProvider(t, srv)

	require.NoError(t, p.UpsertEnvVars(context.Background(), cred, testServiceRef(), []domain.CloudEnvWrite{
		{Key: "API_KEY", Value: "k", Sensitive: true},
	}))

	bindings, _ := json.Marshal(f.policies["tt-api-API_KEY"]["bindings"])
	assert.Contains(t, string(bindings), "serviceAccount:123456-compute@developer.gserviceaccount.com")
}

func TestCloudRunEnvIsLimitedToServices(t *testing.T) {
	p := gcloud.NewProvider()
	ref := testServiceRef()
	ref.Kind = domain.CloudResourceCloudRunJob

	_, err := p.EnvCapabilities(ref)
	assert.ErrorIs(t, err, port.ErrUnsupported)

	caps, err := p.EnvCapabilities(testServiceRef())
	require.NoError(t, err)
	assert.True(t, caps.WritesRollOut)
	assert.False(t, caps.Supports(domain.EnvironmentPreview))
}

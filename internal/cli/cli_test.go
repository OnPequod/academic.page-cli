package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/config"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

type recorded struct {
	Method, Path, Query, ContentType string
	Body                             []byte
}

type fakeAPI struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), body})
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer good-key" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"message":"Unauthenticated.","code":"missing_api_token"}`)
			return
		}
		switch {
		case r.URL.Path == "/content/sites":
			fmt.Fprint(w, `{"data":[{"id":7,"name":"Lab"}],"links":{"next":null},"meta":{"current_page":1,"last_page":1,"total":1}}`)
		case r.URL.Path == "/content/sites/7/posts":
			fmt.Fprint(w, `{"data":[{"id":1,"title":"Hello","slug":"hello","published_at":"2026-01-01T00:00:00Z"}],"links":{"next":null},"meta":{"current_page":1,"last_page":1,"total":1}}`)
		case r.URL.Path == "/content/sites/7/posts/404":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not found.","code":"not_found"}`)
		case r.URL.Path == "/sites/7/posts" && r.Method == "POST":
			var in map[string]any
			_ = json.Unmarshal(body, &in)
			if in["title"] == nil {
				w.WriteHeader(422)
				fmt.Fprint(w, `{"message":"The title field is required.","errors":{"title":["The title field is required."]}}`)
				return
			}
			w.WriteHeader(201)
			out, _ := json.Marshal(map[string]any{"data": map[string]any{"id": 9, "title": in["title"], "published_at": in["published_at"]}})
			w.Write(out)
		case r.URL.Path == "/sites/7/posts/9" && r.Method == "DELETE":
			w.WriteHeader(204)
		case r.URL.Path == "/sites/7/media/multipart/init":
			// Multipart itself is covered in package app; here, exercise the
			// single-request fallback.
			w.WriteHeader(422)
			fmt.Fprint(w, `{"message":"Multipart uploads are not available.","code":"multipart_upload_not_supported"}`)
		case r.URL.Path == "/sites/7/media" && r.Method == "POST":
			w.WriteHeader(201)
			fmt.Fprint(w, `{"data":{"id":3,"filename":"cv.pdf"}}`)
		case r.URL.Path == "/content/sites/7/publications.bib":
			fmt.Fprint(w, "@article{a, title={A}}\n")
		default:
			w.WriteHeader(404)
			fmt.Fprintf(w, `{"message":"No route %s","code":"not_found"}`, r.URL.Path)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return recorded{}
	}
	return f.requests[len(f.requests)-1]
}

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	reg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	c := &CLI{
		Build:           BuildInfo{Version: "test"},
		Registry:        reg,
		Stdin:           strings.NewReader(stdin),
		Stdout:          &out,
		Stderr:          &errOut,
		StdinIsTerminal: func() bool { return false },
	}
	code := c.Run(context.Background(), args)
	return result{code, out.String(), errOut.String()}
}

func setup(t *testing.T) *fakeAPI {
	f := newFakeAPI(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(config.EnvBaseURL, f.URL)
	t.Setenv(config.EnvToken, "good-key")
	t.Setenv(config.EnvSite, "7")
	t.Setenv(config.EnvProfile, "")
	return f
}

func TestListJSON(t *testing.T) {
	f := setup(t)
	r := run(t, "", "posts", "list", "--json", "--per-page", "5")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, `"title": "Hello"`) {
		t.Errorf("stdout = %s", r.stdout)
	}
	if got := f.last(); got.Path != "/content/sites/7/posts" || got.Query != "per_page=5" {
		t.Errorf("request = %+v", got)
	}
}

func TestListTable(t *testing.T) {
	setup(t)
	r := run(t, "", "posts", "list")
	if r.code != 0 || !strings.Contains(r.stdout, "ID") || !strings.Contains(r.stdout, "Hello") {
		t.Errorf("code %d, stdout %q", r.code, r.stdout)
	}
}

func TestCreateDefaultsToDraft(t *testing.T) {
	f := setup(t)
	r := run(t, "", "posts", "create", "--title", "New", "--content-markdown", "Hello **world**")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, `Created post 9 "New" (draft).`) {
		t.Errorf("stdout = %q", r.stdout)
	}
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if v, ok := body["published_at"]; !ok || v != nil {
		t.Errorf("published_at should be sent as null, body = %s", f.last().Body)
	}
	if doc, _ := body["content_structured"].(map[string]any); doc["type"] != "doc" {
		t.Errorf("content_structured = %v", body["content_structured"])
	}
}

func TestPublish(t *testing.T) {
	f := setup(t)
	r := run(t, "", "posts", "create", "--title", "New", "--publish")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if s, _ := body["published_at"].(string); s == "" {
		t.Errorf("published_at = %v", body["published_at"])
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	f := setup(t)
	r := run(t, "", "posts", "create", "--title", "X", "--dry-run", "--json")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if len(f.requests) != 0 {
		t.Errorf("dry run sent %d requests", len(f.requests))
	}
	var plan app.Plan
	if err := json.Unmarshal([]byte(r.stdout), &plan); err != nil || plan.Method != "POST" || !strings.HasSuffix(plan.URL, "/sites/7/posts") {
		t.Errorf("plan = %s (%v)", r.stdout, err)
	}
}

func TestDataFlagAndUnknownField(t *testing.T) {
	setup(t)
	r := run(t, `{"title":"From stdin","bogus":1}`, "posts", "create", "--data", "-")
	if r.code != app.ExitUsage || !strings.Contains(r.stderr, `no body field "bogus"`) {
		t.Errorf("code %d, stderr %q", r.code, r.stderr)
	}
}

func TestExitCodes(t *testing.T) {
	setup(t)
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"posts", "get", "404"}, app.ExitNotFound},
		{[]string{"posts", "get"}, app.ExitUsage},
		{[]string{"posts", "get", "abc"}, app.ExitUsage},
		{[]string{"posts", "create"}, app.ExitValidation},
		{[]string{"posts", "delete", "9"}, app.ExitUsage},
		{[]string{"posts", "delete", "9", "--yes"}, app.ExitOK},
		{[]string{"posts", "frobnicate"}, app.ExitUsage},
		{[]string{"posts", "list", "--nope"}, app.ExitUsage},
		{[]string{"posts", "list", "--drafts", "--slug", "x"}, app.ExitUsage},
	}
	for _, tc := range cases {
		if r := run(t, "", tc.args...); r.code != tc.code {
			t.Errorf("%v: code %d, want %d (stderr %q)", tc.args, r.code, tc.code, r.stderr)
		}
	}
}

func TestErrorJSON(t *testing.T) {
	setup(t)
	t.Setenv(config.EnvToken, "bad-key")
	r := run(t, "", "posts", "list", "--json")
	if r.code != app.ExitAuth {
		t.Fatalf("code %d", r.code)
	}
	var out struct {
		Error app.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &out); err != nil {
		t.Fatalf("stderr is not JSON: %q", r.stderr)
	}
	if out.Error.Code != "missing_api_token" || out.Error.Hint == "" || out.Error.ExitCode != app.ExitAuth {
		t.Errorf("error = %+v", out.Error)
	}
}

func TestBibtexIsText(t *testing.T) {
	setup(t)
	r := run(t, "", "publications", "bibtex")
	if r.code != 0 || r.stdout != "@article{a, title={A}}\n" {
		t.Errorf("code %d, stdout %q", r.code, r.stdout)
	}
}

func TestUpload(t *testing.T) {
	f := setup(t)
	path := filepath.Join(t.TempDir(), "cv.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 test"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := run(t, "", "media", "upload", path, "--alt-text", "My CV")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	got := f.last()
	if !strings.HasPrefix(got.ContentType, "multipart/form-data") || !bytes.Contains(got.Body, []byte("My CV")) || !bytes.Contains(got.Body, []byte(`filename="cv.pdf"`)) {
		t.Errorf("upload request = %s %s", got.ContentType, got.Body)
	}
}

func TestLoginStoresKeyAndPicksOnlySite(t *testing.T) {
	setup(t)
	t.Setenv(config.EnvToken, "")
	t.Setenv(config.EnvSite, "")
	r := run(t, "good-key\n", "auth", "login", "--token-stdin")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, "good-key") {
		t.Error("the key was printed")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["default"]
	if p == nil || p.Token != "good-key" || p.Site != 7 {
		t.Errorf("profile = %+v", p)
	}
	info, _ := os.Stat(cfg.File())
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v", info.Mode().Perm())
	}

	// The stored profile is now enough to run a command.
	if r := run(t, "", "posts", "list", "--json"); r.code != 0 {
		t.Errorf("after login: code %d: %s", r.code, r.stderr)
	}
	if r := run(t, "", "auth", "status", "--json"); r.code != 0 || strings.Contains(r.stdout, "good-key") {
		t.Errorf("status: code %d, %s", r.code, r.stdout)
	}
}

func TestLoginRejectsBadKey(t *testing.T) {
	setup(t)
	t.Setenv(config.EnvToken, "")
	r := run(t, "bad-key\n", "auth", "login", "--token-stdin")
	if r.code != app.ExitAuth {
		t.Errorf("code %d", r.code)
	}
	cfg, _ := config.Load()
	if p := cfg.Profiles["default"]; p != nil && p.Token != "" {
		t.Error("a rejected key was saved")
	}
}

func TestSchema(t *testing.T) {
	r := run(t, "", "schema")
	var out struct {
		Commands []struct {
			Command string         `json:"command"`
			MCPTool string         `json:"mcp_tool"`
			Input   map[string]any `json:"input_schema"`
		} `json:"commands"`
		ExitCodes []any `json:"exit_codes"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if len(out.Commands) < 30 || len(out.ExitCodes) != 11 {
		t.Errorf("commands %d, exit codes %d", len(out.Commands), len(out.ExitCodes))
	}
	if r := run(t, "", "schema", "posts", "create"); r.code != 0 || !strings.Contains(r.stdout, `"--content-markdown"`) {
		t.Errorf("schema posts create: %d %s", r.code, r.stdout)
	}
}

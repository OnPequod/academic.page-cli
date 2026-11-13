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

	"github.com/OnPequod/academic.page-cli/internal/api"
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
			fmt.Fprint(w, `{"data":[{"id":7,"name":"Lab","url":"https://lab.academic.page","custom_domain":null}],"links":{"next":null},"meta":{"current_page":1,"last_page":1,"total":1}}`)
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
		case r.URL.Path == "/sites/7/publications" && r.Method == "POST":
			w.WriteHeader(409)
			fmt.Fprint(w, `{"message":"This publication is already on the site.","code":"publication_exists","match":{"id":12,"title":"A"}}`)
		case r.URL.Path == "/sites/7/posts/9" && r.Method == "PATCH":
			w.WriteHeader(409)
			fmt.Fprint(w, `{"message":"The post was edited since you read it.","code":"edit_conflict","current_edit_version":4}`)
		case r.URL.Path == "/sites/7/publications/import", r.URL.Path == "/sites/7/profile/degrees/order",
			r.URL.Path == "/sites/7/profile/honors", r.URL.Path == "/sites/7/profile/honors/order",
			r.URL.Path == "/sites/7/profile/service", r.URL.Path == "/sites/7/profile/service/order",
			r.URL.Path == "/sites/7/profile/memberships", r.URL.Path == "/sites/7/profile/memberships/order",
			r.URL.Path == "/sites/7/profile/grants", r.URL.Path == "/sites/7/profile/grants/order",
			r.URL.Path == "/sites/7/talks", r.URL.Path == "/sites/7/courses/3/lessons/5":
			fmt.Fprint(w, `{"data":{"id":5,"title":"Echo"}}`)
		case strings.HasPrefix(r.URL.Path, "/sites/7/section-order/"):
			fmt.Fprint(w, `{"data":{"section":"publications","order":"custom"}}`)
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
	if body["content_markdown"] != "Hello **world**" || body["content_structured"] != nil {
		t.Errorf("Markdown should pass through to the API, body = %s", f.last().Body)
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
	if !strings.Contains(r.stdout, "The key can read 1 site.") || !strings.Contains(r.stdout, "Default site: lab.academic.page (Lab).") {
		t.Errorf("login summary: %q", r.stdout)
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

func TestNewResources(t *testing.T) {
	f := setup(t)

	r := run(t, "", "course-lessons", "update", "3", "5", "--title", "Week 1")
	if r.code != 0 || f.last().Path != "/sites/7/courses/3/lessons/5" || f.last().Method != "PATCH" {
		t.Fatalf("lesson update: code %d, %+v, stderr %q", r.code, f.last(), r.stderr)
	}
	if r := run(t, "", "lesson-materials", "update", "3", "5", "8", "--dry-run", "--json"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/courses/3/lessons/5/materials/8") {
		t.Errorf("material update: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "profile-cv", "delete", "--yes", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "DELETE") {
		t.Errorf("cv delete: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "course-lessons", "get", "3"); r.code != app.ExitUsage {
		t.Errorf("lesson get with one argument: code %d", r.code)
	}

	bib := filepath.Join(t.TempDir(), "refs.bib")
	if err := os.WriteFile(bib, []byte("@article{a, title={A}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	r = run(t, "", "publications", "import", "--bibtex-file", bib, "--preview", "--select", "0,a")
	last := f.last()
	if r.code != 0 || last.Query != "dry_run=true&select=0%2Ca" || !strings.Contains(string(last.Body), `"bibtex":"@article{a, title={A}}"`) {
		t.Fatalf("import: code %d, %+v, stderr %q", r.code, last, r.stderr)
	}

	r = run(t, "", "profile-degrees", "reorder", "--ids", "3,1,2")
	if r.code != 0 || string(f.last().Body) != `{"ids":[3,1,2]}` {
		t.Fatalf("reorder: code %d, body %s, stderr %q", r.code, f.last().Body, r.stderr)
	}

	r = run(t, "", "talks", "create", "--title", "T", "--abstract-markdown", "**Hi**", "--tag-ids", "4")
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if r.code != 0 || body["abstract_markdown"] != "**Hi**" || body["published_at"] != nil || fmt.Sprint(body["tag_ids"]) != "[4]" {
		t.Fatalf("talk create: code %d, body %s, stderr %q", r.code, f.last().Body, r.stderr)
	}
	if _, ok := body["published_at"]; !ok {
		t.Error("talk create should send published_at: null")
	}
}

func TestProfileHonors(t *testing.T) {
	f := setup(t)

	r := run(t, "", "profile-honors", "create", "--name", "Early Career Award", "--organization", "Society for Clinical Trials", "--year", "2016", "--url", "https://example.org/award")
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if r.code != 0 || f.last().Method != "POST" || f.last().Path != "/sites/7/profile/honors" {
		t.Fatalf("honor create: code %d, %+v, stderr %q", r.code, f.last(), r.stderr)
	}
	if body["name"] != "Early Career Award" || body["organization"] != "Society for Clinical Trials" || fmt.Sprint(body["year"]) != "2016" || body["url"] != "https://example.org/award" {
		t.Fatalf("honor create body: %s", f.last().Body)
	}

	if r := run(t, "", "profile-honors", "update", "4", "--description", "For trial design.", "--dry-run", "--json"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/profile/honors/4") || !strings.Contains(r.stdout, "PATCH") {
		t.Errorf("honor update: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "profile-honors", "reorder", "--ids", "4,2"); r.code != 0 || string(f.last().Body) != `{"ids":[4,2]}` {
		t.Errorf("honor reorder: code %d, body %s, stderr %q", r.code, f.last().Body, r.stderr)
	}
	if r := run(t, "", "profile-honors", "delete", "4", "--yes", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "DELETE") {
		t.Errorf("honor delete: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	// The API refuses a create with no name; the schema tells an agent so first.
	if r := run(t, "", "schema", "profile-honors", "create"); r.code != 0 || !strings.Contains(r.stdout, `"--name"`) || !strings.Contains(r.stdout, `"required": true`) {
		t.Errorf("schema profile-honors create: code %d, stdout %s", r.code, r.stdout)
	}
}

func TestProfileMemberships(t *testing.T) {
	f := setup(t)

	r := run(t, "", "profile-memberships", "create", "--organization", "American Statistical Association", "--membership-type", "Member", "--start-year", "2012", "--current")
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if r.code != 0 || f.last().Method != "POST" || f.last().Path != "/sites/7/profile/memberships" {
		t.Fatalf("membership create: code %d, %+v, stderr %q", r.code, f.last(), r.stderr)
	}
	if body["organization"] != "American Statistical Association" || body["membership_type"] != "Member" || fmt.Sprint(body["start_year"]) != "2012" || body["current"] != true {
		t.Fatalf("membership create body: %s", f.last().Body)
	}

	if r := run(t, "", "profile-memberships", "update", "4", "--end-year", "2023", "--dry-run", "--json"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/profile/memberships/4") || !strings.Contains(r.stdout, "PATCH") {
		t.Errorf("membership update: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "profile-memberships", "reorder", "--ids", "4,2"); r.code != 0 || string(f.last().Body) != `{"ids":[4,2]}` {
		t.Errorf("membership reorder: code %d, body %s, stderr %q", r.code, f.last().Body, r.stderr)
	}
	if r := run(t, "", "profile-memberships", "delete", "4", "--yes", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "DELETE") {
		t.Errorf("membership delete: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "schema", "profile-memberships", "create"); r.code != 0 || !strings.Contains(r.stdout, `"--organization"`) || !strings.Contains(r.stdout, `"required": true`) {
		t.Errorf("schema profile-memberships create: code %d, stdout %s", r.code, r.stdout)
	}
}

func TestProfileGrants(t *testing.T) {
	f := setup(t)

	r := run(t, "", "profile-grants", "create", "--title", "Methods for Missing Data", "--funder", "National Institutes of Health", "--award-number", "R01 AG012345", "--role", "co_pi", "--status", "pending", "--amount", "250000", "--currency", "USD", "--show-amount", "--project-id", "12")
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if r.code != 0 || f.last().Method != "POST" || f.last().Path != "/sites/7/profile/grants" {
		t.Fatalf("grant create: code %d, %+v, stderr %q", r.code, f.last(), r.stderr)
	}
	if body["title"] != "Methods for Missing Data" || body["award_number"] != "R01 AG012345" || body["role"] != "co_pi" || body["status"] != "pending" || fmt.Sprint(body["amount"]) != "250000" || body["show_amount"] != true || fmt.Sprint(body["project_id"]) != "12" {
		t.Fatalf("grant create body: %s", f.last().Body)
	}

	if r := run(t, "", "profile-grants", "update", "4", "--status", "completed", "--dry-run", "--json"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/profile/grants/4") || !strings.Contains(r.stdout, "PATCH") {
		t.Errorf("grant update: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "profile-grants", "reorder", "--ids", "4,2"); r.code != 0 || string(f.last().Body) != `{"ids":[4,2]}` {
		t.Errorf("grant reorder: code %d, body %s, stderr %q", r.code, f.last().Body, r.stderr)
	}
	if r := run(t, "", "profile-grants", "delete", "4", "--yes", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "DELETE") {
		t.Errorf("grant delete: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "schema", "profile-grants", "create"); r.code != 0 || !strings.Contains(r.stdout, `"--title"`) || !strings.Contains(r.stdout, `"required": true`) {
		t.Errorf("schema profile-grants create: code %d, stdout %s", r.code, r.stdout)
	}
}

func TestProfileService(t *testing.T) {
	f := setup(t)

	r := run(t, "", "profile-service", "create", "--kind", "reviewing", "--title", "Reviewer", "--organization", "Statistics in Medicine", "--start-year", "2019", "--current")
	var body map[string]any
	_ = json.Unmarshal(f.last().Body, &body)
	if r.code != 0 || f.last().Method != "POST" || f.last().Path != "/sites/7/profile/service" {
		t.Fatalf("service create: code %d, %+v, stderr %q", r.code, f.last(), r.stderr)
	}
	if body["kind"] != "reviewing" || body["title"] != "Reviewer" || body["organization"] != "Statistics in Medicine" || fmt.Sprint(body["start_year"]) != "2019" || body["current"] != true {
		t.Fatalf("service create body: %s", f.last().Body)
	}

	if r := run(t, "", "profile-service", "update", "4", "--end-year", "2023", "--dry-run", "--json"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/profile/service/4") || !strings.Contains(r.stdout, "PATCH") {
		t.Errorf("service update: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "profile-service", "reorder", "--ids", "4,2"); r.code != 0 || string(f.last().Body) != `{"ids":[4,2]}` {
		t.Errorf("service reorder: code %d, body %s, stderr %q", r.code, f.last().Body, r.stderr)
	}
	if r := run(t, "", "profile-service", "delete", "4", "--yes", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "DELETE") {
		t.Errorf("service delete: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "schema", "profile-service", "create"); r.code != 0 || !strings.Contains(r.stdout, `"--kind"`) || !strings.Contains(r.stdout, `"reviewing"`) {
		t.Errorf("schema profile-service create: code %d, stdout %s", r.code, r.stdout)
	}
}

func TestConflicts(t *testing.T) {
	setup(t)
	r := run(t, "", "publications", "create", "--title", "A", "--json")
	var out struct {
		Error app.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &out); err != nil {
		t.Fatalf("stderr is not JSON: %q", r.stderr)
	}
	if r.code != app.ExitConflict || out.Error.Code != "publication_exists" || out.Error.Hint == "" || !strings.Contains(string(out.Error.Details["match"]), `"id": 12`) {
		t.Errorf("code %d, error %+v", r.code, out.Error)
	}

	r = run(t, "", "posts", "update", "9", "--title", "B", "--edit-version", "3")
	if r.code != app.ExitConflict || !strings.Contains(r.stderr, "current_edit_version: 4") {
		t.Errorf("edit conflict: code %d, stderr %q", r.code, r.stderr)
	}
}

func TestManageFlag(t *testing.T) {
	setup(t)
	if r := run(t, "", "publications", "get", "1", "--manage", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/publications/1") {
		t.Errorf("publications --manage: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := run(t, "", "publications", "get", "1", "--drafts"); r.code != app.ExitUsage {
		t.Errorf("publications --drafts should be unknown: code %d", r.code)
	}
	if r := run(t, "", "key", "show", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "/me") {
		t.Errorf("key show: code %d, stdout %q", r.code, r.stdout)
	}
}

func TestAssignmentMarkdown(t *testing.T) {
	setup(t)
	r := run(t, "", "course-assignments", "create", "3", "--title", "Essay", "--content-markdown", "Write **500** words.", "--dry-run", "--json")
	if r.code != 0 || !strings.Contains(r.stdout, `"content_markdown": "Write **500** words."`) || strings.Contains(r.stdout, `"content_structured"`) {
		t.Errorf("code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestRichTextIsMarkdownFirst(t *testing.T) {
	setup(t)
	r := run(t, "", "posts", "create", "--title", "T", "--data", `{"content_structured":{"type":"doc","content":[]}}`, "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, `"content_structured"`) {
		t.Errorf("Tiptap through --data should still work: code %d, stderr %q", r.code, r.stderr)
	}
	r = run(t, "", "posts", "create", "--title", "T", "--data", `{"content_structured":{"type":"doc"}}`, "--content-markdown", "x")
	if r.code != app.ExitUsage {
		t.Errorf("Tiptap and Markdown together: code %d", r.code)
	}
	doc := filepath.Join(t.TempDir(), "post.json")
	if err := os.WriteFile(doc, []byte(`{"type":"doc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "", "posts", "create", "--title", "T", "--content-file", doc); r.code != app.ExitUsage || !strings.Contains(r.stderr, "Markdown") {
		t.Errorf(".json content file: code %d, stderr %q", r.code, r.stderr)
	}
	if r := run(t, "", "profile", "update", "--bio-markdown", "x"); r.code != app.ExitUsage {
		t.Errorf("profile has summary and body, not bio: code %d", r.code)
	}
	if r := run(t, "", "profile", "update", "--summary-markdown", "I study *birds*.", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, `"summary_markdown": "I study *birds*."`) {
		t.Errorf("profile summary: code %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestHelpShowsChoicesAndLimits(t *testing.T) {
	r := run(t, "", "lesson-materials", "create", "--help")
	if !strings.Contains(r.stdout, "one of: reading, slides, data_set, code_file, external_link, resource, no_file, other") {
		t.Errorf("material type choices missing from help:\n%s", r.stdout)
	}
	r = run(t, "", "courses", "update", "--help")
	for _, want := range []string{
		"--instructor-name", "--primary-institution", "(up to 255 characters)", "(up to 500 characters)",
		"term (required; one of: spring, summer, fall, winter, q1, q2, q3, q4, other)",
		"Course Info",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("courses update --help lacks %q", want)
		}
	}
}

func TestCoursePartialUpdate(t *testing.T) {
	f := setup(t)
	r := run(t, "", "courses", "update", "4", "--instructor-name", "Ada Lovelace", "--null", "primary_institution", "--edit-version", "3", "--dry-run", "--json")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	var plan struct {
		Method string         `json:"method"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &plan); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"instructor_name": "Ada Lovelace", "primary_institution": nil, "edit_version": float64(3)}
	if plan.Method != "PATCH" || len(plan.Body) != len(want) {
		t.Fatalf("plan = %+v", plan)
	}
	for k, v := range want {
		if got, ok := plan.Body[k]; !ok || got != v {
			t.Errorf("body[%s] = %v, want %v", k, got, v)
		}
	}
	if len(f.requests) != 0 {
		t.Error("a dry run sent a request")
	}
}

func TestSectionOrderCommands(t *testing.T) {
	for _, section := range []string{"publications", "talks"} {
		t.Run(section, func(t *testing.T) {
			f := setup(t)
			r := run(t, "", section, "get-order", "--json")
			if r.code != 0 || f.last().Method != "GET" || f.last().Path != "/sites/7/section-order/"+section {
				t.Fatalf("get order: %+v, request %+v", r, f.last())
			}
			r = run(t, "", section, "set-order", "--order", "custom", "--reset", "--json")
			var body map[string]any
			_ = json.Unmarshal(f.last().Body, &body)
			if r.code != 0 || f.last().Method != "PUT" || body["order"] != "custom" || body["reset"] != true {
				t.Fatalf("set order: %+v, request %+v", r, f.last())
			}
			r = run(t, "", section, "update", "12", "--year-order", "2", "--dry-run", "--json")
			if r.code != 0 || !strings.Contains(r.stdout, `"year_order": 2`) {
				t.Fatalf("year position: %+v", r)
			}
		})
	}
}

func TestWriteSummaryShowsURLAndVersionWithoutMissingID(t *testing.T) {
	var out bytes.Buffer
	c := &CLI{Stdout: &out}
	rc := &registry.Command{Group: "profile", Name: "update", Kind: registry.KindWrite}
	data := json.RawMessage(`{"name":"Erik","url":"https://example.org/","edit_version":4}`)
	if err := c.printWriteSummary(rc, app.Call{}, nil, data); err != nil {
		t.Fatal(err)
	}
	want := "Updated profile \"Erik\".\nURL: https://example.org/\nEdit version: 4 (pass --edit-version 4 on your next update).\n"
	if out.String() != want {
		t.Fatalf("summary = %q, want %q", out.String(), want)
	}
}

func TestSiteTableKeepsAddressesAndTypes(t *testing.T) {
	var out bytes.Buffer
	c := &CLI{Stdout: &out}
	url := "https://a-long-site-name.example.org/courses/a-long-course-path-for-testing"
	c.printTable([]map[string]any{{"id": 7, "name": "Lab", "site_type": "lab", "url": url}})
	if !strings.Contains(out.String(), "SITE_TYPE") || !strings.Contains(out.String(), url) {
		t.Fatalf("site table: %s", out.String())
	}
}

func TestHumanErrorsOmitDebugTraceAndKeepActionableDetails(t *testing.T) {
	var out bytes.Buffer
	c := &CLI{Stderr: &out}
	err := &api.Error{Status: 500, Code: "server_error", Message: "Request failed.", Details: map[string]json.RawMessage{
		"trace":                json.RawMessage(`[{"function":"internalMethod"}]`),
		"exception":            json.RawMessage(`"InternalException"`),
		"file":                 json.RawMessage(`"/srv/app/internal.php"`),
		"line":                 json.RawMessage(`99`),
		"current_edit_version": json.RawMessage(`4`),
	}}
	c.printError(err)
	if strings.Contains(out.String(), "internalMethod") || strings.Contains(out.String(), "InternalException") || strings.Contains(out.String(), "/srv/") || !strings.Contains(out.String(), "current_edit_version: 4") {
		t.Fatalf("human error: %s", out.String())
	}
	out.Reset()
	c.json = true
	c.printError(err)
	var body struct {
		Error app.ErrorBody `json:"error"`
	}
	if json.Unmarshal(out.Bytes(), &body) != nil || len(body.Error.Details) != 5 {
		t.Fatalf("JSON error lost diagnostics: %s", out.String())
	}
}

func TestSiteLabelPrefersTheCustomDomain(t *testing.T) {
	cases := map[string]siteSummary{
		"jane.example.org (Jane Doe)":   {ID: 3, Name: "Jane Doe", URL: "https://jane.academic.page", CustomDomain: "jane.example.org"},
		"jane.academic.page (Jane Doe)": {ID: 3, Name: "Jane Doe", URL: "https://jane.academic.page"},
		"Jane Doe":                      {ID: 3, Name: "Jane Doe"},
	}
	for want, site := range cases {
		if got := site.label(); got != want {
			t.Errorf("label() = %q, want %q", got, want)
		}
	}
}

func TestEmptyTagListPointsToManage(t *testing.T) {
	rc := &registry.Command{Group: "tags", Name: "list", Kind: registry.KindRead, EmptyHint: "Add --manage to list every tag."}
	var out bytes.Buffer
	c := &CLI{Stdout: &out}
	if err := c.printResult(rc, app.Call{}, &app.Result{JSON: []byte(`{"data":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "No records.\nAdd --manage to list every tag.\n" {
		t.Errorf("output = %q", out.String())
	}

	out.Reset()
	if err := c.printResult(rc, app.Call{Drafts: true}, &app.Result{JSON: []byte(`{"data":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "No records.\n" {
		t.Errorf("with --manage, output = %q", out.String())
	}
}

func TestTextListFlagsKeepCommas(t *testing.T) {
	setup(t)
	r := run(t, "", "talks", "update", "5", "--copresenters", "Cai, C.X.", "--copresenters", "Doe, J.", "--dry-run")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, `"Cai, C.X.",`) || !strings.Contains(r.stdout, `"Doe, J."`) {
		t.Errorf("copresenters were split: %s", r.stdout)
	}
}

func TestProjectsUseBooleanPublishingAndTitledLinks(t *testing.T) {
	setup(t)
	for _, tc := range []struct {
		args      []string
		published bool
	}{
		{[]string{"projects", "create", "--title", "Research"}, false},
		{[]string{"projects", "create", "--title", "Research", "--publish"}, true},
		{[]string{"projects", "update", "9", "--unpublish"}, false},
	} {
		args := append(tc.args, "--dry-run", "--json", "--data", `{"links":[{"title":"Data","url":"https://example.org/data"}]}`)
		result := run(t, "", args...)
		if result.code != 0 {
			t.Fatalf("%v: %s", args, result.stderr)
		}
		var plan app.Plan
		if err := json.Unmarshal([]byte(result.stdout), &plan); err != nil {
			t.Fatal(err)
		}
		if plan.Body["published"] != tc.published {
			t.Fatalf("body: %v", plan.Body)
		}
		if _, exists := plan.Body["published_at"]; exists {
			t.Fatalf("retired timestamp in body: %v", plan.Body)
		}
		links, ok := plan.Body["links"].([]any)
		if !ok || len(links) != 1 || links[0].(map[string]any)["title"] != "Data" {
			t.Fatalf("links: %v", plan.Body["links"])
		}
	}
	result := run(t, "", "projects", "create", "--title", "Research", "--project-url", "https://example.org")
	if result.code == 0 {
		t.Fatal("retired project URL flag was accepted")
	}
}

func TestSitesUseTakesAnAddress(t *testing.T) {
	setup(t)
	r := run(t, "", "sites", "use", "https://lab.academic.page/")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "now uses lab.academic.page (Lab).") || strings.Contains(r.stdout, "ID") {
		t.Errorf("sites use output: %q", r.stdout)
	}

	if r := run(t, "", "sites", "use", "nobody.academic.page"); r.code != app.ExitUsage {
		t.Errorf("an unknown address: code %d, %s", r.code, r.stderr)
	}
}

func TestNavigationUpdateSendsTheWholeMenu(t *testing.T) {
	setup(t)

	menu := `{"items":[{"target":"home"},{"label":"Software","children":[{"target":"project:12"},{"url":"https://github.com/jane","label":"GitHub"}]}]}`
	r := run(t, menu, "navigation", "update", "header", "--data", "-", "--dry-run")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	for _, want := range []string{"PUT", "/sites/7/navigation/header", `"label": "Software"`, `"target": "project:12"`, `"url": "https://github.com/jane"`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("dry run lacks %s:\n%s", want, r.stdout)
		}
	}
	if r := run(t, "", "navigation", "get", "footer", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/navigation/footer") {
		t.Fatalf("navigation get: %d %s %s", r.code, r.stdout, r.stderr)
	}
}

func TestAppearanceUpdateSendsOnlyWhatChanges(t *testing.T) {
	setup(t)

	r := run(t, "", "appearance", "update", "--theme", "pragma", "--show-footer-navigation=false", "--dry-run")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	for _, want := range []string{"PATCH", "/sites/7/appearance", `"theme": "pragma"`, `"show_footer_navigation": false`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("dry run lacks %s:\n%s", want, r.stdout)
		}
	}
	for _, unwanted := range []string{"accent_color", "footer_text", "default_list_view"} {
		if strings.Contains(r.stdout, unwanted) {
			t.Errorf("dry run sends %s, which was not set:\n%s", unwanted, r.stdout)
		}
	}
	if r := run(t, "", "appearance", "get", "--dry-run"); r.code != 0 || !strings.Contains(r.stdout, "/sites/7/appearance") {
		t.Fatalf("appearance get: %d %s %s", r.code, r.stdout, r.stderr)
	}
}

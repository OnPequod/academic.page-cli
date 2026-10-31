// Package app runs registry commands against the API. The CLI and the MCP
// server both go through Execute, so they behave the same way.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/OnPequod/academic.page-cli/internal/api"
	"github.com/OnPequod/academic.page-cli/internal/config"
	"github.com/OnPequod/academic.page-cli/internal/content"
	"github.com/OnPequod/academic.page-cli/internal/registry"
	"github.com/OnPequod/academic.page-cli/internal/spec"
)

// App holds what every command needs.
type App struct {
	Registry *registry.Registry
	Settings config.Settings
	Client   *api.Client
	// Now returns the current time; tests replace it.
	Now func() time.Time
}

// New returns an App for the given settings.
func New(reg *registry.Registry, settings config.Settings, userAgent string) *App {
	return &App{
		Registry: reg,
		Settings: settings,
		Client:   api.New(settings.BaseURL, settings.Token, userAgent),
		Now:      time.Now,
	}
}

// Call is one command invocation.
type Call struct {
	Command *registry.Command
	// Site overrides the configured site when non-zero.
	Site int64
	// Arg is the positional ID.
	Arg string
	// File is the path of the file to upload.
	File string
	// Query holds query parameters by API name.
	Query map[string]string
	// Body holds body fields by API name.
	Body map[string]any

	// ContentMarkdown is Markdown for content_structured.
	ContentMarkdown string
	// ContentFile is a .md, .markdown or .json file for content_structured;
	// "-" reads Markdown from Stdin.
	ContentFile string
	Stdin       io.Reader

	Publish bool
	Drafts  bool
	All     bool
	DryRun  bool
}

// Result is what a command returns.
type Result struct {
	// JSON is the API's response, or a summary for empty responses.
	JSON json.RawMessage
	// Text is set for non-JSON responses such as BibTeX.
	Text string
	// Plan is set for dry runs.
	Plan *Plan
}

// Plan describes the request a dry run would have sent.
type Plan struct {
	DryRun bool           `json:"dry_run"`
	Method string         `json:"method"`
	URL    string         `json:"url"`
	Body   map[string]any `json:"body,omitempty"`
	File   *FileInfo      `json:"file,omitempty"`
	// Multipart is true for uploads: the file goes up in parts through the
	// multipart endpoints, and the request above is only the fallback.
	Multipart bool `json:"multipart,omitempty"`
	All       bool `json:"follow_pages,omitempty"`
}

// FileInfo describes an upload.
type FileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size_bytes"`
}

// Execute runs a call.
func (a *App) Execute(ctx context.Context, call Call) (*Result, error) {
	cmd := call.Command
	op := cmd.Op
	if call.Drafts {
		if cmd.DraftsOp == nil {
			return nil, Usagef("`%s` has no --drafts option; only the published version can be read", cmd.Path())
		}
		op = cmd.DraftsOp
	}

	query, err := a.query(call, op)
	if err != nil {
		return nil, err
	}
	path, err := a.path(call, op)
	if err != nil {
		return nil, err
	}
	req := api.Request{Method: op.Method, Path: path, Query: query}
	if cmd.Text {
		req.Accept = "application/x-bibtex, text/plain;q=0.9, */*;q=0.1"
	}
	if call.All && !cmd.Paginated {
		return nil, Usagef("`%s` returns one record; --all only applies to list commands", cmd.Path())
	}

	plan := &Plan{DryRun: true, Method: op.Method, All: call.All}
	var upload *FileInfo
	var uploadFields map[string]any
	switch cmd.Kind {
	case registry.KindWrite:
		body, err := a.body(call)
		if err != nil {
			return nil, err
		}
		plan.Body = body
		if req.Body, err = json.Marshal(body); err != nil {
			return nil, err
		}
		req.ContentType = "application/json"
	case registry.KindUpload:
		body, err := a.body(call)
		if err != nil {
			return nil, err
		}
		info, err := fileInfo(call.File)
		if err != nil {
			return nil, err
		}
		plan.Body, plan.File = body, info
		if info.Size > MaxUploadBytes {
			return nil, Usagef("%s is %s; the API accepts files up to 1 GB", info.Path, humanBytes(info.Size))
		}
		plan.Multipart = true
		upload, uploadFields = info, body
	default:
		if len(call.Body) > 0 || call.ContentFile != "" || call.ContentMarkdown != "" || call.Publish {
			return nil, Usagef("`%s` only reads; it takes no body fields", cmd.Path())
		}
	}

	if call.DryRun {
		plan.URL = a.Client.URL(req)
		return &Result{Plan: plan}, nil
	}
	if a.Settings.Token == "" {
		return nil, &api.Error{Status: http.StatusUnauthorized, Code: "missing_api_token", Message: "No API key is configured."}
	}

	if upload != nil {
		// Multipart sends the file straight to storage, which avoids the
		// 100 MB per-request cap of the CDN in front of the API. A single
		// request is only the fallback for storage without multipart.
		site, err := a.site(call)
		if err != nil {
			return nil, err
		}
		data, err := a.multipartUpload(ctx, site, upload, uploadFields)
		if err == nil {
			return &Result{JSON: data}, nil
		}
		if !errors.Is(err, errMultipartUnsupported) {
			return nil, err
		}
		if req.Body, req.ContentType, err = multipartBody(cmd.FileArg.Name, upload, uploadFields); err != nil {
			return nil, err
		}
		resp, err := a.Client.Do(ctx, req)
		if err != nil {
			var apiErr *api.Error
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusRequestEntityTooLarge {
				apiErr.Code = "file_too_large"
				apiErr.Message = fmt.Sprintf("%s (%s) is too large to upload in one request, and this site's storage does not accept multipart uploads.", upload.Name, humanBytes(upload.Size))
			}
			return nil, err
		}
		return &Result{JSON: resp.Body}, nil
	}

	if call.All {
		data, err := a.Client.All(ctx, req)
		if err != nil {
			return nil, err
		}
		return &Result{JSON: data}, nil
	}
	resp, err := a.Client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if cmd.Text {
		return &Result{Text: string(resp.Body)}, nil
	}
	if len(bytes.TrimSpace(resp.Body)) == 0 {
		summary := map[string]any{"ok": true, "status": resp.Status}
		if cmd.Kind == registry.KindDelete {
			summary = map[string]any{"deleted": true, "id": call.Arg}
		}
		data, _ := json.Marshal(summary)
		return &Result{JSON: data}, nil
	}
	if !json.Valid(resp.Body) {
		return nil, fmt.Errorf("the API returned a non-JSON response (status %d); check the base URL %s", resp.Status, a.Settings.BaseURL)
	}
	return &Result{JSON: resp.Body}, nil
}

func (a *App) site(call Call) (int64, error) {
	if call.Site != 0 {
		return call.Site, nil
	}
	if a.Settings.Site != 0 {
		return a.Settings.Site, nil
	}
	return 0, Usagef("no site chosen: pass --site <id>, set %s, or run `academic-page sites use <id>` (`academic-page sites list` shows the IDs)", config.EnvSite)
}

func (a *App) path(call Call, op *spec.Operation) (string, error) {
	path := op.Path
	for _, p := range op.Params {
		if p.In != "path" {
			continue
		}
		var value string
		if p.Name == "site" {
			site, err := a.site(call)
			if err != nil {
				return "", err
			}
			value = fmt.Sprint(site)
		} else {
			if call.Arg == "" {
				return "", Usagef("`%s` needs the %s: %s", call.Command.Path(), p.Name, p.Description)
			}
			if p.Schema.PrimaryType() == "integer" && !isPositiveInt(call.Arg) {
				return "", Usagef("%s must be a numeric ID, got %q", p.Name, call.Arg)
			}
			value = call.Arg
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(value))
	}
	return path, nil
}

func (a *App) query(call Call, op *spec.Operation) (url.Values, error) {
	q := url.Values{}
	accepted := map[string]bool{}
	for _, p := range op.Params {
		if p.In == "query" {
			accepted[p.Name] = true
		}
	}
	for _, name := range sortedKeys(call.Query) {
		value := call.Query[name]
		if !accepted[name] {
			for _, f := range call.Command.Query {
				if f.Name == name && f.DraftsOnly {
					return nil, Usagef("--%s only works with --drafts", f.Flag)
				}
				if f.Name == name && f.PublishedOnly {
					return nil, Usagef("--%s does not work with --drafts", f.Flag)
				}
			}
			return nil, Usagef("`%s` has no %q option", call.Command.Path(), name)
		}
		q.Set(name, value)
	}
	return q, nil
}

// body builds a write body: fields, then content, then publishing.
func (a *App) body(call Call) (map[string]any, error) {
	cmd := call.Command
	known := map[string]bool{}
	for _, f := range cmd.Body {
		known[f.Name] = true
	}
	if cmd.Content {
		known["content_structured"] = true
	}
	if cmd.Publishable {
		known["published_at"] = true
	}
	body := map[string]any{}
	for _, name := range sortedKeys(call.Body) {
		if !known[name] {
			return nil, Usagef("`%s` has no body field %q; it accepts: %s", cmd.Path(), name, strings.Join(sortedKeys(known), ", "))
		}
		body[name] = call.Body[name]
	}

	if call.ContentFile != "" || call.ContentMarkdown != "" {
		if !cmd.Content {
			return nil, Usagef("`%s` has no content", cmd.Path())
		}
		if call.ContentFile != "" && call.ContentMarkdown != "" {
			return nil, Usagef("give --content-file or --content-markdown, not both")
		}
		doc, err := contentDoc(call)
		if err != nil {
			return nil, err
		}
		body["content_structured"] = doc
	}

	if call.Publish {
		if !cmd.Publishable {
			return nil, Usagef("`%s` cannot be published", cmd.Path())
		}
		if _, ok := body["published_at"]; ok {
			return nil, Usagef("give --publish or --published-at, not both")
		}
		body["published_at"] = a.Now().UTC().Format(time.RFC3339)
	} else if cmd.Publishable && cmd.Arg == nil {
		// Creates are drafts unless asked otherwise.
		if _, ok := body["published_at"]; !ok {
			body["published_at"] = nil
		}
	}
	return body, nil
}

func contentDoc(call Call) (any, error) {
	if call.ContentMarkdown != "" {
		return content.FromMarkdown([]byte(call.ContentMarkdown)), nil
	}
	var data []byte
	var err error
	if call.ContentFile == "-" {
		if call.Stdin == nil {
			return nil, Usagef("--content-file - reads standard input, which is not available here")
		}
		data, err = io.ReadAll(call.Stdin)
	} else {
		data, err = os.ReadFile(call.ContentFile)
	}
	if err != nil {
		return nil, Usagef("read content file: %v", err)
	}
	switch strings.ToLower(filepath.Ext(call.ContentFile)) {
	case ".json":
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, Usagef("%s is not valid JSON: %v", call.ContentFile, err)
		}
		if doc["type"] != "doc" {
			return nil, Usagef(`%s must be a Tiptap document: {"type": "doc", "content": [...]}`, call.ContentFile)
		}
		return doc, nil
	case ".md", ".markdown", ".txt", "":
		return content.FromMarkdown(data), nil
	}
	return nil, Usagef("--content-file takes Markdown (.md) or Tiptap JSON (.json), got %s", call.ContentFile)
}

func fileInfo(path string) (*FileInfo, error) {
	if path == "" {
		return nil, Usagef("give the file to upload")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, Usagef("cannot read %s: %v", path, err)
	}
	if st.IsDir() {
		return nil, Usagef("%s is a directory; give a file", path)
	}
	return &FileInfo{Path: path, Name: filepath.Base(path), Size: st.Size()}, nil
}

func multipartBody(field string, info *FileInfo, fields map[string]any) ([]byte, string, error) {
	f, err := os.Open(info.Path)
	if err != nil {
		return nil, "", Usagef("cannot read %s: %v", info.Path, err)
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, name := range sortedKeys(fields) {
		if fields[name] == nil {
			continue
		}
		if err := w.WriteField(name, fmt.Sprint(fields[name])); err != nil {
			return nil, "", err
		}
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, info.Name))
	h.Set("Content-Type", contentTypeOf(info.Path))
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func contentTypeOf(path string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); t != "" {
		return t
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	return http.DetectContentType(head[:n])
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

func isPositiveInt(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

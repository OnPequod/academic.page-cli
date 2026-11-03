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
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OnPequod/academic.page-cli/internal/config"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

type storage struct {
	*httptest.Server
	mu        sync.Mutex
	calls     []string
	partSizes []int
	complete  map[string]any
	// supported=false answers init with multipart_upload_not_supported.
	supported bool
	// initMessage overrides the message of that refusal.
	initMessage string
	// singleLimit makes the single upload answer 413 above this many bytes,
	// as nginx does.
	singleLimit int
	failPart    int
	// initCode makes init fail with this 422 code.
	initCode string
	// singleFailures makes the first single uploads answer 503 (after
	// storing the file, as a lost response would), and completeFailures the
	// first completes.
	singleFailures, completeFailures int
	// dropSingle closes the connection on every single upload, as a
	// network failure does.
	dropSingle bool
	// keys records the Idempotency-Key of each single upload; stored maps
	// a key to the record it created.
	keys   []string
	stored map[string]string
	single map[string]any
}

func newStorage(t *testing.T, supported bool) *storage {
	s := &storage{supported: supported}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/multipart/init"):
			if s.initCode != "" {
				w.WriteHeader(422)
				fmt.Fprintf(w, `{"message":"Refused.","code":%q}`, s.initCode)
				return
			}
			if !s.supported {
				msg := s.initMessage
				if msg == "" {
					msg = "Multipart uploads are not available for this storage."
				}
				w.WriteHeader(422)
				fmt.Fprintf(w, `{"message":%q,"code":"multipart_upload_not_supported"}`, msg)
				return
			}
			fmt.Fprint(w, `{"upload_id":"u1","object_key":"k","part_size_hint":10485760,"expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/multipart/u1/sign"):
			var in struct {
				PartNumber int `json:"part_number"`
			}
			_ = json.Unmarshal(body, &in)
			fmt.Fprintf(w, `{"upload_id":"u1","part_number":%d,"url":"%s/bucket/part/%d?sig=x","method":"PUT"}`, in.PartNumber, s.URL, in.PartNumber)
		case strings.HasPrefix(r.URL.Path, "/bucket/part/"):
			if r.Header.Get("Authorization") != "" {
				t.Error("the API key was sent to storage")
			}
			var n int
			_, _ = fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/bucket/part/"), &n)
			if n == s.failPart {
				w.WriteHeader(403)
				return
			}
			s.partSizes = append(s.partSizes, len(body))
			w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, n))
		case strings.HasSuffix(r.URL.Path, "/multipart/u1/complete"):
			_ = json.Unmarshal(body, &s.complete)
			if s.completeFailures > 0 {
				s.completeFailures--
				w.WriteHeader(502)
				return
			}
			w.WriteHeader(201)
			fmt.Fprint(w, `{"data":{"id":5,"filename":"big.bin"}}`)
		case strings.HasSuffix(r.URL.Path, "/multipart/u1/abort"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/media"):
			key := r.Header.Get("Idempotency-Key")
			s.keys = append(s.keys, key)
			if s.dropSingle {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			if s.stored == nil {
				s.stored = map[string]string{}
			}
			if rec, ok := s.stored[key]; ok && key != "" {
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(201)
				fmt.Fprint(w, rec)
				return
			}
			if s.singleLimit > 0 && len(body) > s.singleLimit {
				w.WriteHeader(413)
				fmt.Fprint(w, "<html><body>413 Request Entity Too Large</body></html>")
				return
			}
			rec := fmt.Sprintf(`{"data":{"id":%d,"size":%d}}`, 6+len(s.stored), len(body))
			s.stored[key] = rec
			if mr, err := multipartReader(r, body); err == nil {
				s.single = mr
			}
			if s.singleFailures > 0 {
				s.singleFailures--
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
			fmt.Fprint(w, rec)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func bigFile(t *testing.T, size int64) string {
	path := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func uploadCall(t *testing.T, s *storage, path string) (*Result, error) {
	return uploadWith(t, s, path, nil)
}

func uploadWith(t *testing.T, s *storage, path string, headers map[string]string) (*Result, error) {
	reg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := New(reg, config.Settings{BaseURL: s.URL, Token: "k", Site: 7}, "test")
	a.Client.Sleep = func(context.Context, time.Duration) error { return nil }
	return a.Execute(context.Background(), Call{
		Command: reg.Find("media", "upload"),
		File:    path,
		Body:    map[string]any{"alt_text": "Alt", "description": "Desc"},
		Headers: headers,
	})
}

// multipartReader returns the text fields of a multipart/form-data body.
func multipartReader(r *http.Request, body []byte) (map[string]any, error) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	out := map[string]any{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if part.FileName() == "" {
			data, _ := io.ReadAll(part)
			out[part.FormName()] = string(data)
		}
	}
	return out, nil
}

func TestMultipartUpload(t *testing.T) {
	s := newStorage(t, true)
	size := int64(21 << 20)
	res, err := uploadCall(t, s, bigFile(t, size))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.JSON), `"id":5`) {
		t.Errorf("result = %s", res.JSON)
	}
	var total int
	for _, n := range s.partSizes {
		total += n
	}
	if len(s.partSizes) != 3 || int64(total) != size {
		t.Errorf("parts = %v (total %d)", s.partSizes, total)
	}
	parts, _ := s.complete["parts"].([]any)
	if len(parts) != 3 || s.complete["alt_text"] != "Alt" {
		t.Errorf("complete body = %v", s.complete)
	}
	if first, _ := parts[0].(map[string]any); first["etag"] != `"etag-1"` {
		t.Errorf("first part = %v", parts[0])
	}
}

func TestMultipartFallsBackToSingleUpload(t *testing.T) {
	s := newStorage(t, false)
	res, err := uploadCall(t, s, bigFile(t, 1024))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.JSON), `"id":6`) {
		t.Errorf("result = %s", res.JSON)
	}
}

func TestLimitRefusalDoesNotFallBack(t *testing.T) {
	s := newStorage(t, false)
	s.initMessage = "This file is larger than your plan allows. Your current limit is 1 GB per file."
	_, err := uploadCall(t, s, bigFile(t, 1024))
	if ExitCode(err) != ExitValidation || !strings.Contains(err.Error(), "larger than your plan") {
		t.Errorf("err = %v", err)
	}
	if len(s.calls) != 1 {
		t.Errorf("calls = %v", s.calls)
	}
}

func TestLimitCodesDoNotFallBack(t *testing.T) {
	for _, code := range []string{"upload_too_large", "storage_quota_exceeded", "bucket_quota_exceeded"} {
		s := newStorage(t, true)
		s.initCode = code
		_, err := uploadCall(t, s, bigFile(t, 1024))
		if Describe(err).Code != code || Describe(err).Hint == "" || len(s.calls) != 1 {
			t.Errorf("%s: err = %+v, calls = %v", code, Describe(err), s.calls)
		}
	}
}

func TestSingleUploadTooLarge(t *testing.T) {
	s := newStorage(t, false)
	s.singleLimit = 1 << 20
	_, err := uploadCall(t, s, bigFile(t, 2<<20))
	if ExitCode(err) != ExitValidation || Describe(err).Code != "file_too_large" {
		t.Errorf("err = %v (%+v)", err, Describe(err))
	}
}

func TestRejectsFilesOverOneGigabyte(t *testing.T) {
	s := newStorage(t, true)
	_, err := uploadCall(t, s, bigFile(t, MaxUploadBytes+1))
	if ExitCode(err) != ExitUsage || len(s.calls) != 0 {
		t.Errorf("err = %v, calls = %v", err, s.calls)
	}
}

func TestMultipartAbortsOnFailure(t *testing.T) {
	s := newStorage(t, true)
	s.failPart = 2
	if _, err := uploadCall(t, s, bigFile(t, 21<<20)); err == nil {
		t.Fatal("expected an error")
	}
	if last := s.calls[len(s.calls)-1]; last != "POST /sites/7/media/multipart/u1/abort" {
		t.Errorf("last call = %s", last)
	}
}

func TestSmallFilesUseOnePart(t *testing.T) {
	s := newStorage(t, true)
	if _, err := uploadCall(t, s, bigFile(t, 1024)); err != nil {
		t.Fatal(err)
	}
	want := "POST /sites/7/media/multipart/init,POST /sites/7/media/multipart/u1/sign,PUT /bucket/part/1,POST /sites/7/media/multipart/u1/complete"
	if got := strings.Join(s.calls, ","); got != want {
		t.Errorf("calls = %s", got)
	}
}

func TestSingleUploadSendsAnIdempotencyKey(t *testing.T) {
	s := newStorage(t, false)
	if _, err := uploadCall(t, s, bigFile(t, 1024)); err != nil {
		t.Fatal(err)
	}
	if len(s.keys) != 1 || len(s.keys[0]) != 36 {
		t.Fatalf("keys = %q", s.keys)
	}
	if s.single["alt_text"] != "Alt" || s.single["description"] != "Desc" {
		t.Errorf("fields = %v", s.single)
	}
	if _, err := uploadCall(t, s, bigFile(t, 1024)); err != nil {
		t.Fatal(err)
	}
	if len(s.keys) != 2 || s.keys[0] == s.keys[1] {
		t.Errorf("each upload should get its own key: %q", s.keys)
	}
}

// A server error after the file was stored is retried with the same key,
// and the API replays the stored record instead of storing a duplicate.
func TestSingleUploadRetriesWithTheSameKey(t *testing.T) {
	s := newStorage(t, false)
	s.singleFailures = 1
	res, err := uploadCall(t, s, bigFile(t, 1024))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.keys) != 2 || s.keys[0] != s.keys[1] || len(s.stored) != 1 {
		t.Errorf("keys = %q, stored = %v", s.keys, s.stored)
	}
	if !strings.Contains(string(res.JSON), `"id":6`) {
		t.Errorf("result = %s", res.JSON)
	}
}

func TestSingleUploadUsesTheGivenKey(t *testing.T) {
	s := newStorage(t, false)
	if _, err := uploadWith(t, s, bigFile(t, 1024), map[string]string{"Idempotency-Key": "cli-audit-1"}); err != nil {
		t.Fatal(err)
	}
	res, err := uploadWith(t, s, bigFile(t, 1024), map[string]string{"Idempotency-Key": "cli-audit-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.keys, ",") != "cli-audit-1,cli-audit-1" || len(s.stored) != 1 || !strings.Contains(string(res.JSON), `"id":6`) {
		t.Errorf("keys = %q, stored = %v, result = %s", s.keys, s.stored, res.JSON)
	}
	if _, err := uploadWith(t, s, bigFile(t, 1024), map[string]string{"Idempotency-Key": strings.Repeat("k", 256)}); ExitCode(err) != ExitUsage {
		t.Errorf("a 256-character key: err = %v", err)
	}
}

// When every attempt loses its response, the error names the key, so a
// later retry can still get the stored record.
func TestLostUploadNamesTheKey(t *testing.T) {
	s := newStorage(t, false)
	s.dropSingle = true
	_, err := uploadCall(t, s, bigFile(t, 1024))
	s.mu.Lock()
	defer s.mu.Unlock()
	var retry *RetryableUploadError
	if !errors.As(err, &retry) || retry.Key != s.keys[0] || ExitCode(err) != ExitNetwork {
		t.Fatalf("err = %v", err)
	}
	// Four attempts, or more: net/http also replays a request with an
	// Idempotency-Key once when a reused connection drops.
	if len(s.keys) < 4 {
		t.Errorf("attempts = %d, want at least 4", len(s.keys))
	}
	for _, k := range s.keys {
		if k != retry.Key {
			t.Errorf("retries used another key: %q", s.keys)
		}
	}
	d := Describe(err)
	if string(d.Details["idempotency_key"]) != `"`+retry.Key+`"` || !strings.Contains(d.Hint, "--idempotency-key "+retry.Key) {
		t.Errorf("describe = %+v", d)
	}
}

func TestMultipartCompleteIsRetried(t *testing.T) {
	s := newStorage(t, true)
	s.completeFailures = 1
	if _, err := uploadCall(t, s, bigFile(t, 1024)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.Join(s.calls, ","), "/complete"); got != 2 {
		t.Errorf("completes = %d", got)
	}
}

package app

import (
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
			w.WriteHeader(201)
			fmt.Fprint(w, `{"data":{"id":5,"filename":"big.bin"}}`)
		case strings.HasSuffix(r.URL.Path, "/multipart/u1/abort"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/media"):
			if s.singleLimit > 0 && len(body) > s.singleLimit {
				w.WriteHeader(413)
				fmt.Fprint(w, "<html><body>413 Request Entity Too Large</body></html>")
				return
			}
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"data":{"id":6,"size":%d}}`, len(body))
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
	reg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := New(reg, config.Settings{BaseURL: s.URL, Token: "k", Site: 7}, "test")
	a.Client.Sleep = func(context.Context, time.Duration) error { return nil }
	return a.Execute(context.Background(), Call{
		Command: reg.Find("media", "upload"),
		File:    path,
		Body:    map[string]any{"alt_text": "Alt"},
	})
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

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClient(url string) *Client {
	c := New(url, "secret", "test")
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		fmt.Fprint(w, `{"data":{}}`)
	}))
	defer srv.Close()
	if _, err := newClient(srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/x"}); err != nil {
		t.Fatal(err)
	}
}

func TestErrorParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		fmt.Fprint(w, `{"message":"The title field is required.","errors":{"title":["The title field is required."]}}`)
	}))
	defer srv.Close()
	_, err := newClient(srv.URL).Do(context.Background(), Request{Method: "POST", Path: "/x"})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Status != 422 || apiErr.Code != "validation_failed" || apiErr.Errors["title"] == nil {
		t.Errorf("err = %+v", apiErr)
	}
	if apiErr.Hint() == "" {
		t.Error("422 should have a hint")
	}
}

func TestRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"data":{}}`)
	}))
	defer srv.Close()
	if _, err := newClient(srv.URL).Do(context.Background(), Request{Method: "POST", Path: "/x"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestDoesNotRetryWriteServerErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()
	_, err := newClient(srv.URL).Do(context.Background(), Request{Method: "POST", Path: "/x"})
	if err == nil || calls.Load() != 1 {
		t.Errorf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestAllFollowsNext(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"data":[{"id":3}],"links":{"next":null}}`)
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":1},{"id":2}],"links":{"next":"%s/items?page=2&per_page=100"}}`, srv.URL)
	}))
	defer srv.Close()
	raw, err := newClient(srv.URL).All(context.Background(), Request{Method: "GET", Path: "/items"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != `{"data":[{"id":1},{"id":2},{"id":3}],"meta":{"total":3}}` {
		t.Errorf("all = %s", got)
	}
}

func TestAllRefusesOtherHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[],"links":{"next":"https://evil.example/items?page=2"}}`)
	}))
	defer srv.Close()
	_, err := newClient(srv.URL).All(context.Background(), Request{Method: "GET", Path: "/items"})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("err = %v", err)
	}
}

func TestMissingToken(t *testing.T) {
	c := New("http://unused", "", "test")
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "missing_api_token" {
		t.Errorf("err = %v", err)
	}
}

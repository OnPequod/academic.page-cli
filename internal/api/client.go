// Package api is a small HTTP client for the Academic.page API v1.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client calls the API with a bearer key.
type Client struct {
	BaseURL    string
	Token      string
	UserAgent  string
	HTTP       *http.Client
	MaxRetries int
	// Sleep waits between retries; tests replace it.
	Sleep func(context.Context, time.Duration) error
}

// New returns a client with sensible defaults.
func New(baseURL, token, userAgent string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		UserAgent:  userAgent,
		HTTP:       &http.Client{Timeout: 2 * time.Minute},
		MaxRetries: 3,
		Sleep:      sleep,
	}
}

// Request describes one call. Path is relative to the base URL.
type Request struct {
	Method      string
	Path        string
	Query       url.Values
	Body        []byte
	ContentType string
	// Accept overrides application/json, e.g. for BibTeX.
	Accept string
}

// Response is a completed call.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// URL returns the full URL for a request.
func (c *Client) URL(r Request) string {
	u := c.BaseURL + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	return u
}

// Do sends a request, retrying rate limits and, for reads, server errors.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	return c.do(ctx, r.Method, c.URL(r), r)
}

func (c *Client) do(ctx context.Context, method, rawURL string, r Request) (*Response, error) {
	if c.Token == "" {
		return nil, &Error{Status: http.StatusUnauthorized, Code: "missing_api_token", Message: "No API key is configured."}
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.once(ctx, method, rawURL, r)
		retryable, wait := c.shouldRetry(method, resp, err, attempt)
		if !retryable {
			if err != nil {
				return nil, err
			}
			if resp.Status >= 400 {
				return resp, parseError(resp)
			}
			return resp, nil
		}
		if err := c.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c *Client) shouldRetry(method string, resp *Response, err error, attempt int) (bool, time.Duration) {
	if attempt >= c.MaxRetries {
		return false, 0
	}
	backoff := time.Duration(1<<attempt) * time.Second
	if err != nil {
		var netErr *NetworkError
		return method == http.MethodGet && errors.As(err, &netErr), backoff
	}
	switch {
	case resp.Status == http.StatusTooManyRequests:
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			return true, time.Duration(s) * time.Second
		}
		return true, backoff
	case resp.Status >= 500 && method == http.MethodGet:
		return true, backoff
	}
	return false, 0
}

func (c *Client) once(ctx context.Context, method, rawURL string, r Request) (*Response, error) {
	var body io.Reader
	if r.Body != nil {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	accept := r.Accept
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if r.ContentType != "" {
		req.Header.Set("Content-Type", r.ContentType)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &NetworkError{Err: err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, &NetworkError{Err: err}
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: data}, nil
}

// All follows links.next from a list request and returns every record as
// {"data": [...], "meta": {"total": n}}.
func (c *Client) All(ctx context.Context, r Request) (json.RawMessage, error) {
	if r.Query == nil {
		r.Query = url.Values{}
	}
	if r.Query.Get("per_page") == "" {
		r.Query.Set("per_page", "100")
	}
	next := c.URL(r)
	var all []json.RawMessage
	for pages := 0; next != ""; pages++ {
		if pages >= 1000 {
			return nil, errors.New("stopped after 1000 pages; the API kept returning a next link")
		}
		if err := c.sameOrigin(next); err != nil {
			return nil, err
		}
		resp, err := c.do(ctx, r.Method, next, r)
		if err != nil {
			return nil, err
		}
		var page struct {
			Data  []json.RawMessage `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, fmt.Errorf("parse list response: %w", err)
		}
		all = append(all, page.Data...)
		next = ""
		if page.Links.Next != nil {
			next = *page.Links.Next
		}
	}
	if all == nil {
		all = []json.RawMessage{}
	}
	return json.Marshal(map[string]any{"data": all, "meta": map[string]int{"total": len(all)}})
}

// sameOrigin refuses to send the key to a host other than the base URL's.
func (c *Client) sameOrigin(raw string) error {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad next link %q: %w", raw, err)
	}
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return fmt.Errorf("refusing to follow next link to %s://%s; the API key is only sent to %s://%s", u.Scheme, u.Host, base.Scheme, base.Host)
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// PutPart uploads one part of a multipart upload to a presigned storage URL
// and returns the part's ETag. The URL carries its own signature, so the API
// key is not sent.
func (c *Client) PutPart(ctx context.Context, method, rawURL string, data []byte) (string, error) {
	if method == "" {
		method = http.MethodPut
	}
	for attempt := 0; ; attempt++ {
		etag, retry, err := c.putPartOnce(ctx, method, rawURL, data)
		if err == nil {
			return etag, nil
		}
		if !retry || attempt >= c.MaxRetries {
			return "", err
		}
		if err := c.Sleep(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
			return "", err
		}
	}
}

func (c *Client) putPartOnce(ctx context.Context, method, rawURL string, data []byte) (etag string, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(data))
	if err != nil {
		return "", false, err
	}
	req.ContentLength = int64(len(data))
	res, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", true, &NetworkError{Err: err}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 300 {
		return "", res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests,
			fmt.Errorf("storage returned %d %s", res.StatusCode, http.StatusText(res.StatusCode))
	}
	etag = res.Header.Get("ETag")
	if etag == "" {
		return "", false, errors.New("storage accepted the part but returned no ETag header")
	}
	return etag, false, nil
}

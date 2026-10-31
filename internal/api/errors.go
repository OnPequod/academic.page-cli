package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Error is an error response from the API.
type Error struct {
	Status  int                 `json:"status"`
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors,omitempty"`
	// Details holds the other fields of the error body, such as the match
	// of a duplicate publication or current_edit_version.
	Details map[string]json.RawMessage `json:"details,omitempty"`
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	fields := make([]string, 0, len(e.Errors))
	for f := range e.Errors {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, f := range fields {
		fmt.Fprintf(&b, "\n  %s: %s", f, strings.Join(e.Errors[f], " "))
	}
	keys := make([]string, 0, len(e.Details))
	for k := range e.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "\n  %s: %s", k, e.Details[k])
	}
	return b.String()
}

// Hint says how to fix the error, or returns "".
func (e *Error) Hint() string {
	switch e.Code {
	case "missing_api_token":
		return "Create a key at https://academic.page/admin/account/api, then run `academic-page auth login` or set ACADEMIC_PAGE_TOKEN."
	case "api_access_requires_pro":
		return "API access needs a Pro plan on the account that owns the key."
	case "missing_ability":
		return "This key lacks the ability this call needs. Writes and --drafts need a management key with access:write (or access:read for reads); create one at https://academic.page/admin/account/api."
	case "api_scope_mismatch":
		return "This key is not scoped to that site. Check --site, or create a key that covers it."
	case "site_not_found":
		return "No site with that ID is visible to this key. Run `academic-page sites list` to see the IDs."
	case "site_disabled":
		return "The site is disabled. Re-enable it in the Academic.page admin."
	case "upload_too_large":
		return "The file is larger than the site's plan allows per file (1 GB on Pro)."
	case "storage_quota_exceeded":
		return "The site's storage is full. Delete unused media files, or move to a plan with more storage."
	case "bucket_quota_exceeded":
		return "The storage bucket is full. This is a server-side limit; contact Academic.page support."
	case "edit_conflict":
		return "The record was edited after you read it. Fetch it again, apply your change to the new version, and pass its edit_version."
	case "publication_exists":
		return "The publication is already on the site (see match). Update that record instead, or pass --on-match suggest to propose changes to it."
	case "possible_duplicate":
		return "This looks like a publication already on the site (see match). Check it; pass --on-match create_anyway only if it is a different work."
	case "preprint_of_existing":
		return "This matches a preprint or published version already on the site (see match). Pass --on-match suggest to propose linking them, or create_anyway to keep both."
	case "ignored_before":
		return "This publication was ignored on an earlier import. Pass --on-match create_anyway to add it anyway."
	case "tag_in_use":
		return "The tag is still attached to records. Remove it from them first."
	case "section_not_enabled":
		return "That section is turned off for this site. Turn it on in the site's settings."
	}
	switch {
	case e.Status == http.StatusUnauthorized:
		return "The key was rejected. It may be revoked or mistyped; run `academic-page auth login` with a new key."
	case e.Status == http.StatusNotFound:
		return "Check the ID. List commands show the IDs that exist."
	case e.Status == http.StatusConflict:
		return "The request conflicts with the record's current state. Fetch it again and retry."
	case e.Status == http.StatusUnprocessableEntity && len(e.Errors) > 0:
		return "Fix the fields listed above and try again."
	case e.Status == http.StatusRequestEntityTooLarge:
		return "The file is larger than the server accepts in one request. Ask the site's administrator to enable multipart uploads, or upload a smaller file."
	case e.Status == http.StatusTooManyRequests:
		return "Rate limit reached (600/min for content reads, 120/min for management). Wait a minute and retry."
	case e.Status >= 500:
		return "The API had a server error. Retry later; if it persists, report it with the time of the request."
	}
	return ""
}

func parseError(r *Response) *Error {
	e := &Error{Status: r.Status}
	var body struct {
		Message string              `json:"message"`
		Code    string              `json:"code"`
		Errors  map[string][]string `json:"errors"`
	}
	if json.Unmarshal(r.Body, &body) == nil {
		e.Message, e.Code, e.Errors = body.Message, body.Code, body.Errors
		var all map[string]json.RawMessage
		if json.Unmarshal(r.Body, &all) == nil {
			for k, v := range all {
				switch k {
				case "message", "code", "errors":
				default:
					if e.Details == nil {
						e.Details = map[string]json.RawMessage{}
					}
					e.Details[k] = v
				}
			}
		}
	}
	if e.Code == "" {
		switch r.Status {
		case http.StatusUnprocessableEntity:
			e.Code = "validation_failed"
		case http.StatusTooManyRequests:
			e.Code = "rate_limited"
		case http.StatusNotFound:
			e.Code = "not_found"
		case http.StatusConflict:
			e.Code = "conflict"
		case http.StatusRequestEntityTooLarge:
			e.Code = "payload_too_large"
		}
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("The API returned %d %s.", r.Status, http.StatusText(r.Status))
	}
	return e
}

// NetworkError means the request did not get a response.
type NetworkError struct{ Err error }

func (e *NetworkError) Error() string { return "could not reach the API: " + e.Err.Error() }
func (e *NetworkError) Unwrap() error { return e.Err }

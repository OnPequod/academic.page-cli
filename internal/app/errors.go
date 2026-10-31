package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/OnPequod/academic.page-cli/internal/api"
)

// Exit codes. They are part of the CLI's interface: scripts and agents may
// rely on them, so do not renumber.
const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsage      = 2
	ExitAuth       = 3
	ExitForbidden  = 4
	ExitNotFound   = 5
	ExitValidation = 6
	ExitRateLimit  = 7
	ExitServer     = 8
	ExitNetwork    = 9
	ExitConflict   = 10
)

// ExitCodes describes each exit code, for the schema command.
var ExitCodes = []struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Meaning string `json:"meaning"`
}{
	{ExitOK, "ok", "Success."},
	{ExitError, "error", "Any other failure."},
	{ExitUsage, "usage", "Bad arguments or flags, or missing site; nothing was sent."},
	{ExitAuth, "auth", "No API key, or the key was rejected (401)."},
	{ExitForbidden, "forbidden", "The key may not do this: missing ability, wrong scope, no Pro plan, or site disabled (403)."},
	{ExitNotFound, "not_found", "The site, section or record does not exist (404)."},
	{ExitValidation, "validation", "The API rejected the request (422, or 413 for a file that is too large); the error says why."},
	{ExitRateLimit, "rate_limited", "Too many requests (429), even after retrying."},
	{ExitServer, "server", "The API had a server error (5xx)."},
	{ExitNetwork, "network", "The API could not be reached."},
	{ExitConflict, "conflict", "The record changed since it was read (409). Read it again, then retry with the new edit_version."},
}

// UsageError is a mistake in how a command was called.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

// Usagef returns a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

// ExitCode maps an error to an exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var usage *UsageError
	var apiErr *api.Error
	var netErr *api.NetworkError
	switch {
	case errors.As(err, &usage):
		return ExitUsage
	case errors.As(err, &apiErr):
		switch {
		case apiErr.Status == http.StatusUnauthorized:
			return ExitAuth
		case apiErr.Status == http.StatusForbidden:
			return ExitForbidden
		case apiErr.Status == http.StatusNotFound:
			return ExitNotFound
		case apiErr.Status == http.StatusUnprocessableEntity, apiErr.Status == http.StatusRequestEntityTooLarge:
			return ExitValidation
		case apiErr.Status == http.StatusConflict:
			return ExitConflict
		case apiErr.Status == http.StatusTooManyRequests:
			return ExitRateLimit
		case apiErr.Status >= 500:
			return ExitServer
		}
	case errors.As(err, &netErr), errors.Is(err, context.DeadlineExceeded):
		return ExitNetwork
	}
	return ExitError
}

// ErrorBody is the JSON form of an error.
type ErrorBody struct {
	Code     string              `json:"code"`
	Message  string              `json:"message"`
	Status   int                 `json:"status,omitempty"`
	Fields   map[string][]string `json:"fields,omitempty"`
	Hint     string              `json:"hint,omitempty"`
	ExitCode int                 `json:"exit_code"`
}

// Describe turns an error into its JSON form.
func Describe(err error) ErrorBody {
	b := ErrorBody{Code: "error", Message: err.Error(), ExitCode: ExitCode(err)}
	var usage *UsageError
	var apiErr *api.Error
	var netErr *api.NetworkError
	switch {
	case errors.As(err, &usage):
		b.Code = "usage"
		b.Hint = "Run the command with --help, or `academic-page schema` for every command and field."
	case errors.As(err, &apiErr):
		b.Code, b.Message, b.Status, b.Fields, b.Hint = apiErr.Code, apiErr.Message, apiErr.Status, apiErr.Errors, apiErr.Hint()
	case errors.As(err, &netErr):
		b.Code = "network"
		b.Hint = "Check your connection and the base URL (--base-url or " + "ACADEMIC_PAGE_BASE_URL)."
	}
	return b
}

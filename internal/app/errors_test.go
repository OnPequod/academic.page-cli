package app

import (
	"net/http"
	"testing"

	"github.com/OnPequod/academic.page-cli/internal/api"
)

func TestEditConflict(t *testing.T) {
	err := &api.Error{Status: http.StatusConflict, Code: "edit_conflict", Message: "The post was edited since you read it."}
	if ExitCode(err) != ExitConflict {
		t.Errorf("exit code %d, want %d", ExitCode(err), ExitConflict)
	}
	if b := Describe(err); b.Code != "edit_conflict" || b.Hint == "" || b.ExitCode != ExitConflict {
		t.Errorf("describe = %+v", b)
	}
}

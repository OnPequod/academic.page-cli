package registry

import (
	"regexp"
	"testing"
)

func load(t *testing.T) *Registry {
	t.Helper()
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Every API operation must have a command or a stated reason not to, so the
// CLI keeps up when the spec is refreshed.
func TestEveryOperationIsMapped(t *testing.T) {
	r := load(t)
	if missing := r.Unmapped(); len(missing) > 0 {
		t.Fatalf("operations with no command; add them to the table or to Excluded: %v", missing)
	}
	for id := range Excluded {
		if _, ok := r.Spec.Operations[id]; !ok {
			t.Errorf("Excluded lists %s, which is no longer in the spec", id)
		}
	}
}

func TestToolNamesAreUniqueAndValid(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	seen := map[string]bool{}
	for _, c := range load(t).Commands {
		name := c.ToolName()
		if !valid.MatchString(name) {
			t.Errorf("tool name %q is not valid", name)
		}
		if seen[name] {
			t.Errorf("duplicate tool name %q", name)
		}
		seen[name] = true
	}
}

func TestCommandShapes(t *testing.T) {
	r := load(t)

	create := r.Find("posts", "create")
	if create.Kind != KindWrite || !create.Content || !create.Publishable || create.Arg != nil {
		t.Errorf("posts create: %+v", create)
	}
	for _, f := range create.Body {
		if f.Name == "content_structured" {
			t.Error("content_structured should be handled by --content-*, not a flag")
		}
	}

	get := r.Find("posts", "get")
	if get.Arg == nil || get.Arg.Name != "id" || get.DraftsOp == nil {
		t.Errorf("posts get: %+v", get)
	}

	list := r.Find("posts", "list")
	if !list.Paginated {
		t.Error("posts list should be paginated")
	}
	var slug *Field
	for i := range list.Query {
		if list.Query[i].Name == "slug" {
			slug = &list.Query[i]
		}
	}
	if slug == nil || !slug.PublishedOnly {
		t.Errorf("posts list --slug should exist and be published-only: %+v", slug)
	}

	upload := r.Find("media", "upload")
	if upload.FileArg == nil || upload.Kind != KindUpload {
		t.Errorf("media upload: %+v", upload)
	}

	if r.Find("sites", "list").NeedsSite() {
		t.Error("sites list should not need a site")
	}
}

func TestInputSchema(t *testing.T) {
	s := load(t).Find("posts", "update").InputSchema()
	props := s["properties"].(map[string]any)
	for _, name := range []string{"site", "id", "title", "content_markdown", "publish", "published_at", "dry_run"} {
		if _, ok := props[name]; !ok {
			t.Errorf("posts update input schema lacks %q", name)
		}
	}
	if req := s["required"].([]string); len(req) != 1 || req[0] != "id" {
		t.Errorf("required = %v", req)
	}
}

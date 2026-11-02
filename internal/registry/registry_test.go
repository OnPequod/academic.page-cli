package registry

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/OnPequod/academic.page-cli/internal/spec"
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
	if create.Kind != KindWrite || !create.IsTextFile("content_markdown") || !create.Publishable || create.Arg != nil {
		t.Errorf("posts create: %+v", create)
	}
	if r.Find("posts", "get").ManageFlag != "drafts" || r.Find("publications", "get").ManageFlag != "manage" {
		t.Error("drafts flag should be --drafts for posts and --manage for publications")
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
	if len(upload.Headers) != 1 || upload.Headers[0].Header != "Idempotency-Key" || upload.Headers[0].Name != "idempotency_key" || upload.Headers[0].Flag != "idempotency-key" {
		t.Errorf("media upload headers: %+v", upload.Headers)
	}

	if r.Find("sites", "list").NeedsSite() || r.Find("key", "show").NeedsSite() {
		t.Error("sites list and key show should not need a site")
	}

	lesson := r.Find("course-lessons", "update")
	if len(lesson.Parents) != 1 || lesson.Parents[0].Name != "course_id" || lesson.Parents[0].Param != "courseId" || lesson.Arg == nil || lesson.Arg.Param != "lessonId" {
		t.Errorf("course-lessons update: parents %+v, arg %+v", lesson.Parents, lesson.Arg)
	}
	if lessons := r.Find("course-lessons", "list"); len(lessons.Parents) != 1 || lessons.Arg != nil {
		t.Errorf("course-lessons list: parents %+v, arg %+v", lessons.Parents, lessons.Arg)
	}
	material := r.Find("lesson-materials", "get")
	if len(material.Parents) != 2 || material.Parents[1].Name != "lesson_id" || material.Arg.Param != "materialId" {
		t.Errorf("lesson-materials get: parents %+v, arg %+v", material.Parents, material.Arg)
	}

	imp := r.Find("publications", "import")
	if imp.Op.BodyType != "application/json" || !imp.IsTextFile("bibtex") {
		t.Errorf("publications import: body %s, text files %v", imp.Op.BodyType, imp.TextFiles)
	}
	for _, f := range imp.Body {
		if f.Name == "dry_run" || f.Name == "select" {
			t.Errorf("import body field %s duplicates the query parameter", f.Name)
		}
	}
	var preview *Field
	for i := range imp.Query {
		if imp.Query[i].Name == "dry_run" {
			preview = &imp.Query[i]
		}
	}
	if preview == nil || preview.Flag != "preview" || preview.OptionName() != "preview" {
		t.Errorf("import dry_run should be --preview: %+v", preview)
	}

	reorder := r.Find("profile-degrees", "reorder")
	if len(reorder.Body) != 1 || reorder.Body[0].Name != "ids" || reorder.Body[0].Items != "integer" {
		t.Errorf("reorder body: %+v", reorder.Body)
	}

	talk := r.Find("talks", "create")
	if !talk.IsTextFile("abstract_markdown") || !talk.Publishable {
		t.Errorf("talks create: text files %v", talk.TextFiles)
	}
	for _, f := range r.Find("publications", "create").Body {
		if f.Name == "on_match" && len(f.Enum) != 3 {
			t.Errorf("on_match enum = %v", f.Enum)
		}
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

	props = load(t).Find("talks", "update").InputSchema()["properties"].(map[string]any)
	for _, name := range []string{"abstract_markdown", "abstract_file", "tag_ids", "edit_version"} {
		if _, ok := props[name]; !ok {
			t.Errorf("talks update input schema lacks %q", name)
		}
	}
	if items := props["tag_ids"].(map[string]any)["items"].(map[string]any)["type"]; items != "integer" {
		t.Errorf("tag_ids items = %v", items)
	}

	props = load(t).Find("posts", "create").InputSchema()["properties"].(map[string]any)
	if desc, _ := props["content_structured"].(map[string]any)["description"].(string); !strings.Contains(desc, "content_markdown") {
		t.Errorf("content_structured should point to content_markdown: %q", desc)
	}

	props = load(t).Find("publications", "import").InputSchema()["properties"].(map[string]any)
	for _, name := range []string{"bibtex", "bibtex_file", "preview", "select", "dry_run"} {
		if _, ok := props[name]; !ok {
			t.Errorf("publications import input schema lacks %q", name)
		}
	}
	req := load(t).Find("course-lessons", "get").InputSchema()["required"].([]string)
	if len(req) != 2 || req[0] != "course_id" || req[1] != "id" {
		t.Errorf("course-lessons get required = %v", req)
	}
}

// Every enum in a request body (nested ones included) or query parameter
// must reach the command's input schema, which the schema command and the
// MCP tools share. This fails if a spec refresh adds choices the CLI drops.
func TestEnumsReachInputSchemas(t *testing.T) {
	r := load(t)
	checked := 0
	for _, c := range r.Commands {
		props := c.InputSchema()["properties"].(map[string]any)
		for _, f := range c.Query {
			for _, p := range append(append([]spec.Param{}, c.Op.Params...), draftsParams(c)...) {
				if p.Name == f.Name && p.In == "query" && len(p.Schema.Enum) > 0 {
					checked++
					assertEnum(t, c.Path()+" --"+f.Flag, props[f.OptionName()], p.Schema.Enum)
				}
			}
		}
		if c.Op.Body == nil {
			continue
		}
		for _, f := range c.Body {
			walkEnums(t, r.Spec, c.Path()+" "+f.Name, r.Spec.Resolve(c.Op.Body.Properties[f.Name]), props[f.OptionName()], &checked, 0)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d enums checked; the walk is broken", checked)
	}
}

func draftsParams(c *Command) []spec.Param {
	if c.DraftsOp == nil {
		return nil
	}
	return c.DraftsOp.Params
}

func walkEnums(t *testing.T, s *spec.Spec, where string, schema *spec.Schema, got any, checked *int, depth int) {
	t.Helper()
	if schema == nil || schema.ReadOnly || depth > 4 {
		return
	}
	if len(schema.Enum) > 0 {
		*checked++
		assertEnum(t, where, got, schema.Enum)
	}
	gotMap, _ := got.(map[string]any)
	switch schema.PrimaryType() {
	case "array":
		if item := schema.ItemSchema(); item != nil {
			walkEnums(t, s, where+"[]", s.Resolve(item), gotMap["items"], checked, depth+1)
		}
	case "object":
		gotProps, _ := gotMap["properties"].(map[string]any)
		for name, prop := range schema.Properties {
			walkEnums(t, s, where+"."+name, s.Resolve(prop), gotProps[name], checked, depth+1)
		}
	}
}

func assertEnum(t *testing.T, where string, got any, want []any) {
	t.Helper()
	m, ok := got.(map[string]any)
	if !ok {
		t.Errorf("%s: missing from the input schema", where)
		return
	}
	enum, _ := m["enum"].([]any)
	for _, v := range want {
		if !slices.Contains(enum, v) {
			t.Errorf("%s: input schema enum %v lacks %v", where, enum, v)
		}
	}
}

// The values the CLI/API audit (CLI-05) found missing, read from the spec so
// they cannot drift from it.
func TestAuditedEnums(t *testing.T) {
	r := load(t)
	enumOf := func(group, name, option string) []any {
		c := r.Find(group, name)
		prop, _ := c.InputSchema()["properties"].(map[string]any)[option].(map[string]any)
		enum, _ := prop["enum"].([]any)
		return enum
	}
	cases := []struct {
		group, name, option string
		want                []any
	}{
		{"lesson-materials", "create", "type", []any{"reading", "slides", "data_set", "code_file", "external_link", "resource", "no_file", "other"}},
		{"course-references", "create", "kind", []any{"textbook", "book", "purchased_resource", "reference_site", "data_set", "file_archive", "other"}},
		{"media", "list", "type", []any{"image", "video", "document", "audio", "other"}},
		{"talks", "create", "talk_type", nil},
		{"profile-links", "create", "service", nil},
		{"publications", "create", "type", nil},
		{"publications", "create", "on_match", nil},
	}
	for _, tc := range cases {
		got := enumOf(tc.group, tc.name, tc.option)
		if len(got) == 0 {
			t.Errorf("%s %s %s: no enum", tc.group, tc.name, tc.option)
		}
		for _, v := range tc.want {
			if !slices.Contains(got, v) {
				t.Errorf("%s %s %s: enum %v lacks %v", tc.group, tc.name, tc.option, got, v)
			}
		}
	}

	instances := r.Find("courses", "update").InputSchema()["properties"].(map[string]any)["instances"].(map[string]any)
	term := instances["items"].(map[string]any)["properties"].(map[string]any)["term"].(map[string]any)
	if enum, _ := term["enum"].([]any); len(enum) != 9 || !slices.Contains(enum, "q4") {
		t.Errorf("courses update instances[].term enum = %v", term["enum"])
	}
	if _, ok := instances["items"].(map[string]any)["properties"].(map[string]any)["is_current"]; ok {
		t.Error("read-only offering fields should not be inputs")
	}
}

// CLI-03: course writes carry the Course Info fields with their limits.
func TestCourseInfoFields(t *testing.T) {
	for _, name := range []string{"create", "update"} {
		props := load(t).Find("courses", name).InputSchema()["properties"].(map[string]any)
		for field, max := range map[string]int{"title": 255, "instructor_name": 255, "primary_institution": 255, "excerpt": 500} {
			p, ok := props[field].(map[string]any)
			if !ok {
				t.Errorf("courses %s lacks %s", name, field)
				continue
			}
			if p["maxLength"] != max {
				t.Errorf("courses %s %s maxLength = %v, want %d", name, field, p["maxLength"], max)
			}
		}
	}
}

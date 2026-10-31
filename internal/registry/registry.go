// Package registry maps OpenAPI operations to CLI commands.
//
// The table below is the only hand-written part: it names each command and
// says which operation backs it. Arguments, flags, body fields and their
// descriptions come from the embedded OpenAPI document, so the CLI commands,
// the schema command and the MCP tools always describe the same thing.
package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/OnPequod/academic.page-cli/internal/spec"
)

// Kind says what a command does to the site.
type Kind string

const (
	KindRead   Kind = "read"
	KindWrite  Kind = "write"
	KindDelete Kind = "delete"
	KindUpload Kind = "upload"
)

// Field is a value a command accepts: a positional argument, a query flag or
// a body field.
type Field struct {
	// Name is the API's name for the value, such as per_page.
	Name string `json:"name"`
	// Flag is the CLI flag, such as per-page. Empty for positional arguments.
	Flag        string `json:"flag,omitempty"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Nullable    bool   `json:"nullable,omitempty"`
	Default     any    `json:"default,omitempty"`
	// Option is the name MCP tools and the schema use, when it differs from
	// Name (the API's dry_run becomes preview, beside the CLI's dry_run).
	Option string `json:"option,omitempty"`
	// Items is an array's item type, such as integer or object.
	Items string `json:"items,omitempty"`
	Enum  []any  `json:"enum,omitempty"`
	// Param is the path parameter a positional argument fills.
	Param string `json:"-"`
	// DraftsOnly and PublishedOnly mark query flags that only one of the two
	// operations behind a read command accepts.
	DraftsOnly    bool `json:"drafts_only,omitempty"`
	PublishedOnly bool `json:"published_only,omitempty"`
}

// Command is one CLI command, such as "posts list".
type Command struct {
	Group string
	Name  string
	Short string
	Kind  Kind
	Op    *spec.Operation
	// DraftsOp is the management operation used with --drafts (or
	// --manage, for records without drafts), if any.
	DraftsOp *spec.Operation
	// ManageFlag names the flag that selects DraftsOp: "drafts" where the
	// group's records have drafts, otherwise "manage".
	ManageFlag string
	// Paginated commands accept --page, --per-page and --all.
	Paginated bool
	// Text commands return a non-JSON body, such as BibTeX.
	Text bool
	// Publishable writes accept --publish and default to drafts.
	Publishable bool
	// Rich lists body fields that hold rich text. Markdown is the normal way
	// to write them, through --<prefix>-markdown and --<prefix>-file (see
	// RichPrefix); the CLI converts it. The API's Tiptap form stays
	// available through --data for callers that want it.
	Rich []string
	// TextFiles lists string body fields that can be read from a file with
	// --<field>-file, such as the BibTeX of an import.
	TextFiles []string

	// Parents are the positional IDs of the records this one belongs to,
	// outermost first, such as the course and lesson of a material.
	Parents []*Field
	// Arg is the positional ID argument, if any.
	Arg *Field
	// FileArg is the positional file argument of an upload.
	FileArg *Field
	Query   []Field
	Body    []Field
}

// Path returns the command's words, such as "posts list".
func (c *Command) Path() string { return c.Group + " " + c.Name }

// ToolName returns the MCP tool name, such as posts_list.
func (c *Command) ToolName() string {
	return strings.ReplaceAll(c.Group, "-", "_") + "_" + strings.ReplaceAll(c.Name, "-", "_")
}

// NeedsSite reports whether the command's path has a {site} parameter.
func (c *Command) NeedsSite() bool { return strings.Contains(c.Op.Path, "{site}") }

// Group describes a command group.
type Group struct {
	Name  string
	Short string
}

type entry struct {
	group, name, short string
	kind               Kind
	op, draftsOp       string
	text               bool
	// parents name the leading path arguments that identify parent
	// records, such as course_id and lesson_id for lesson materials.
	parents []string
	// textFile is a string body field that --<field>-file can fill.
	textFile string
}

// RichPrefix returns the flag and option prefix of a Tiptap field:
// content_structured gives content, so --content-markdown and content_file.
func RichPrefix(field string) string { return strings.TrimSuffix(field, "_structured") }

// Groups lists the command groups in help order.
var Groups = []Group{
	{"key", "The API key in use"},
	{"sites", "Sites you can reach with this key"},
	{"profile", "The site owner's profile"},
	{"profile-cv", "The CV file on the profile"},
	{"profile-degrees", "Degrees on the profile"},
	{"profile-positions", "Positions on the profile"},
	{"profile-links", "Links on the profile"},
	{"posts", "Blog posts"},
	{"pages", "Pages"},
	{"projects", "Projects"},
	{"publications", "Publications"},
	{"talks", "Talks"},
	{"courses", "Courses"},
	{"course-lessons", "Lessons in a course"},
	{"lesson-materials", "Materials in a lesson"},
	{"course-assignments", "Assignments in a course"},
	{"course-references", "References (readings) in a course"},
	{"lab-members", "Lab members (the study team on study sites)"},
	{"study", "Study details"},
	{"tags", "Tags"},
	{"media", "Media library files"},
}

var (
	course = []string{"course_id"}
	lesson = []string{"course_id", "lesson_id"}
)

var table = []entry{
	{group: "key", name: "show", short: "Show this key's abilities, site scope, Pro status and expiry", op: "getMe"},

	{group: "sites", name: "list", short: "List the sites this key can read", op: "listSites"},
	{group: "sites", name: "get", short: "Show a site's settings, domains, sections and navigation", op: "getSite"},
	{group: "sites", name: "manifest", short: "List every public record's ID and timestamps", op: "getManifest"},

	{group: "profile", name: "get", short: "Show the profile", op: "getProfile", draftsOp: "manageGetProfile"},
	{group: "profile", name: "update", short: "Update the profile (saves like the Save button)", op: "manageUpdateProfile", kind: KindWrite},

	{group: "profile-cv", name: "get", short: "Show the CV file attached to the profile", op: "manageGetProfileCv"},
	{group: "profile-cv", name: "set", short: "Attach a media file as the profile's CV", op: "manageSetProfileCv", kind: KindWrite},
	{group: "profile-cv", name: "delete", short: "Remove the profile's CV", op: "manageDeleteProfileCv", kind: KindDelete},

	{group: "profile-degrees", name: "list", short: "List degrees", op: "manageListProfileDegrees"},
	{group: "profile-degrees", name: "get", short: "Show a degree", op: "manageGetProfileDegree"},
	{group: "profile-degrees", name: "create", short: "Add a degree", op: "manageCreateProfileDegree", kind: KindWrite},
	{group: "profile-degrees", name: "update", short: "Update a degree", op: "manageUpdateProfileDegree", kind: KindWrite},
	{group: "profile-degrees", name: "delete", short: "Delete a degree", op: "manageDeleteProfileDegree", kind: KindDelete},
	{group: "profile-degrees", name: "reorder", short: "Set the order of the degrees", op: "manageReorderProfileDegrees", kind: KindWrite},

	{group: "profile-positions", name: "list", short: "List positions", op: "manageListProfilePositions"},
	{group: "profile-positions", name: "get", short: "Show a position", op: "manageGetProfilePosition"},
	{group: "profile-positions", name: "create", short: "Add a position", op: "manageCreateProfilePosition", kind: KindWrite},
	{group: "profile-positions", name: "update", short: "Update a position", op: "manageUpdateProfilePosition", kind: KindWrite},
	{group: "profile-positions", name: "delete", short: "Delete a position", op: "manageDeleteProfilePosition", kind: KindDelete},
	{group: "profile-positions", name: "reorder", short: "Set the order of the positions", op: "manageReorderProfilePositions", kind: KindWrite},

	{group: "profile-links", name: "list", short: "List links", op: "manageListProfileLinks"},
	{group: "profile-links", name: "get", short: "Show a link", op: "manageGetProfileLink"},
	{group: "profile-links", name: "create", short: "Add a link", op: "manageCreateProfileLink", kind: KindWrite},
	{group: "profile-links", name: "update", short: "Update a link", op: "manageUpdateProfileLink", kind: KindWrite},
	{group: "profile-links", name: "delete", short: "Delete a link", op: "manageDeleteProfileLink", kind: KindDelete},
	{group: "profile-links", name: "reorder", short: "Set the order of the links", op: "manageReorderProfileLinks", kind: KindWrite},

	{group: "posts", name: "list", short: "List posts", op: "listPosts", draftsOp: "manageListPosts"},
	{group: "posts", name: "get", short: "Show a post", op: "getPost", draftsOp: "manageGetPost"},
	{group: "posts", name: "create", short: "Create a post (a draft unless --publish)", op: "manageCreatePost", kind: KindWrite},
	{group: "posts", name: "update", short: "Update a post", op: "manageUpdatePost", kind: KindWrite},
	{group: "posts", name: "delete", short: "Delete a post", op: "manageDeletePost", kind: KindDelete},

	{group: "pages", name: "list", short: "List pages", op: "listPages", draftsOp: "manageListPages"},
	{group: "pages", name: "get", short: "Show a page", op: "getPage", draftsOp: "manageGetPage"},
	{group: "pages", name: "create", short: "Create a page (a draft unless --publish)", op: "manageCreatePage", kind: KindWrite},
	{group: "pages", name: "update", short: "Update a page", op: "manageUpdatePage", kind: KindWrite},
	{group: "pages", name: "delete", short: "Delete a page", op: "manageDeletePage", kind: KindDelete},

	{group: "projects", name: "list", short: "List projects", op: "listProjects", draftsOp: "manageListProjects"},
	{group: "projects", name: "get", short: "Show a project", op: "getProject", draftsOp: "manageGetProject"},
	{group: "projects", name: "create", short: "Create a project (a draft unless --publish)", op: "manageCreateProject", kind: KindWrite},
	{group: "projects", name: "update", short: "Update a project", op: "manageUpdateProject", kind: KindWrite},
	{group: "projects", name: "delete", short: "Delete a project", op: "manageDeleteProject", kind: KindDelete},

	{group: "publications", name: "list", short: "List publications (CSL-JSON, formatted citation, DOI)", op: "listPublications", draftsOp: "manageListPublications"},
	{group: "publications", name: "get", short: "Show a publication", op: "getPublication", draftsOp: "manageGetPublication"},
	{group: "publications", name: "bibtex", short: "Export every publication as BibTeX", op: "exportPublicationsBibtex", text: true},
	{group: "publications", name: "create", short: "Add a publication (skips matches unless --on-match says otherwise)", op: "manageCreatePublication", kind: KindWrite},
	{group: "publications", name: "update", short: "Update a publication", op: "manageUpdatePublication", kind: KindWrite},
	{group: "publications", name: "delete", short: "Delete a publication", op: "manageDeletePublication", kind: KindDelete},
	{group: "publications", name: "import", short: "Import BibTeX (use --preview first to see what would change)", op: "manageImportPublications", kind: KindWrite, textFile: "bibtex"},
	{group: "publications", name: "apply-suggestion", short: "Apply a publication's pending suggested change", op: "manageApplyPublicationSuggestion", kind: KindWrite},
	{group: "publications", name: "keep-citation", short: "Dismiss a publication's suggestion and keep its citation", op: "manageKeepPublicationCitation", kind: KindWrite},

	{group: "talks", name: "list", short: "List talks", op: "listTalks", draftsOp: "manageListTalks"},
	{group: "talks", name: "get", short: "Show a talk", op: "getTalk", draftsOp: "manageGetTalk"},
	{group: "talks", name: "create", short: "Create a talk (a draft unless --publish)", op: "manageCreateTalk", kind: KindWrite},
	{group: "talks", name: "update", short: "Update a talk", op: "manageUpdateTalk", kind: KindWrite},
	{group: "talks", name: "delete", short: "Delete a talk", op: "manageDeleteTalk", kind: KindDelete},

	{group: "courses", name: "list", short: "List courses", op: "listCourses", draftsOp: "manageListCourses"},
	{group: "courses", name: "get", short: "Show a course", op: "getCourse", draftsOp: "manageGetCourse"},
	{group: "courses", name: "create", short: "Create a course (a draft unless --publish)", op: "manageCreateCourse", kind: KindWrite},
	{group: "courses", name: "update", short: "Update a course", op: "manageUpdateCourse", kind: KindWrite},
	{group: "courses", name: "delete", short: "Delete a course", op: "manageDeleteCourse", kind: KindDelete},

	{group: "course-lessons", name: "list", short: "List a course's lessons", op: "manageListCourseLessons", parents: course},
	{group: "course-lessons", name: "get", short: "Show a lesson", op: "manageGetCourseLesson", parents: course},
	{group: "course-lessons", name: "create", short: "Add a lesson to a course", op: "manageCreateCourseLesson", kind: KindWrite, parents: course},
	{group: "course-lessons", name: "update", short: "Update a lesson", op: "manageUpdateCourseLesson", kind: KindWrite, parents: course},
	{group: "course-lessons", name: "delete", short: "Delete a lesson", op: "manageDeleteCourseLesson", kind: KindDelete, parents: course},
	{group: "course-lessons", name: "reorder", short: "Set the order of a course's lessons", op: "manageReorderCourseLessons", kind: KindWrite, parents: course},

	{group: "lesson-materials", name: "list", short: "List a lesson's materials", op: "manageListCourseMaterials", parents: lesson},
	{group: "lesson-materials", name: "get", short: "Show a material", op: "manageGetCourseMaterial", parents: lesson},
	{group: "lesson-materials", name: "create", short: "Add a material to a lesson", op: "manageCreateCourseMaterial", kind: KindWrite, parents: lesson},
	{group: "lesson-materials", name: "update", short: "Update a material", op: "manageUpdateCourseMaterial", kind: KindWrite, parents: lesson},
	{group: "lesson-materials", name: "delete", short: "Delete a material", op: "manageDeleteCourseMaterial", kind: KindDelete, parents: lesson},
	{group: "lesson-materials", name: "reorder", short: "Set the order of a lesson's materials", op: "manageReorderCourseMaterials", kind: KindWrite, parents: lesson},

	{group: "course-assignments", name: "list", short: "List a course's assignments", op: "manageListCourseAssignments", parents: course},
	{group: "course-assignments", name: "get", short: "Show an assignment", op: "manageGetCourseAssignment", parents: course},
	{group: "course-assignments", name: "create", short: "Add an assignment to a course", op: "manageCreateCourseAssignment", kind: KindWrite, parents: course},
	{group: "course-assignments", name: "update", short: "Update an assignment", op: "manageUpdateCourseAssignment", kind: KindWrite, parents: course},
	{group: "course-assignments", name: "delete", short: "Delete an assignment", op: "manageDeleteCourseAssignment", kind: KindDelete, parents: course},

	{group: "course-references", name: "list", short: "List a course's references (readings)", op: "manageListCourseReferences", parents: course},
	{group: "course-references", name: "get", short: "Show a reference", op: "manageGetCourseReference", parents: course},
	{group: "course-references", name: "create", short: "Add a reference to a course", op: "manageCreateCourseReference", kind: KindWrite, parents: course},
	{group: "course-references", name: "update", short: "Update a reference", op: "manageUpdateCourseReference", kind: KindWrite, parents: course},
	{group: "course-references", name: "delete", short: "Delete a reference", op: "manageDeleteCourseReference", kind: KindDelete, parents: course},
	{group: "course-references", name: "reorder", short: "Set the order of a course's references", op: "manageReorderCourseReferences", kind: KindWrite, parents: course},

	{group: "lab-members", name: "list", short: "List lab members", op: "listLabMembers", draftsOp: "manageListLabMembers"},
	{group: "lab-members", name: "get", short: "Show a lab member", op: "getLabMember", draftsOp: "manageGetLabMember"},
	{group: "lab-members", name: "create", short: "Add a lab member (a draft unless --publish)", op: "manageCreateLabMember", kind: KindWrite},
	{group: "lab-members", name: "update", short: "Update a lab member", op: "manageUpdateLabMember", kind: KindWrite},
	{group: "lab-members", name: "delete", short: "Delete a lab member", op: "manageDeleteLabMember", kind: KindDelete},

	{group: "study", name: "get", short: "Show study details", op: "getStudy", draftsOp: "manageGetStudy"},
	{group: "study", name: "update", short: "Update study details", op: "manageUpdateStudy", kind: KindWrite},

	{group: "tags", name: "list", short: "List tags", op: "listTags", draftsOp: "manageListTags"},
	{group: "tags", name: "get", short: "Show a tag", op: "getTag", draftsOp: "manageGetTag"},
	{group: "tags", name: "create", short: "Create a tag", op: "manageCreateTag", kind: KindWrite},
	{group: "tags", name: "update", short: "Rename a tag", op: "manageUpdateTag", kind: KindWrite},
	{group: "tags", name: "delete", short: "Delete a tag", op: "manageDeleteTag", kind: KindDelete},

	{group: "media", name: "list", short: "List media files", op: "listMedia", draftsOp: "manageListMedia"},
	{group: "media", name: "get", short: "Show a media file", op: "getMediaItem", draftsOp: "manageGetMedia"},
	{group: "media", name: "upload", short: "Upload a file (up to 1 GB) to the media library", op: "manageUploadMedia", kind: KindUpload},
	{group: "media", name: "update", short: "Update a media file's alt text, caption or description", op: "manageUpdateMedia", kind: KindWrite},
	{group: "media", name: "delete", short: "Delete a media file", op: "manageDeleteMedia", kind: KindDelete},
}

// Excluded lists operations the CLI deliberately has no command for, with
// the reason. Every other operation in the spec must appear in the table.
var Excluded = map[string]string{
	"getOpenApi":              "the spec is embedded; see `academic-page schema --openapi`",
	"manageMultipartInit":     "used by `media upload`, which sends every file in parts",
	"manageMultipartSign":     "used by `media upload`, which sends every file in parts",
	"manageMultipartComplete": "used by `media upload`, which sends every file in parts",
	"manageMultipartAbort":    "used by `media upload`, which sends every file in parts",
}

// Registry holds the built commands.
type Registry struct {
	Spec     *spec.Spec
	Commands []*Command
}

// Load builds the registry from the embedded spec.
func Load() (*Registry, error) {
	s, err := spec.Load()
	if err != nil {
		return nil, err
	}
	return Build(s)
}

// Build builds the registry from a spec.
func Build(s *spec.Spec) (*Registry, error) {
	r := &Registry{Spec: s}
	for _, e := range table {
		c, err := build(s, e)
		if err != nil {
			return nil, err
		}
		r.Commands = append(r.Commands, c)
	}
	drafts := map[string]bool{}
	for _, c := range r.Commands {
		if c.Publishable {
			drafts[c.Group] = true
		}
	}
	for _, c := range r.Commands {
		if c.DraftsOp == nil {
			continue
		}
		c.ManageFlag = "manage"
		if drafts[c.Group] {
			c.ManageFlag = "drafts"
		}
	}
	return r, nil
}

// Find returns the command with the given group and name.
func (r *Registry) Find(group, name string) *Command {
	for _, c := range r.Commands {
		if c.Group == group && c.Name == name {
			return c
		}
	}
	return nil
}

// FindTool returns the command with the given MCP tool name.
func (r *Registry) FindTool(name string) *Command {
	for _, c := range r.Commands {
		if c.ToolName() == name {
			return c
		}
	}
	return nil
}

// Unmapped returns spec operations with neither a command nor an exclusion.
func (r *Registry) Unmapped() []string {
	used := map[string]bool{}
	for _, c := range r.Commands {
		used[c.Op.ID] = true
		if c.DraftsOp != nil {
			used[c.DraftsOp.ID] = true
		}
	}
	var out []string
	for _, id := range r.Spec.OperationIDs() {
		if !used[id] && Excluded[id] == "" {
			out = append(out, id)
		}
	}
	return out
}

func build(s *spec.Spec, e entry) (*Command, error) {
	op, ok := s.Operations[e.op]
	if !ok {
		return nil, fmt.Errorf("%s %s: operation %s is not in the spec", e.group, e.name, e.op)
	}
	kind := e.kind
	if kind == "" {
		kind = KindRead
	}
	c := &Command{Group: e.group, Name: e.name, Short: e.short, Kind: kind, Op: op, Text: e.text}
	if e.draftsOp != "" {
		if c.DraftsOp, ok = s.Operations[e.draftsOp]; !ok {
			return nil, fmt.Errorf("%s %s: operation %s is not in the spec", e.group, e.name, e.draftsOp)
		}
	}

	args := pathArgs(op)
	if len(e.parents) > len(args) {
		return nil, fmt.Errorf("%s %s: %s has fewer path arguments than parents", e.group, e.name, op.ID)
	}
	for i, name := range e.parents {
		args[i].Name = name
	}
	c.Parents, args = args[:len(e.parents)], args[len(e.parents):]
	if len(args) > 1 {
		return nil, fmt.Errorf("%s %s: %s has more path arguments than the CLI maps", e.group, e.name, op.ID)
	}
	if len(args) == 1 {
		c.Arg = args[0]
	}
	if c.DraftsOp != nil && (c.Arg == nil) != (len(pathArgs(c.DraftsOp)) == 0) {
		return nil, fmt.Errorf("%s %s: %s and %s take different path arguments", e.group, e.name, op.ID, c.DraftsOp.ID)
	}

	c.Query = queryFields(s, op, c.DraftsOp)
	inQuery := map[string]bool{}
	for _, f := range c.Query {
		inQuery[f.Name] = true
		if f.Name == "page" || f.Name == "per_page" {
			c.Paginated = true
		}
	}

	if op.Body != nil {
		for _, name := range sortedKeys(op.Body.Properties) {
			if inQuery[name] {
				// Sent in the query string instead; one flag is enough.
				continue
			}
			prop := s.Resolve(op.Body.Properties[name])
			f := field(s, name, prop)
			f.Required = contains(op.Body.Required, name)
			if prop.Format == "binary" {
				f.Flag = ""
				c.FileArg = &f
				continue
			}
			if f.Type == "object" && (strings.HasSuffix(name, "_structured") || strings.Contains(prop.Description, "Tiptap")) {
				c.Rich = append(c.Rich, name)
			}
			if name == "published_at" {
				c.Publishable = true
			}
			c.Body = append(c.Body, f)
		}
	}
	if e.textFile != "" {
		if !hasField(c.Body, e.textFile) {
			return nil, fmt.Errorf("%s %s: %s has no body field %s", e.group, e.name, op.ID, e.textFile)
		}
		c.TextFiles = []string{e.textFile}
	}
	return c, nil
}

// pathArgs returns an operation's path arguments other than the site. The
// last one is always named "id", so every tool and command names it the
// same way.
func pathArgs(op *spec.Operation) []*Field {
	var out []*Field
	for _, p := range op.Params {
		if p.In == "path" && p.Name != "site" {
			f := field(nil, p.Name, p.Schema)
			f.Flag = ""
			f.Description = p.Description
			f.Required = true
			f.Param = p.Name
			out = append(out, &f)
		}
	}
	if len(out) > 0 {
		out[len(out)-1].Name = "id"
	}
	return out
}

func queryFields(s *spec.Spec, op, draftsOp *spec.Operation) []Field {
	published := params(s, op, "query")
	if draftsOp == nil {
		return published
	}
	drafts := params(s, draftsOp, "query")
	byName := map[string]bool{}
	for _, f := range drafts {
		byName[f.Name] = true
	}
	var out []Field
	seen := map[string]bool{}
	for _, f := range published {
		f.PublishedOnly = !byName[f.Name]
		out = append(out, f)
		seen[f.Name] = true
	}
	for _, f := range drafts {
		if !seen[f.Name] {
			f.DraftsOnly = true
			out = append(out, f)
		}
	}
	return out
}

func params(s *spec.Spec, op *spec.Operation, in string) []Field {
	var out []Field
	for _, p := range op.Params {
		if p.In != in {
			continue
		}
		f := field(s, p.Name, p.Schema)
		f.Description = p.Description
		f.Required = p.Required
		out = append(out, f)
	}
	return out
}

func field(s *spec.Spec, name string, schema *spec.Schema) Field {
	types, nullable := schema.Types()
	typ := "string"
	if len(types) > 0 {
		typ = types[0]
	}
	f := Field{
		Name:        name,
		Flag:        strings.ReplaceAll(name, "_", "-"),
		Type:        typ,
		Format:      schema.Format,
		Description: schema.Description,
		Nullable:    nullable,
		Default:     schema.Default,
	}
	for _, v := range schema.Enum {
		if v != nil {
			f.Enum = append(f.Enum, v)
		}
	}
	if typ == "array" {
		f.Items = "string"
		if item := schema.ItemSchema(); item != nil {
			if s != nil {
				item = s.Resolve(item)
			}
			f.Items = item.PrimaryType()
		}
	}
	if name == "dry_run" {
		// The API's own preview. The CLI's --dry-run sends nothing, so this
		// one gets another name.
		f.Flag, f.Option = "preview", "preview"
		f.Description = "Ask the API what would change, without saving anything (the request is sent). " + f.Description
	}
	return f
}

// OptionName returns the name MCP tools and the schema use for the field.
func (f Field) OptionName() string {
	if f.Option != "" {
		return f.Option
	}
	return f.Name
}

func hasField(fields []Field, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

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
	// DraftsOp is the management operation used with --drafts, if any.
	DraftsOp *spec.Operation
	// Paginated commands accept --page, --per-page and --all.
	Paginated bool
	// Text commands return a non-JSON body, such as BibTeX.
	Text bool
	// Publishable writes accept --publish and default to drafts.
	Publishable bool
	// Content writes accept --content-file and --content-markdown.
	Content bool

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
}

// Groups lists the command groups in help order.
var Groups = []Group{
	{"sites", "Sites you can reach with this key"},
	{"profile", "The site owner's profile"},
	{"posts", "Blog posts"},
	{"pages", "Pages"},
	{"projects", "Projects"},
	{"publications", "Publications"},
	{"talks", "Talks"},
	{"courses", "Courses"},
	{"lab-members", "Lab members"},
	{"study", "Study content"},
	{"tags", "Tags"},
	{"media", "Media library files"},
}

var table = []entry{
	{group: "sites", name: "list", short: "List the sites this key can read", op: "listSites"},
	{group: "sites", name: "get", short: "Show a site's settings, domains, sections and navigation", op: "getSite"},
	{group: "sites", name: "manifest", short: "List every public record's ID and timestamps", op: "getManifest"},

	{group: "profile", name: "get", short: "Show the profile", op: "getProfile"},
	{group: "study", name: "get", short: "Show study content", op: "getStudy"},

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

	{group: "publications", name: "list", short: "List publications (CSL-JSON, formatted citation, DOI)", op: "listPublications"},
	{group: "publications", name: "get", short: "Show a publication", op: "getPublication"},
	{group: "publications", name: "bibtex", short: "Export every publication as BibTeX", op: "exportPublicationsBibtex", text: true},

	{group: "talks", name: "list", short: "List talks", op: "listTalks"},
	{group: "talks", name: "get", short: "Show a talk", op: "getTalk"},

	{group: "courses", name: "list", short: "List courses", op: "listCourses"},
	{group: "courses", name: "get", short: "Show a course", op: "getCourse"},

	{group: "lab-members", name: "list", short: "List lab members", op: "listLabMembers"},
	{group: "lab-members", name: "get", short: "Show a lab member", op: "getLabMember"},

	{group: "tags", name: "list", short: "List tags", op: "listTags"},
	{group: "tags", name: "get", short: "Show a tag", op: "getTag"},

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

	c.Arg = pathArg(op)
	if c.DraftsOp != nil && (c.Arg == nil) != (pathArg(c.DraftsOp) == nil) {
		return nil, fmt.Errorf("%s %s: %s and %s take different path arguments", e.group, e.name, op.ID, c.DraftsOp.ID)
	}

	c.Query = queryFields(op, c.DraftsOp)
	for _, f := range c.Query {
		if f.Name == "page" || f.Name == "per_page" {
			c.Paginated = true
		}
	}

	if op.Body != nil {
		for _, name := range sortedKeys(op.Body.Properties) {
			prop := s.Resolve(op.Body.Properties[name])
			f := field(name, prop)
			f.Required = contains(op.Body.Required, name)
			if prop.Format == "binary" {
				f.Flag = ""
				c.FileArg = &f
				continue
			}
			switch name {
			case "content_structured":
				c.Content = true
				continue
			case "published_at":
				c.Publishable = true
			}
			c.Body = append(c.Body, f)
		}
	}
	return c, nil
}

func pathArg(op *spec.Operation) *Field {
	for _, p := range op.Params {
		if p.In == "path" && p.Name != "site" {
			// Always "id", so every tool and command names it the same way.
			f := field("id", p.Schema)
			f.Flag = ""
			f.Description = p.Description
			f.Required = true
			return &f
		}
	}
	return nil
}

func queryFields(op, draftsOp *spec.Operation) []Field {
	published := params(op, "query")
	if draftsOp == nil {
		return published
	}
	drafts := params(draftsOp, "query")
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

func params(op *spec.Operation, in string) []Field {
	var out []Field
	for _, p := range op.Params {
		if p.In != in {
			continue
		}
		f := field(p.Name, p.Schema)
		f.Description = p.Description
		f.Required = p.Required
		out = append(out, f)
	}
	return out
}

func field(name string, schema *spec.Schema) Field {
	types, nullable := schema.Types()
	typ := "string"
	if len(types) > 0 {
		typ = types[0]
	}
	return Field{
		Name:        name,
		Flag:        strings.ReplaceAll(name, "_", "-"),
		Type:        typ,
		Format:      schema.Format,
		Description: schema.Description,
		Nullable:    nullable,
		Default:     schema.Default,
	}
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

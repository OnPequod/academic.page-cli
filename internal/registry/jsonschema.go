package registry

import (
	"fmt"
	"slices"
	"strings"
)

// Option names shared by the MCP tools and the schema output. Body and query
// fields keep their API names; these are the extra controls.
const (
	OptSite    = "site"
	OptAll     = "all"
	OptDryRun  = "dry_run"
	OptPublish = "publish"
)

// MarkdownOption and FileOption name the options that fill a Tiptap or text
// field from Markdown or a file, such as content_markdown and bibtex_file.
func MarkdownOption(field string) string { return RichPrefix(field) + "_markdown" }

// FileOption is described with MarkdownOption.
func FileOption(field string) string { return RichPrefix(field) + "_file" }

// InputSchema returns a JSON Schema for the command's inputs, as an MCP tool
// receives them: positional arguments, query fields and body fields by API
// name, plus the controls above.
func (c *Command) InputSchema() map[string]any {
	props := map[string]any{}
	var required []string

	if c.NeedsSite() {
		props[OptSite] = map[string]any{"type": "integer", "minimum": 1, "description": "Site ID. Defaults to the profile's site."}
	}
	for _, f := range []*Field{c.Parent, c.Arg, c.FileArg} {
		if f == nil {
			continue
		}
		s := fieldSchema(*f)
		if f == c.FileArg {
			s = map[string]any{"type": "string", "description": "Path of the local file to upload."}
		}
		props[f.Name] = s
		required = append(required, f.Name)
	}
	for _, f := range c.Query {
		props[f.OptionName()] = fieldSchema(f)
	}
	for _, f := range c.Body {
		s := fieldSchema(f)
		if c.IsRich(f.Name) {
			s["description"] = fmt.Sprintf(`%s Prefer %s.`, strings.TrimSpace(f.Description+` A Tiptap document ({"type":"doc","content":[...]}).`), MarkdownOption(f.Name))
		}
		props[f.OptionName()] = s
	}
	for _, name := range c.Rich {
		props[MarkdownOption(name)] = map[string]any{"type": "string", "description": fmt.Sprintf("%s as Markdown; converted to the editor's format.", name)}
		props[FileOption(name)] = map[string]any{"type": "string", "description": fmt.Sprintf("Path of a Markdown (.md) or Tiptap JSON (.json) file for %s.", name)}
	}
	for _, name := range c.TextFiles {
		props[FileOption(name)] = map[string]any{"type": "string", "description": fmt.Sprintf("Path of a file whose text is sent as %s.", name)}
	}
	if c.DraftsOp != nil {
		desc := "Read through the management API, which includes drafts and unpublished records. Needs a management key."
		if c.ManageFlag != "drafts" {
			desc = "Read through the management API, which adds edit_version and management fields. Needs a management key."
		}
		props[c.ManageFlag] = map[string]any{"type": "boolean", "description": desc}
	}
	if c.Paginated {
		props[OptAll] = map[string]any{"type": "boolean", "description": "Follow every page and return all records."}
	}
	if c.Kind != KindRead {
		props[OptDryRun] = map[string]any{"type": "boolean", "description": "Return the request that would be sent, without sending it."}
	}
	if c.Publishable {
		props[OptPublish] = map[string]any{"type": "boolean", "description": "Publish now. Without it, creates are drafts."}
		props["published_at"] = map[string]any{"type": []string{"string", "null"}, "format": "date-time", "description": "Publish time; null makes it a draft."}
	}

	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func fieldSchema(f Field) map[string]any {
	s := map[string]any{}
	t := f.Type
	if t == "" {
		t = "string"
	}
	if f.Nullable {
		s["type"] = []string{t, "null"}
	} else {
		s["type"] = t
	}
	if t == "array" {
		items := f.Items
		if items == "" {
			items = "string"
		}
		s["items"] = map[string]any{"type": items}
	}
	if len(f.Enum) > 0 {
		enum := f.Enum
		if f.Nullable {
			enum = append(append([]any{}, enum...), nil)
		}
		s["enum"] = enum
	}
	if f.Format != "" {
		s["format"] = f.Format
	}
	desc := f.Description
	if f.DraftsOnly {
		desc += " Only with the management read (drafts or manage = true)."
	}
	if f.PublishedOnly {
		desc += " Only without the management read."
	}
	if desc != "" {
		s["description"] = desc
	}
	if f.Default != nil {
		s["default"] = f.Default
	}
	return s
}

// IsRich reports whether the body field holds a Tiptap document.
func (c *Command) IsRich(name string) bool { return slices.Contains(c.Rich, name) }

// IsTextFile reports whether the body field can be read from a file.
func (c *Command) IsTextFile(name string) bool { return slices.Contains(c.TextFiles, name) }

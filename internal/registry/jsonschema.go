package registry

import (
	"fmt"
	"slices"
	"strings"

	"github.com/OnPequod/academic.page-cli/internal/spec"
)

// Option names shared by the MCP tools and the schema output. Body and query
// fields keep their API names; these are the extra controls.
const (
	OptSite    = "site"
	OptAll     = "all"
	OptDryRun  = "dry_run"
	OptPublish = "publish"
)

// MarkdownOption names a Tiptap field's Markdown sibling, and FileOption the
// option that fills a text field from a file, such as content_file and
// bibtex_file.
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
	for _, f := range append(append([]*Field{}, c.Parents...), c.Arg, c.FileArg) {
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
	for _, f := range c.Headers {
		props[f.OptionName()] = fieldSchema(f)
	}
	for _, f := range c.Body {
		s := fieldSchema(f)
		if slices.Contains(c.Tiptap, f.Name) {
			s["description"] = fmt.Sprintf("Advanced: the editor's Tiptap JSON document. Use %s instead unless you need exact control.", MarkdownOption(f.Name))
		}
		props[f.OptionName()] = s
	}
	for _, name := range c.TextFiles {
		props[FileOption(name)] = map[string]any{"type": "string", "description": fmt.Sprintf("Path of a file whose text is sent as %s.", name)}
		if strings.HasSuffix(name, "_markdown") {
			props[FileOption(name)] = map[string]any{"type": "string", "description": fmt.Sprintf("Path of a Markdown file whose text is sent as %s.", name)}
		}
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
		if c.PublishingField() == "published_at" {
			props["published_at"] = map[string]any{"type": []string{"string", "null"}, "format": "date-time", "description": "Publish time; null makes it a draft."}
		}
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
		if f.ItemSchema != nil {
			s["items"] = f.ItemSchema
		}
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
	if f.MaxLength != nil {
		s["maxLength"] = *f.MaxLength
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

// IsTextFile reports whether the body field can be read from a file.
func (c *Command) IsTextFile(name string) bool { return slices.Contains(c.TextFiles, name) }

// valueSchema converts a spec schema into the JSON Schema an input takes:
// references resolved, read-only properties left out, and enums and limits
// kept, so nested values such as a course offering's term list their choices.
func valueSchema(s *spec.Spec, schema *spec.Schema, depth int) map[string]any {
	if s != nil {
		schema = s.Resolve(schema)
	}
	out := map[string]any{}
	types, nullable := schema.Types()
	t := "object"
	if len(types) > 0 {
		t = types[0]
	}
	if nullable {
		out["type"] = []string{t, "null"}
	} else {
		out["type"] = t
	}
	if schema.Description != "" {
		out["description"] = schema.Description
	}
	if schema.Format != "" {
		out["format"] = schema.Format
	}
	if schema.MaxLength != nil {
		out["maxLength"] = *schema.MaxLength
	}
	if schema.Default != nil {
		out["default"] = schema.Default
	}
	if len(schema.Enum) > 0 {
		enum := append([]any{}, schema.Enum...)
		if nullable && !slices.Contains(enum, nil) {
			enum = append(enum, nil)
		}
		out["enum"] = enum
	}
	if depth >= 4 {
		return out
	}
	switch t {
	case "array":
		if item := schema.ItemSchema(); item != nil {
			out["items"] = valueSchema(s, item, depth+1)
		}
	case "object":
		if len(schema.Properties) == 0 {
			break
		}
		props := map[string]any{}
		for name, prop := range schema.Properties {
			if s != nil {
				prop = s.Resolve(prop)
			}
			if prop.ReadOnly {
				continue
			}
			props[name] = valueSchema(s, prop, depth+1)
		}
		out["properties"] = props
		var required []string
		for _, name := range schema.Required {
			if _, ok := props[name]; ok {
				required = append(required, name)
			}
		}
		if len(required) > 0 {
			out["required"] = required
		}
	}
	return out
}

package registry

// Option names shared by the MCP tools and the schema output. Body and query
// fields keep their API names; these are the extra controls.
const (
	OptSite            = "site"
	OptDrafts          = "drafts"
	OptAll             = "all"
	OptDryRun          = "dry_run"
	OptPublish         = "publish"
	OptContentMarkdown = "content_markdown"
	OptContentFile     = "content_file"
)

// InputSchema returns a JSON Schema for the command's inputs, as an MCP tool
// receives them: positional arguments, query fields and body fields by API
// name, plus the controls above.
func (c *Command) InputSchema() map[string]any {
	props := map[string]any{}
	var required []string

	if c.NeedsSite() {
		props[OptSite] = map[string]any{"type": "integer", "minimum": 1, "description": "Site ID. Defaults to the profile's site."}
	}
	for _, f := range []*Field{c.Arg, c.FileArg} {
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
		props[f.Name] = fieldSchema(f)
	}
	for _, f := range c.Body {
		props[f.Name] = fieldSchema(f)
	}
	if c.DraftsOp != nil {
		props[OptDrafts] = map[string]any{"type": "boolean", "description": "Read through the management API, including drafts and unpublished records. Needs a management key."}
	}
	if c.Paginated {
		props[OptAll] = map[string]any{"type": "boolean", "description": "Follow every page and return all records."}
	}
	if c.Kind != KindRead {
		props[OptDryRun] = map[string]any{"type": "boolean", "description": "Return the request that would be sent, without sending it."}
	}
	if c.Content {
		props[OptContentMarkdown] = map[string]any{"type": "string", "description": "Body content as Markdown; converted to the editor's format."}
		props[OptContentFile] = map[string]any{"type": "string", "description": "Path of a Markdown (.md) or Tiptap JSON (.json) file for the body content."}
		props["content_structured"] = map[string]any{"type": "object", "description": `Body content as a Tiptap document ({"type":"doc","content":[...]}). Prefer content_markdown.`}
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
		s["items"] = map[string]any{"type": "string"}
	}
	if f.Format != "" {
		s["format"] = f.Format
	}
	desc := f.Description
	if f.DraftsOnly {
		desc += " Only with drafts=true."
	}
	if f.PublishedOnly {
		desc += " Only without drafts."
	}
	if desc != "" {
		s["description"] = desc
	}
	if f.Default != nil {
		s["default"] = f.Default
	}
	return s
}

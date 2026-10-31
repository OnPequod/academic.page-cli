// Package spec reads the embedded OpenAPI document for the Academic.page API.
//
// The CLI builds its commands, its schema output and its MCP tools from this
// document, so refreshing openapi.json (make spec) is how the CLI learns about
// API changes. The tests fail when the document gains an operation the CLI
// does not map.
package spec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed openapi.json
var raw []byte

// Raw returns the embedded OpenAPI document.
func Raw() []byte { return raw }

// Schema is the subset of JSON Schema the API uses.
type Schema struct {
	Ref         string             `json:"$ref,omitempty"`
	Type        any                `json:"type,omitempty"`
	Format      string             `json:"format,omitempty"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Items       json.RawMessage    `json:"items,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	Default     any                `json:"default,omitempty"`
	Enum        []any              `json:"enum,omitempty"`
}

// Types returns the schema's types, with "null" removed and nullable reported
// separately.
func (s *Schema) Types() (types []string, nullable bool) {
	switch t := s.Type.(type) {
	case string:
		types = []string{t}
	case []any:
		for _, v := range t {
			if str, ok := v.(string); ok {
				types = append(types, str)
			}
		}
	}
	out := types[:0]
	for _, t := range types {
		if t == "null" {
			nullable = true
			continue
		}
		out = append(out, t)
	}
	return out, nullable
}

// PrimaryType returns the first non-null type, or "object" when none is given.
func (s *Schema) PrimaryType() string {
	types, _ := s.Types()
	if len(types) == 0 {
		return "object"
	}
	return types[0]
}

// Param is an operation parameter.
type Param struct {
	Ref         string  `json:"$ref,omitempty"`
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

// Operation is one method on one path.
type Operation struct {
	ID          string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Params      []Param
	// Body is the request body schema, resolved, or nil when there is none.
	Body *Schema
	// BodyType is the request body's media type, such as application/json.
	BodyType string
}

type rawOperation struct {
	OperationID string   `json:"operationId"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Parameters  []Param  `json:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Schema *Schema `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

type document struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Parameters map[string]Param   `json:"parameters"`
		Schemas    map[string]*Schema `json:"schemas"`
	} `json:"components"`
}

// Spec is the parsed document.
type Spec struct {
	Version    string
	Operations map[string]*Operation
	Schemas    map[string]*Schema
}

var methods = []string{"get", "post", "put", "patch", "delete"}

// Load parses the embedded document.
func Load() (*Spec, error) { return Parse(raw) }

// Parse parses an OpenAPI document.
func Parse(data []byte) (*Spec, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	s := &Spec{Version: doc.Info.Version, Operations: map[string]*Operation{}, Schemas: doc.Components.Schemas}
	for path, item := range doc.Paths {
		for _, method := range methods {
			body, ok := item[method]
			if !ok {
				continue
			}
			var ro rawOperation
			if err := json.Unmarshal(body, &ro); err != nil {
				return nil, fmt.Errorf("parse %s %s: %w", method, path, err)
			}
			if ro.OperationID == "" {
				return nil, fmt.Errorf("%s %s has no operationId", method, path)
			}
			op := &Operation{
				ID:          ro.OperationID,
				Method:      strings.ToUpper(method),
				Path:        path,
				Summary:     ro.Summary,
				Description: ro.Description,
				Tags:        ro.Tags,
			}
			for _, p := range ro.Parameters {
				if p.Ref != "" {
					name := strings.TrimPrefix(p.Ref, "#/components/parameters/")
					resolved, ok := doc.Components.Parameters[name]
					if !ok {
						return nil, fmt.Errorf("%s: unknown parameter %s", op.ID, p.Ref)
					}
					p = resolved
				}
				if p.Schema == nil {
					p.Schema = &Schema{Type: "string"}
				}
				op.Params = append(op.Params, p)
			}
			if ro.RequestBody != nil {
				for mediaType, c := range ro.RequestBody.Content {
					op.BodyType = mediaType
					op.Body = s.Resolve(c.Schema)
					break
				}
			}
			s.Operations[op.ID] = op
		}
	}
	return s, nil
}

// Resolve follows a $ref to a component schema.
func (s *Spec) Resolve(schema *Schema) *Schema {
	if schema == nil || schema.Ref == "" {
		return schema
	}
	if resolved, ok := s.Schemas[strings.TrimPrefix(schema.Ref, "#/components/schemas/")]; ok {
		return resolved
	}
	return schema
}

// OperationIDs returns every operation ID, sorted.
func (s *Spec) OperationIDs() []string {
	ids := make([]string, 0, len(s.Operations))
	for id := range s.Operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

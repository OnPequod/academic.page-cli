package app

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/OnPequod/academic.page-cli/internal/registry"
)

// CallFromArgs builds a Call from JSON arguments shaped by
// Command.InputSchema, as MCP tools receive them.
func CallFromArgs(cmd *registry.Command, raw json.RawMessage) (Call, error) {
	call := Call{Command: cmd, Query: map[string]string{}, Body: map[string]any{}}
	args := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return call, Usagef("arguments must be a JSON object: %v", err)
		}
	}

	query := map[string]bool{}
	for _, f := range cmd.Query {
		query[f.Name] = true
	}
	for name, v := range args {
		var err error
		switch {
		case name == registry.OptSite:
			call.Site, err = toInt(name, v)
		case cmd.Arg != nil && name == cmd.Arg.Name:
			call.Arg, err = toArg(name, v)
		case cmd.FileArg != nil && name == cmd.FileArg.Name:
			call.File, err = toString(name, v)
		case name == registry.OptDrafts:
			call.Drafts, err = toBool(name, v)
		case name == registry.OptAll:
			call.All, err = toBool(name, v)
		case name == registry.OptDryRun:
			call.DryRun, err = toBool(name, v)
		case name == registry.OptPublish:
			call.Publish, err = toBool(name, v)
		case name == registry.OptContentMarkdown:
			call.ContentMarkdown, err = toString(name, v)
		case name == registry.OptContentFile:
			call.ContentFile, err = toString(name, v)
			if call.ContentFile == "-" {
				err = Usagef("content_file must be a file path")
			}
		case query[name]:
			if v != nil {
				call.Query[name] = fmt.Sprint(v)
				if f, ok := v.(float64); ok {
					call.Query[name] = strconv.FormatFloat(f, 'f', -1, 64)
				}
			}
		default:
			// Body fields; Execute rejects unknown names with the list of
			// valid ones.
			call.Body[name] = v
		}
		if err != nil {
			return call, err
		}
	}
	return call, nil
}

func toBool(name string, v any) (bool, error) {
	b, ok := v.(bool)
	if !ok && v != nil {
		return false, Usagef("%s must be true or false", name)
	}
	return b, nil
}

func toString(name string, v any) (string, error) {
	s, ok := v.(string)
	if !ok && v != nil {
		return "", Usagef("%s must be a string", name)
	}
	return s, nil
}

func toInt(name string, v any) (int64, error) {
	f, ok := v.(float64)
	if !ok || f < 1 || f != float64(int64(f)) {
		return 0, Usagef("%s must be a positive whole number", name)
	}
	return int64(f), nil
}

func toArg(name string, v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case float64:
		if v != float64(int64(v)) {
			return "", Usagef("%s must be a whole number", name)
		}
		return strconv.FormatInt(int64(v), 10), nil
	}
	return "", Usagef("%s must be an ID", name)
}

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

// resourceCommand builds a command from a registry entry.
func (c *CLI) resourceCommand(rc *registry.Command) *cobra.Command {
	use := rc.Name
	var positional []*registry.Field
	for _, p := range rc.Parents {
		use += " <" + p.Name + ">"
		positional = append(positional, p)
	}
	if rc.Arg != nil {
		use += " <" + rc.Arg.Name + ">"
		positional = append(positional, rc.Arg)
	}
	if rc.FileArg != nil {
		use += " <" + rc.FileArg.Name + ">"
		positional = append(positional, rc.FileArg)
	}

	var (
		drafts, all, dryRun, publish, unpublish, yes bool
		data                                         string
		nulls                                        []string
		files                                        = map[string]*string{}
	)

	cmd := &cobra.Command{
		Use:   use,
		Short: rc.Short,
		Long:  longHelp(rc),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != len(positional) {
				names := make([]string, len(positional))
				for i, f := range positional {
					names[i] = "<" + f.Name + ">"
				}
				if len(names) == 0 {
					return app.Usagef("`%s` takes no arguments, got %q", rc.Path(), strings.Join(args, " "))
				}
				return app.Usagef("`%s` needs %s", rc.Path(), strings.Join(names, " "))
			}
			return nil
		},
	}
	flags := cmd.Flags()
	query := addFields(flags, rc.Query)
	body := addFields(flags, rc.Body)

	if rc.DraftsOp != nil {
		usage := "Read through the management API, which includes drafts and unpublished records (needs a management key)"
		if rc.ManageFlag != "drafts" {
			usage = "Read through the management API, which adds edit_version and management fields (needs a management key)"
		}
		flags.BoolVar(&drafts, rc.ManageFlag, false, usage)
	}
	if rc.Paginated {
		flags.BoolVar(&all, "all", false, "Follow every page and return all records")
	}
	flags.BoolVar(&dryRun, "dry-run", false, "Print the request instead of sending it")
	if rc.Kind == registry.KindWrite || rc.Kind == registry.KindUpload {
		flags.StringVar(&data, "data", "", "Body fields as a JSON object, @file.json, or - for stdin; flags override it")
		var nullable []string
		for _, f := range rc.Body {
			if f.Nullable {
				nullable = append(nullable, f.Name)
			}
		}
		if len(nullable) > 0 {
			flags.StringSliceVar(&nulls, "null", nil, "Clear these fields (set them to null): "+strings.Join(nullable, ", "))
		}
	}
	for _, name := range rc.TextFiles {
		files[name] = flags.String(strings.ReplaceAll(registry.FileOption(name), "_", "-"), "", name+" from a file, or - for stdin")
	}
	if rc.Publishable {
		flags.BoolVar(&publish, "publish", false, "Publish now (sets published_at to the current time)")
		if rc.Arg != nil {
			flags.BoolVar(&unpublish, "unpublish", false, "Return to draft (clears published_at)")
		}
	}
	if rc.Kind == registry.KindDelete {
		flags.BoolVar(&yes, "yes", false, "Confirm the delete (required)")
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		call := app.Call{
			Command: rc, Drafts: drafts, All: all, DryRun: dryRun, Publish: publish, Stdin: c.Stdin,
			Query: map[string]string{}, Body: map[string]any{},
			Files: map[string]string{},
		}
		for name, v := range files {
			if *v != "" {
				call.Files[name] = *v
			}
		}
		for i, f := range positional {
			switch f {
			case rc.FileArg:
				call.File = args[i]
			case rc.Arg:
				call.Arg = args[i]
			default:
				call.Parents = append(call.Parents, args[i])
			}
		}
		if rc.Kind == registry.KindDelete && !yes && !dryRun {
			return app.Usagef("deleting is permanent; add --yes to confirm (or --dry-run to preview)")
		}

		if data != "" {
			fields, err := readData(data, c)
			if err != nil {
				return err
			}
			for k, v := range fields {
				call.Body[k] = v
			}
		}
		for _, f := range query {
			if f.changed() {
				call.Query[f.field.Name] = fmt.Sprint(f.value())
			}
		}
		for _, f := range body {
			if f.changed() {
				call.Body[f.field.Name] = f.value()
			}
		}
		for _, name := range nulls {
			call.Body[name] = nil
		}
		if unpublish {
			if publish {
				return app.Usagef("give --publish or --unpublish, not both")
			}
			call.Body["published_at"] = nil
		}

		a, err := c.app()
		if err != nil {
			return err
		}
		res, err := a.Execute(cmd.Context(), call)
		if err != nil {
			return err
		}
		return c.printResult(rc, call, res)
	}
	return cmd
}

func longHelp(rc *registry.Command) string {
	var b strings.Builder
	b.WriteString(rc.Short + ".")
	if desc := rc.Description(); desc != "" {
		b.WriteString("\n\n" + desc)
	}
	fmt.Fprintf(&b, "\n\nAPI: %s %s", rc.Op.Method, rc.Op.Path)
	if rc.DraftsOp != nil {
		fmt.Fprintf(&b, "\nWith --%s: %s %s", rc.ManageFlag, rc.DraftsOp.Method, rc.DraftsOp.Path)
	}
	if rc.Publishable && rc.Arg == nil {
		b.WriteString("\n\nCreates a draft unless you pass --publish or --published-at.")
	}
	var nested []string
	for _, f := range rc.Body {
		if f.ItemSchema != nil {
			nested = append(nested, f.Name+": a list of objects with "+describeProperties(f.ItemSchema))
		}
	}
	if len(nested) > 0 {
		b.WriteString("\n\nOnly through --data (a JSON object of body fields):\n  " + strings.Join(nested, "\n  "))
	}
	return b.String()
}

// describeProperties lists an object schema's properties for help text, with
// their choices, such as "term (required; one of: spring, fall)".
func describeProperties(schema map[string]any) string {
	props, _ := schema["properties"].(map[string]any)
	required := map[string]bool{}
	if req, ok := schema["required"].([]string); ok {
		for _, name := range req {
			required[name] = true
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		var notes []string
		if required[name] {
			notes = append(notes, "required")
		}
		if prop, ok := props[name].(map[string]any); ok {
			if enum, ok := prop["enum"].([]any); ok {
				notes = append(notes, "one of: "+joinValues(enum))
			}
			if max, ok := prop["maxLength"].(int); ok {
				notes = append(notes, fmt.Sprintf("up to %d characters", max))
			}
		}
		parts[i] = name
		if len(notes) > 0 {
			parts[i] += " (" + strings.Join(notes, "; ") + ")"
		}
	}
	return strings.Join(parts, ", ")
}

// joinValues lists enum values, leaving out null.
func joinValues(values []any) string {
	var out []string
	for _, v := range values {
		if v != nil {
			out = append(out, fmt.Sprint(v))
		}
	}
	return strings.Join(out, ", ")
}

type boundFlag struct {
	field registry.Field
	flag  *pflag.Flag
	value func() any
}

func (f boundFlag) changed() bool { return f.flag.Changed }

// addFields adds a flag for each field, typed from the spec.
func addFields(flags *pflag.FlagSet, fields []registry.Field) []boundFlag {
	var out []boundFlag
	for _, field := range fields {
		desc := field.Description
		if desc == "" {
			desc = strings.ReplaceAll(field.Name, "_", " ")
		}
		if field.Format != "" {
			desc += " (" + field.Format + ")"
		}
		if len(field.Enum) > 0 {
			desc += " (one of: " + joinValues(field.Enum) + ")"
		}
		if field.MaxLength != nil {
			desc += fmt.Sprintf(" (up to %d characters)", *field.MaxLength)
		}
		if field.DraftsOnly {
			desc += " [management read only]"
		}
		if field.PublishedOnly {
			desc += " [published read only]"
		}
		var value func() any
		switch field.Type {
		case "integer":
			v := flags.Int64(field.Flag, 0, desc)
			value = func() any { return *v }
		case "boolean":
			v := flags.Bool(field.Flag, false, desc)
			value = func() any { return *v }
		case "array":
			switch field.Items {
			case "object", "array":
				// Lists of objects only come through --data.
				continue
			case "integer":
				v := flags.Int64Slice(field.Flag, nil, desc+" (comma-separated or repeated)")
				value = func() any { return *v }
			default:
				v := flags.StringSlice(field.Flag, nil, desc+" (comma-separated or repeated)")
				value = func() any { return *v }
			}
		case "object":
			// Objects only come through --data.
			continue
		default:
			v := flags.String(field.Flag, "", desc)
			value = func() any { return *v }
		}
		out = append(out, boundFlag{field: field, flag: flags.Lookup(field.Flag), value: value})
	}
	return out
}

func readData(arg string, c *CLI) (map[string]any, error) {
	var raw []byte
	switch {
	case arg == "-":
		var err error
		if raw, err = readAll(c.Stdin); err != nil {
			return nil, app.Usagef("read --data from stdin: %v", err)
		}
	case strings.HasPrefix(arg, "@"):
		var err error
		if raw, err = readFile(arg[1:]); err != nil {
			return nil, app.Usagef("read --data file: %v", err)
		}
	default:
		raw = []byte(arg)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, app.Usagef("--data must be a JSON object: %v", err)
	}
	return fields, nil
}

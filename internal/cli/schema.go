package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/config"
	"github.com/OnPequod/academic.page-cli/internal/mcpserver"
	"github.com/OnPequod/academic.page-cli/internal/registry"
	"github.com/OnPequod/academic.page-cli/internal/spec"
)

type apiRef struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operation_id"`
}

type commandSchema struct {
	Command     string           `json:"command"`
	Summary     string           `json:"summary"`
	Description string           `json:"description,omitempty"`
	Kind        registry.Kind    `json:"kind"`
	API         apiRef           `json:"api"`
	DraftsAPI   *apiRef          `json:"drafts_api,omitempty"`
	MCPTool     string           `json:"mcp_tool"`
	NeedsSite   bool             `json:"needs_site"`
	Args        []registry.Field `json:"args"`
	Flags       []flagSchema     `json:"flags"`
	BodyFields  []registry.Field `json:"body_fields,omitempty"`
	Output      string           `json:"output"`
	InputSchema map[string]any   `json:"input_schema"`
}

type flagSchema struct {
	Flag        string `json:"flag"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

func (c *CLI) schemaCommand() *cobra.Command {
	var openapi bool
	cmd := &cobra.Command{
		Use:   "schema [command...]",
		Short: "Describe every command, argument and field as JSON",
		Long: `Describe every command, argument and field as JSON.

With command words, describe one command:

  academic-page schema posts create

--openapi prints the OpenAPI document the CLI was built from.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if openapi {
				_, err := c.Stdout.Write(spec.Raw())
				return err
			}
			root := cmd.Root()
			var commands []commandSchema
			for _, rc := range c.Registry.Commands {
				commands = append(commands, describeCommand(root, rc))
			}
			if len(args) > 0 {
				want := strings.Join(args, " ")
				for _, cs := range commands {
					if cs.Command == want {
						return c.printJSON(cs)
					}
				}
				return app.Usagef("no command %q; run `academic-page schema` to list them", want)
			}
			return c.printJSON(map[string]any{
				"name":         "academic-page",
				"version":      c.Build.Version,
				"api_spec":     c.Registry.Spec.Version,
				"api_base_url": config.DefaultBaseURL,
				"global_flags": globalFlags(root),
				"environment": map[string]string{
					config.EnvToken:   "API key; overrides the profile's key",
					config.EnvSite:    "Default site ID",
					config.EnvProfile: "Profile name",
					config.EnvBaseURL: "API base URL",
					config.EnvConfig:  "Config file path",
				},
				"exit_codes": app.ExitCodes,
				"errors":     "With --json, errors are printed to stderr as {\"error\": {code, message, status, fields, hint, exit_code}}.",
				"notes": []string{
					"Reads return published content; --drafts on posts, pages, projects and media reads includes drafts (management key).",
					"Writes exist for posts, pages, projects and media. Creates are drafts unless --publish.",
					"Rich text (content, abstract, bio, and so on) is written in Markdown with --<field>-markdown or --<field>-file. The editor's Tiptap JSON is accepted through --data for exact control.",
					"Every command accepts --dry-run, which prints the request without sending it.",
					"Other commands: auth login|logout|status|profiles|use, sites use, mcp, version.",
				},
				"commands":    commands,
				"not_covered": registry.Excluded,
			})
		},
	}
	cmd.Flags().BoolVar(&openapi, "openapi", false, "Print the embedded OpenAPI document")
	return cmd
}

func describeCommand(root *cobra.Command, rc *registry.Command) commandSchema {
	cs := commandSchema{
		Command:     rc.Path(),
		Summary:     rc.Short,
		Description: rc.Op.Description,
		Kind:        rc.Kind,
		API:         apiRef{rc.Op.Method, rc.Op.Path, rc.Op.ID},
		MCPTool:     rc.ToolName(),
		NeedsSite:   rc.NeedsSite(),
		Args:        []registry.Field{},
		Flags:       []flagSchema{},
		BodyFields:  rc.Body,
		InputSchema: mcpserver.Tool(rc).InputSchema.(map[string]any),
		Output:      outputDescription(rc),
	}
	if rc.DraftsOp != nil {
		cs.DraftsAPI = &apiRef{rc.DraftsOp.Method, rc.DraftsOp.Path, rc.DraftsOp.ID}
	}
	for _, f := range append(append([]*registry.Field{}, rc.Parents...), rc.Arg, rc.FileArg) {
		if f != nil {
			cs.Args = append(cs.Args, *f)
		}
	}
	if cmd, _, err := root.Find(strings.Fields(rc.Path())); err == nil {
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			cs.Flags = append(cs.Flags, flagSchema{Flag: "--" + f.Name, Type: f.Value.Type(), Description: f.Usage})
		})
	}
	return cs
}

func outputDescription(rc *registry.Command) string {
	switch {
	case rc.Text:
		return "text (BibTeX); with --json, {\"text\": ...}"
	case rc.Kind == registry.KindDelete:
		return "{\"deleted\": true, \"id\": ...} or the API's response"
	case rc.Paginated:
		return "{data: [...], links, meta}; with --all, {data: [...], meta: {total}}"
	}
	return "{data: {...}}"
}

func globalFlags(root *cobra.Command) []flagSchema {
	var out []flagSchema
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		out = append(out, flagSchema{Flag: "--" + f.Name, Type: f.Value.Type(), Description: f.Usage})
	})
	return out
}

func (c *CLI) mcpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve every command as an MCP tool over stdio",
		Long: fmt.Sprintf(`Serve every command as an MCP tool over stdio.

Uses the active profile's key and site (or %s and %s).
To add it to Claude Code:

  claude mcp add academic-page -- academic-page mcp

Pass --profile or --site to pin the server to one account or site.`, config.EnvToken, config.EnvSite),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := c.app()
			if err != nil {
				return err
			}
			return mcpserver.Run(cmd.Context(), a, c.Build.Version)
		},
	}
}

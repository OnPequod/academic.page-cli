// Package mcpserver exposes every registry command as an MCP tool over stdio.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

const instructions = `Tools for reading and editing an Academic.page academic website through its public API.

- Reads (*_list, *_get, sites_*, profile_get, publications_bibtex) return published content. Pass drafts=true on posts, pages, projects and media reads to include drafts (needs a management key).
- Writes exist for posts, pages, projects and media only. Creates are drafts unless publish=true; only publish when the user asks.
- Write body content as Markdown in content_markdown.
- Every write accepts dry_run=true to preview the request.
- List tools are paginated; pass all=true to fetch every page.
- site defaults to the configured site; sites_list shows the IDs the key can read.
- Errors come back as JSON with code, message, hint and, for validation errors, fields.`

// New builds the server.
func New(a *app.App, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "academic-page", Title: "Academic.page", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	for _, cmd := range a.Registry.Commands {
		server.AddTool(Tool(cmd), handler(a, cmd))
	}
	return server
}

// Run serves over stdin and stdout until the client disconnects.
func Run(ctx context.Context, a *app.App, version string) error {
	return New(a, version).Run(ctx, &mcp.StdioTransport{})
}

// Tool describes a command as an MCP tool.
func Tool(cmd *registry.Command) *mcp.Tool {
	read := cmd.Kind == registry.KindRead
	destructive := cmd.Kind == registry.KindDelete || cmd.Kind == registry.KindWrite
	openWorld := true
	desc := cmd.Short + ". API: " + cmd.Op.Method + " " + cmd.Op.Path + "."
	if cmd.Op.Description != "" {
		desc += " " + cmd.Op.Description
	}
	return &mcp.Tool{
		Name:        cmd.ToolName(),
		Description: desc,
		InputSchema: cmd.InputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           cmd.Short,
			ReadOnlyHint:    read,
			DestructiveHint: &destructive,
			IdempotentHint:  read || cmd.Kind == registry.KindDelete || cmd.Name == "update",
			OpenWorldHint:   &openWorld,
		},
	}
}

func handler(a *app.App, cmd *registry.Command) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		call, err := app.CallFromArgs(cmd, req.Params.Arguments)
		if err != nil {
			return errorResult(err), nil
		}
		res, err := a.Execute(ctx, call)
		if err != nil {
			return errorResult(err), nil
		}
		switch {
		case res.Plan != nil:
			return jsonResult(res.Plan), nil
		case res.Text != "" || res.JSON == nil:
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: res.Text}}}, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(res.JSON)}}}, nil
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, _ := json.Marshal(v)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}

func errorResult(err error) *mcp.CallToolResult {
	res := jsonResult(map[string]any{"error": app.Describe(err)})
	res.IsError = true
	return res
}

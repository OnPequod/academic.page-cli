package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/config"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

func connect(t *testing.T, a *app.App) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := New(a, "test").Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestToolsMatchCommands(t *testing.T) {
	reg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	session := connect(t, app.New(reg, config.Settings{BaseURL: "http://unused"}, "test"))
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(reg.Commands) {
		t.Errorf("%d tools for %d commands", len(tools.Tools), len(reg.Commands))
	}
	for _, tool := range tools.Tools {
		if tool.Name == "posts_delete" && (tool.Annotations == nil || tool.Annotations.ReadOnlyHint || !*tool.Annotations.DestructiveHint) {
			t.Errorf("posts_delete annotations = %+v", tool.Annotations)
		}
	}
}

func TestCallTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/content/sites/7/posts/3" {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not found.","code":"not_found"}`)
			return
		}
		fmt.Fprint(w, `{"data":{"id":3,"title":"Hi"}}`)
	}))
	defer srv.Close()
	reg, _ := registry.Load()
	session := connect(t, app.New(reg, config.Settings{BaseURL: srv.URL, Token: "k", Site: 7}, "test"))
	ctx := context.Background()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "posts_get", Arguments: map[string]any{"id": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, `"title":"Hi"`) {
		t.Errorf("result = %+v", res.Content[0])
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "posts_get", Arguments: map[string]any{"id": 4}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, `"exit_code":5`) {
		t.Errorf("error result = %+v", res.Content[0])
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "posts_create", Arguments: map[string]any{"title": "T", "content_markdown": "*hi*", "dry_run": true}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError || !strings.Contains(text, `"dry_run":true`) || !strings.Contains(text, `"published_at":null`) {
		t.Errorf("dry run = %s", text)
	}
}

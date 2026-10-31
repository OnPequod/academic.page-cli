// Package cli is the Cobra command tree.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/config"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

// BuildInfo is set by the linker through main.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// CLI holds global flags and shared state.
type CLI struct {
	Build    BuildInfo
	Registry *registry.Registry
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	// StdinIsTerminal reports whether prompts can be shown.
	StdinIsTerminal func() bool
	// NewApp builds the App; tests replace it to point at a fake server.
	NewApp func(config.Settings) *app.App

	profile string
	site    int64
	baseURL string
	json    bool
}

// Run runs the CLI and returns the exit code.
func Run(ctx context.Context, build BuildInfo, args []string) int {
	reg, err := registry.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "academic-page: built with a broken API spec:", err)
		return app.ExitError
	}
	c := &CLI{
		Build:           build,
		Registry:        reg,
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		StdinIsTerminal: stdinIsTerminal,
	}
	return c.Run(ctx, args)
}

// Run runs the CLI with this CLI's streams.
func (c *CLI) Run(ctx context.Context, args []string) int {
	root := c.Root()
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return app.ExitOK
	}
	if isCobraUsage(err) {
		err = &app.UsageError{Message: err.Error()}
	}
	c.printError(err)
	return app.ExitCode(err)
}

func (c *CLI) userAgent() string { return "academic-page-cli/" + c.Build.Version }

// Root builds the command tree.
func (c *CLI) Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "academic-page",
		Short: "Manage an Academic.page site from the command line",
		Long: `academic-page reads and edits an Academic.page site through the public API.

Every command accepts --json for machine-readable output, and
"academic-page schema" describes every command, argument and field.
Writes create drafts unless you pass --publish.

Get an API key at https://academic.page/admin/account/api (Pro plan),
then run "academic-page auth login".`,
		Version:       c.Build.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(c.Stdin)
	root.SetOut(c.Stdout)
	root.SetErr(c.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &app.UsageError{Message: err.Error()}
	})
	root.SetVersionTemplate("academic-page {{.Version}}\n")

	pf := root.PersistentFlags()
	pf.StringVar(&c.profile, "profile", "", "Profile to use (env "+config.EnvProfile+")")
	pf.Int64Var(&c.site, "site", 0, "Site ID (env "+config.EnvSite+"; default from the profile)")
	pf.StringVar(&c.baseURL, "base-url", "", "API base URL (env "+config.EnvBaseURL+")")
	pf.BoolVar(&c.json, "json", false, "Print JSON")
	_ = pf.MarkHidden("base-url")

	root.AddGroup(
		&cobra.Group{ID: "content", Title: "Site content:"},
		&cobra.Group{ID: "setup", Title: "Setup and tools:"},
	)
	for _, g := range registry.Groups {
		group := &cobra.Command{Use: g.Name, Short: g.Short, GroupID: "content", Args: noArgs, RunE: showHelp}
		for _, cmd := range c.Registry.Commands {
			if cmd.Group == g.Name {
				group.AddCommand(c.resourceCommand(cmd))
			}
		}
		if g.Name == "sites" {
			group.AddCommand(c.sitesUseCommand())
		}
		root.AddCommand(group)
	}
	for _, cmd := range []*cobra.Command{c.authCommand(), c.schemaCommand(), c.mcpCommand(), c.versionCommand()} {
		cmd.GroupID = "setup"
		root.AddCommand(cmd)
	}
	root.SetHelpCommandGroupID("setup")
	root.SetCompletionCommandGroupID("setup")
	return root
}

// settings resolves flags, environment and the config file.
func (c *CLI) settings() (*config.Config, config.Settings, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Settings{}, err
	}
	s, err := cfg.Resolve(c.profile, c.baseURL, c.site)
	if err != nil {
		return nil, s, &app.UsageError{Message: err.Error()}
	}
	return cfg, s, nil
}

func (c *CLI) app() (*app.App, error) {
	_, s, err := c.settings()
	if err != nil {
		return nil, err
	}
	if c.NewApp != nil {
		return c.NewApp(s), nil
	}
	return app.New(c.Registry, s, c.userAgent()), nil
}

func (c *CLI) printError(err error) {
	body := app.Describe(err)
	if c.json {
		enc := json.NewEncoder(c.Stderr)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]any{"error": body})
		return
	}
	fmt.Fprintln(c.Stderr, "Error:", body.Message)
	fields := make([]string, 0, len(body.Fields))
	for field := range body.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		fmt.Fprintf(c.Stderr, "  %s: %s\n", field, strings.Join(body.Fields[field], " "))
	}
	details := make([]string, 0, len(body.Details))
	for k := range body.Details {
		details = append(details, k)
	}
	sort.Strings(details)
	for _, k := range details {
		var compact bytes.Buffer
		if json.Compact(&compact, body.Details[k]) != nil {
			compact.Write(body.Details[k])
		}
		fmt.Fprintf(c.Stderr, "  %s: %s\n", k, compact.String())
	}
	if body.Hint != "" {
		fmt.Fprintln(c.Stderr, body.Hint)
	}
}

func (c *CLI) printJSON(v any) error {
	enc := json.NewEncoder(c.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return &app.UsageError{Message: fmt.Sprintf("unknown command %q for %q; run `%s --help`", args[0], cmd.CommandPath(), cmd.CommandPath())}
	}
	return nil
}

// showHelp makes group commands runnable, so that unknown subcommands reach
// noArgs and fail instead of printing help and exiting 0.
func showHelp(cmd *cobra.Command, _ []string) error { return cmd.Help() }

func isCobraUsage(err error) bool {
	var usage *app.UsageError
	if errors.As(err, &usage) {
		return false
	}
	msg := err.Error()
	for _, prefix := range []string{"unknown command", "unknown flag", "unknown shorthand", "accepts ", "requires ", "invalid argument", "flag needs"} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

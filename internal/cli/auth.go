package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/OnPequod/academic.page-cli/internal/api"
	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/config"
)

const keysURL = "https://academic.page/admin/account/api"

func (c *CLI) authCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Store and check API keys", Args: noArgs, RunE: showHelp}
	cmd.AddCommand(c.loginCommand(), c.logoutCommand(), c.statusCommand(), c.profilesCommand(), c.useProfileCommand())
	return cmd
}

func (c *CLI) loginCommand() *cobra.Command {
	var tokenStdin, noVerify bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save an API key to a profile",
		Long: `Save an API key to a profile.

Create a key at ` + keysURL + ` (Pro plan). Content keys read
published content; management keys can also read drafts and write.

The key is read from a hidden prompt, or from stdin with --token-stdin:

  academic-page auth login
  pbcopy-or-similar | academic-page auth login --token-stdin --profile work

The key is checked against the API before it is saved, and is never printed.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, s, err := c.settings()
			if err != nil {
				return err
			}
			token, err := c.readToken(tokenStdin)
			if err != nil {
				return err
			}

			var sites []siteSummary
			if !noVerify {
				a := app.New(c.Registry, config.Settings{BaseURL: s.BaseURL, Token: token}, c.userAgent())
				if c.NewApp != nil {
					a = c.NewApp(config.Settings{BaseURL: s.BaseURL, Token: token})
				}
				if sites, err = listSites(cmd, a.Client); err != nil {
					return err
				}
			}

			p := cfg.Profile(s.Profile)
			p.Token = token
			if c.baseURL != "" {
				p.BaseURL = c.baseURL
			}
			if c.site != 0 {
				p.Site = c.site
			} else if p.Site == 0 && len(sites) == 1 {
				p.Site = sites[0].ID
			}
			if cfg.CurrentProfile == "" {
				cfg.CurrentProfile = s.Profile
			}
			if err := cfg.Save(); err != nil {
				return err
			}

			if c.json {
				return c.printJSON(map[string]any{"profile": s.Profile, "site": p.Site, "sites": sites, "verified": !noVerify, "config": cfg.File()})
			}
			fmt.Fprintf(c.Stdout, "Saved the key to profile %q in %s.\n", s.Profile, cfg.File())
			if !noVerify {
				fmt.Fprintf(c.Stdout, "The key can read %d site(s).\n", len(sites))
			}
			switch {
			case p.Site != 0:
				fmt.Fprintf(c.Stdout, "Default site: %d.\n", p.Site)
			case len(sites) > 1:
				fmt.Fprintln(c.Stdout, "Choose a default site with `academic-page sites use <id>`; `academic-page sites list` shows the IDs.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read the key from stdin")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "Save without checking the key against the API")
	return cmd
}

func (c *CLI) readToken(fromStdin bool) (string, error) {
	var token string
	switch {
	case fromStdin || !c.StdinIsTerminal():
		line, err := bufio.NewReader(c.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", app.Usagef("read key from stdin: %v", err)
		}
		token = line
	default:
		fmt.Fprintf(c.Stderr, "Paste your API key from %s (input is hidden): ", keysURL)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(c.Stderr)
		if err != nil {
			return "", app.Usagef("read key: %v", err)
		}
		token = string(b)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", app.Usagef("no key given; create one at %s", keysURL)
	}
	return token, nil
}

type siteSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
}

func listSites(cmd *cobra.Command, client *api.Client) ([]siteSummary, error) {
	raw, err := client.All(cmd.Context(), api.Request{Method: "GET", Path: "/content/sites", Query: url.Values{}})
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []siteSummary `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parse site list: %w", err)
	}
	return body.Data, nil
}

func (c *CLI) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored key from a profile",
		Long:  "Remove the stored key from a profile. This does not revoke the key; revoke it at " + keysURL + ".",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, s, err := c.settings()
			if err != nil {
				return err
			}
			p, ok := cfg.Profiles[s.Profile]
			removed := ok && p.Token != ""
			if removed {
				p.Token = ""
				if err := cfg.Save(); err != nil {
					return err
				}
			}
			if c.json {
				return c.printJSON(map[string]any{"profile": s.Profile, "removed": removed})
			}
			if removed {
				fmt.Fprintf(c.Stdout, "Removed the key from profile %q. Revoke it at %s if it is no longer needed.\n", s.Profile, keysURL)
			} else {
				fmt.Fprintf(c.Stdout, "Profile %q has no stored key.\n", s.Profile)
			}
			if os.Getenv(config.EnvToken) != "" {
				fmt.Fprintf(c.Stdout, "%s is still set in the environment.\n", config.EnvToken)
			}
			return nil
		},
	}
}

func (c *CLI) statusCommand() *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the active profile and check its key",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, s, err := c.settings()
			if err != nil {
				return err
			}
			status := map[string]any{
				"profile":   s.Profile,
				"config":    cfg.File(),
				"base_url":  s.BaseURL,
				"key_set":   s.Token != "",
				"key_from":  s.TokenSource,
				"site":      s.Site,
				"site_from": s.SiteSource,
			}
			var checkErr error
			if s.Token != "" && !offline {
				a, err := c.app()
				if err != nil {
					return err
				}
				sites, err := listSites(cmd, a.Client)
				status["key_valid"] = err == nil
				if err == nil {
					status["sites"] = sites
				} else {
					checkErr = err
					status["error"] = app.Describe(err)
				}
			}
			if c.json {
				if err := c.printJSON(status); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(c.Stdout, "Profile:  %s (%s)\n", s.Profile, cfg.File())
				fmt.Fprintf(c.Stdout, "Base URL: %s\n", s.BaseURL)
				if s.Token == "" {
					fmt.Fprintln(c.Stdout, "Key:      none; run `academic-page auth login`")
				} else {
					fmt.Fprintf(c.Stdout, "Key:      set (from %s)\n", s.TokenSource)
				}
				if s.Site != 0 {
					fmt.Fprintf(c.Stdout, "Site:     %d (from %s)\n", s.Site, s.SiteSource)
				} else {
					fmt.Fprintln(c.Stdout, "Site:     none; run `academic-page sites use <id>`")
				}
				if sites, ok := status["sites"].([]siteSummary); ok {
					fmt.Fprintf(c.Stdout, "Check:    OK, the key can read %d site(s)\n", len(sites))
				}
			}
			if s.Token == "" {
				return &api.Error{Status: 401, Code: "missing_api_token", Message: "No API key is configured."}
			}
			return checkErr
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "Do not check the key against the API")
	return cmd
}

func (c *CLI) profilesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List stored profiles",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, s, err := c.settings()
			if err != nil {
				return err
			}
			type row struct {
				Name    string `json:"name"`
				Active  bool   `json:"active"`
				KeySet  bool   `json:"key_set"`
				Site    int64  `json:"site,omitempty"`
				BaseURL string `json:"base_url,omitempty"`
			}
			rows := []row{}
			for _, name := range cfg.ProfileNames() {
				p := cfg.Profiles[name]
				rows = append(rows, row{Name: name, Active: name == s.Profile, KeySet: p.Token != "", Site: p.Site, BaseURL: p.BaseURL})
			}
			if c.json {
				return c.printJSON(map[string]any{"data": rows})
			}
			if len(rows) == 0 {
				fmt.Fprintln(c.Stdout, "No profiles yet; run `academic-page auth login`.")
			}
			for _, r := range rows {
				mark := " "
				if r.Active {
					mark = "*"
				}
				key := "no key"
				if r.KeySet {
					key = "key set"
				}
				fmt.Fprintf(c.Stdout, "%s %s  (%s, site %d)\n", mark, r.Name, key, r.Site)
			}
			return nil
		},
	}
}

func (c *CLI) useProfileCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Make a profile the default",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return app.Usagef("`auth use` needs a profile name; `academic-page auth profiles` lists them")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[args[0]]; !ok {
				return app.Usagef("no profile %q; create it with `academic-page auth login --profile %s`", args[0], args[0])
			}
			cfg.CurrentProfile = args[0]
			if err := cfg.Save(); err != nil {
				return err
			}
			if c.json {
				return c.printJSON(map[string]any{"profile": args[0]})
			}
			fmt.Fprintf(c.Stdout, "Now using profile %q.\n", args[0])
			return nil
		},
	}
}

func (c *CLI) sitesUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <site>",
		Short: "Set the profile's default site",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return app.Usagef("`sites use` needs a site ID; `academic-page sites list` shows them")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var site int64
			if _, err := fmt.Sscan(args[0], &site); err != nil || site < 1 {
				return app.Usagef("site must be a numeric ID, got %q", args[0])
			}
			cfg, s, err := c.settings()
			if err != nil {
				return err
			}
			cfg.Profile(s.Profile).Site = site
			if cfg.CurrentProfile == "" {
				cfg.CurrentProfile = s.Profile
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			if c.json {
				return c.printJSON(map[string]any{"profile": s.Profile, "site": site})
			}
			fmt.Fprintf(c.Stdout, "Profile %q now uses site %d.\n", s.Profile, site)
			return nil
		},
	}
}

func (c *CLI) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if c.json {
				return c.printJSON(map[string]string{"version": c.Build.Version, "commit": c.Build.Commit, "date": c.Build.Date, "api_spec": c.Registry.Spec.Version})
			}
			fmt.Fprintf(c.Stdout, "academic-page %s (commit %s, built %s, API spec %s)\n", c.Build.Version, c.Build.Commit, c.Build.Date, c.Registry.Spec.Version)
			return nil
		},
	}
}

func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

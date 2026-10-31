// Package config stores profiles: an API base URL, a key and a default site.
//
// The file lives in the OS config directory (academic-page/config.json) with
// mode 0600. Environment variables override it, so agents and CI can run
// without a file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// DefaultBaseURL is the production API.
const DefaultBaseURL = "https://academic.page/api/v1"

// DefaultProfile is used when no profile is chosen.
const DefaultProfile = "default"

// Environment variables.
const (
	EnvConfig  = "ACADEMIC_PAGE_CONFIG"
	EnvProfile = "ACADEMIC_PAGE_PROFILE"
	EnvToken   = "ACADEMIC_PAGE_TOKEN"
	EnvSite    = "ACADEMIC_PAGE_SITE"
	EnvBaseURL = "ACADEMIC_PAGE_BASE_URL"
)

// Profile is one stored account.
type Profile struct {
	BaseURL string `json:"base_url,omitempty"`
	Token   string `json:"token,omitempty"`
	Site    int64  `json:"site,omitempty"`
}

// Config is the whole file.
type Config struct {
	CurrentProfile string              `json:"current_profile,omitempty"`
	Profiles       map[string]*Profile `json:"profiles"`

	path string
}

// Path returns the config file's location.
func Path() (string, error) {
	if p := os.Getenv(EnvConfig); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(dir, "academic-page", "config.json"), nil
}

// Load reads the config file, returning an empty config when there is none.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	c := &Config{Profiles: map[string]*Profile{}, path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w (fix or delete the file)", path, err)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	return c, nil
}

// File returns where the config was loaded from.
func (c *Config) File() string { return c.path }

// Save writes the file atomically with mode 0600.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// ProfileName picks the profile: the flag, then the environment, then the
// file's current profile, then "default".
func (c *Config) ProfileName(flag string) string {
	switch {
	case flag != "":
		return flag
	case os.Getenv(EnvProfile) != "":
		return os.Getenv(EnvProfile)
	case c.CurrentProfile != "":
		return c.CurrentProfile
	}
	return DefaultProfile
}

// Profile returns the named profile, creating it if needed.
func (c *Config) Profile(name string) *Profile {
	p, ok := c.Profiles[name]
	if !ok {
		p = &Profile{}
		c.Profiles[name] = p
	}
	return p
}

// ProfileNames returns the stored profile names, sorted.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Settings are the values a command runs with, after flags and environment.
type Settings struct {
	Profile     string
	BaseURL     string
	Token       string
	TokenSource string
	Site        int64
	SiteSource  string
}

// Resolve combines flags, environment and the profile. Flags win, then the
// environment, then the file.
func (c *Config) Resolve(profileFlag, baseURLFlag string, siteFlag int64) (Settings, error) {
	name := c.ProfileName(profileFlag)
	p := c.Profiles[name]
	if p == nil {
		p = &Profile{}
	}
	s := Settings{Profile: name, BaseURL: DefaultBaseURL}

	switch {
	case baseURLFlag != "":
		s.BaseURL = baseURLFlag
	case os.Getenv(EnvBaseURL) != "":
		s.BaseURL = os.Getenv(EnvBaseURL)
	case p.BaseURL != "":
		s.BaseURL = p.BaseURL
	}

	switch {
	case os.Getenv(EnvToken) != "":
		s.Token, s.TokenSource = os.Getenv(EnvToken), EnvToken
	case p.Token != "":
		s.Token, s.TokenSource = p.Token, "profile "+name
	}

	switch {
	case siteFlag != 0:
		s.Site, s.SiteSource = siteFlag, "--site"
	case os.Getenv(EnvSite) != "":
		var site int64
		if _, err := fmt.Sscan(os.Getenv(EnvSite), &site); err != nil || site < 1 {
			return s, fmt.Errorf("%s must be a site ID (a positive number), got %q", EnvSite, os.Getenv(EnvSite))
		}
		s.Site, s.SiteSource = site, EnvSite
	case p.Site != 0:
		s.Site, s.SiteSource = p.Site, "profile "+name
	}
	return s, nil
}

// Package config loads MCP server configuration from environment variables.
package config

import (
	"errors"
	"os"
)

// Config holds runtime settings.
type Config struct {
	APIBaseURL string // CODESCENE_URL (default https://api.codescene.io)
	APIToken   string // CODESCENE_TOKEN
	ProjectID  string // CODESCENE_PROJECT_ID
	CSCLIPath  string // CS_CLI_PATH (default "cs")
	LogLevel   string // LOG_LEVEL (default "info")

	CodecovBaseURL string // CODECOV_URL (default https://api.codecov.io)
	CodecovToken   string // CODECOV_TOKEN
	CodecovSlug    string // CODECOV_REPO  (e.g. "github/nellcorp/codehealth")
}

// FromEnv reads the configuration from process environment.
func FromEnv() *Config {
	return &Config{
		APIBaseURL: getenv("CODESCENE_URL", "https://api.codescene.io"),
		APIToken:   os.Getenv("CODESCENE_TOKEN"),
		ProjectID:  os.Getenv("CODESCENE_PROJECT_ID"),
		CSCLIPath:  getenv("CS_CLI_PATH", "cs"),
		LogLevel:   getenv("LOG_LEVEL", "info"),

		CodecovBaseURL: getenv("CODECOV_URL", "https://api.codecov.io"),
		CodecovToken:   os.Getenv("CODECOV_TOKEN"),
		CodecovSlug:    os.Getenv("CODECOV_REPO"),
	}
}

// ErrAPINotConfigured is returned when CodeScene API tools are invoked without credentials.
var ErrAPINotConfigured = errors.New("codescene: CODESCENE_TOKEN and CODESCENE_PROJECT_ID required for API tools")

// ErrCoverageNotConfigured is returned when Codecov tools are invoked without credentials.
var ErrCoverageNotConfigured = errors.New("codecov: CODECOV_TOKEN and CODECOV_REPO required for coverage tools (CODECOV_REPO format: service/owner/repo)")

// APIReady reports whether CodeScene API tools can run.
func (c *Config) APIReady() error {
	if c.APIToken == "" || c.ProjectID == "" {
		return ErrAPINotConfigured
	}
	return nil
}

// CoverageReady reports whether Codecov tools can run.
func (c *Config) CoverageReady() error {
	if c.CodecovToken == "" || c.CodecovSlug == "" {
		return ErrCoverageNotConfigured
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

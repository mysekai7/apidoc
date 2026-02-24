package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/internal/store"
)

const defaultConfigContent = `llm:
  provider: "openai"
  api_key: ""
  base_url: "https://api.openai.com/v1"
  model: "gpt-4o"
  max_tokens: 4096
  temperature: 0.2

output:
  dir: "./output"
  formats:
    - markdown
    - openapi

filter:
  ignore_extensions:
    - .js
    - .css
    - .png
    - .jpg
    - .gif
    - .svg
    - .woff
    - .woff2
    - .ico
    - .map
  ignore_content_types:
    - text/html
    - text/css
    - image/*
    - font/*
    - application/javascript
  ignore_paths:
    - /static/
    - /assets/
    - /favicon

sanitize:
  headers:
    - Authorization
    - Cookie
    - Set-Cookie
    - X-Api-Key
    - X-Auth-Token
  body_fields:
    - password
    - secret
    - token
    - api_key
    - access_token
    - refresh_token
    - credential
  replacement: "***REDACTED***"

server:
  host: "127.0.0.1"
  port: 3000
  cors_extension_id: ""

log:
  level: "info"
`

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var cfgPath string
	var verbose bool
	var debug bool

	root := &cobra.Command{
		Use:   "apidoc",
		Short: "API Doc Assistant CLI",
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path")
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose output")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug output")
	_ = verbose
	_ = debug

	root.AddCommand(newInitCmd())
	root.AddCommand(newGenerateCmd(&cfgPath))
	root.AddCommand(newImportCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newDeleteCmd())

	return root
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.apidoc directory and default config",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			baseDir := filepath.Join(home, ".apidoc")
			if err := os.MkdirAll(baseDir, 0o755); err != nil {
				return err
			}

			cfgFile := filepath.Join(baseDir, "config.yaml")
			if _, err := os.Stat(cfgFile); errors.Is(err, os.ErrNotExist) {
				if err := os.WriteFile(cfgFile, []byte(defaultConfigContent), 0o644); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "created", cfgFile)
			} else if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "exists", cfgFile)
			} else {
				return err
			}

			dbPath := filepath.Join(baseDir, "apidoc.db")
			s, err := store.NewSQLiteStore(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()
			fmt.Fprintln(cmd.OutOrStdout(), "database ready", dbPath)
			fmt.Fprintln(cmd.OutOrStdout(), "please update llm.api_key in", cfgFile)
			return nil
		},
	}
}

func newGenerateCmd(cfgPath *string) *cobra.Command {
	var harPath, scenario string
	var noCache, resume bool
	cmd := &cobra.Command{Use: "generate", Short: "Generate docs from HAR", RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		_ = harPath
		_ = scenario
		_ = noCache
		_ = resume
		return cfg.ValidateGenerate()
	}}
	cmd.Flags().StringVar(&harPath, "har", "", "HAR file path")
	cmd.Flags().StringVar(&scenario, "scenario", "", "scenario description")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable cache")
	cmd.Flags().BoolVar(&resume, "resume", false, "resume from failed batches")
	_ = cmd.MarkFlagRequired("har")
	return cmd
}

func newImportCmd() *cobra.Command {
	var harPath, scenario string
	cmd := &cobra.Command{Use: "import", Short: "Import HAR into database", RunE: func(cmd *cobra.Command, args []string) error {
		_ = harPath
		_ = scenario
		return nil
	}}
	cmd.Flags().StringVar(&harPath, "har", "", "HAR file path")
	cmd.Flags().StringVar(&scenario, "scenario", "", "scenario description")
	_ = cmd.MarkFlagRequired("har")
	return cmd
}

func newServeCmd() *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{Use: "serve", Short: "Start HTTP service", RunE: func(cmd *cobra.Command, args []string) error {
		_ = host
		_ = port
		return nil
	}}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "server host")
	cmd.Flags().IntVar(&port, "port", 3000, "server port")
	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List all sessions", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}

func newShowCmd() *cobra.Command {
	var session string
	var version int
	cmd := &cobra.Command{Use: "show", Short: "Show session details", RunE: func(cmd *cobra.Command, args []string) error {
		_ = session
		_ = version
		return nil
	}}
	cmd.Flags().StringVar(&session, "session", "", "session id")
	cmd.Flags().IntVar(&version, "version", 0, "version number")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var session string
	cmd := &cobra.Command{Use: "delete", Short: "Delete session", RunE: func(cmd *cobra.Command, args []string) error {
		_ = session
		return nil
	}}
	cmd.Flags().StringVar(&session, "session", "", "session id")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

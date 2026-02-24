package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/yourorg/apidoc/internal/config"
	"github.com/yourorg/apidoc/internal/generator"
	"github.com/yourorg/apidoc/internal/har"
	"github.com/yourorg/apidoc/internal/server"
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
		Short: "API Doc Assistant — generate API docs from real traffic",
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path")
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose output")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "debug output")

	root.AddCommand(newInitCmd())
	root.AddCommand(newGenerateCmd(&cfgPath, &verbose))
	root.AddCommand(newImportCmd(&cfgPath))
	root.AddCommand(newServeCmd(&cfgPath))
	root.AddCommand(newListCmd(&cfgPath))
	root.AddCommand(newShowCmd(&cfgPath))
	root.AddCommand(newDeleteCmd(&cfgPath))

	return root
}

func openStore(cfgPath string) (*config.Config, *store.SQLiteStore, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	dbPath := filepath.Join(home, ".apidoc", "apidoc.db")
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, s, nil
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
			fmt.Fprintln(cmd.OutOrStdout(), "please set llm.api_key in", cfgFile)
			return nil
		},
	}
}

func newGenerateCmd(cfgPath *string, verbose *bool) *cobra.Command {
	var harPath, scenario string
	var noCache, resume bool

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate API docs from HAR file",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, s, err := openStore(*cfgPath)
			if err != nil {
				return err
			}
			defer s.Close()

			if err := cfg.ValidateGenerate(); err != nil {
				return err
			}

			// Parse HAR
			fmt.Fprintf(cmd.OutOrStdout(), "parsing %s...\n", harPath)
			logs, err := har.Parse(harPath)
			if err != nil {
				return fmt.Errorf("parse HAR: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "found %d requests\n", len(logs))

			// Determine host from first log
			host := "unknown"
			if len(logs) > 0 && logs[0].Host != "" {
				host = logs[0].Host
			}

			// Create session and save logs
			sess, err := s.CreateSession("har", scenario, host)
			if err != nil {
				return err
			}
			if err := s.SaveLogs(sess.ID, logs); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "session %s created (%d logs)\n", sess.ID, len(logs))

			// Generate
			progress := func(stage string) {
				if *verbose {
					fmt.Fprintf(cmd.OutOrStdout(), "  [%s]\n", stage)
				}
			}

			doc, err := generator.Generate(sess, logs, cfg.LLM, s, progress, noCache, resume)
			if err != nil {
				return fmt.Errorf("generate: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "generated %d endpoints, output → %s\n", len(doc.Endpoints), cfg.Output.Dir)
			return nil
		},
	}

	cmd.Flags().StringVar(&harPath, "har", "", "HAR file path (required)")
	cmd.Flags().StringVar(&scenario, "scenario", "", "scenario description (required)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "discard cache, regenerate all")
	cmd.Flags().BoolVar(&resume, "resume", false, "resume from failed batches")
	_ = cmd.MarkFlagRequired("har")
	_ = cmd.MarkFlagRequired("scenario")
	return cmd
}

func newImportCmd(cfgPath *string) *cobra.Command {
	var harPath, scenario string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import HAR file into database (without generating)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openStore(*cfgPath)
			if err != nil {
				return err
			}
			defer s.Close()

			logs, err := har.Parse(harPath)
			if err != nil {
				return fmt.Errorf("parse HAR: %w", err)
			}

			host := "unknown"
			if len(logs) > 0 && logs[0].Host != "" {
				host = logs[0].Host
			}

			sess, err := s.CreateSession("har", scenario, host)
			if err != nil {
				return err
			}
			if err := s.SaveLogs(sess.ID, logs); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported %d logs → session %s\n", len(logs), sess.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&harPath, "har", "", "HAR file path")
	cmd.Flags().StringVar(&scenario, "scenario", "", "scenario description")
	_ = cmd.MarkFlagRequired("har")
	return cmd
}

func newServeCmd(cfgPath *string) *cobra.Command {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start preview & extension API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, s, err := openStore(*cfgPath)
			if err != nil {
				return err
			}
			defer s.Close()

			if host != "" {
				cfg.Server.Host = host
			}
			if port != 0 {
				cfg.Server.Port = port
			}

			srv, err := server.New(cfg, s)
			if err != nil {
				return err
			}

			addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
			fmt.Fprintf(cmd.OutOrStdout(), "serving on http://%s\n", addr)
			fmt.Fprintf(cmd.OutOrStdout(), "  ui:      http://%s/\n", addr)
			fmt.Fprintf(cmd.OutOrStdout(), "  docs:    http://%s/docs/\n", addr)
			fmt.Fprintf(cmd.OutOrStdout(), "  api:     http://%s/api/sessions\n", addr)
			return srv.ListenAndServe(addr)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "override server host")
	cmd.Flags().IntVar(&port, "port", 0, "override server port")
	return cmd
}

func newListCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openStore(*cfgPath)
			if err != nil {
				return err
			}
			defer s.Close()

			sessions, err := s.ListSessions()
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sessions found")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSOURCE\tSCENARIO\tHOST\tLOGS\tSTATUS\tCREATED")
			for _, sess := range sessions {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
					sess.ID, sess.Source, truncate(sess.Scenario, 30), sess.Host,
					sess.LogCount, sess.Status, sess.CreatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

func newShowCmd(cfgPath *string) *cobra.Command {
	var session string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show session details",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openStore(*cfgPath)
			if err != nil {
				return err
			}
			defer s.Close()

			sess, err := s.GetSession(session)
			if err != nil {
				return fmt.Errorf("session not found: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "ID:       %s\n", sess.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Source:   %s\n", sess.Source)
			fmt.Fprintf(cmd.OutOrStdout(), "Scenario: %s\n", sess.Scenario)
			fmt.Fprintf(cmd.OutOrStdout(), "Host:     %s\n", sess.Host)
			fmt.Fprintf(cmd.OutOrStdout(), "Logs:     %d\n", sess.LogCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:   %s\n", sess.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Created:  %s\n", sess.CreatedAt.Format("2006-01-02 15:04:05"))

			logs, err := s.GetLogs(sess.ID)
			if err != nil {
				return err
			}
			if len(logs) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\nTraffic:")
				for _, l := range logs {
					fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s %s → %d (%dms)\n",
						l.Seq, l.Method, l.Path, l.StatusCode, l.LatencyMs)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&session, "session", "", "session id")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newDeleteCmd(cfgPath *string) *cobra.Command {
	var session string

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a session and its data",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openStore(*cfgPath)
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.DeleteSession(session); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted session %s\n", session)
			return nil
		},
	}

	cmd.Flags().StringVar(&session, "session", "", "session id")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

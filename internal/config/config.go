package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultConfigRelPath = ".apidoc/config.yaml"

type LLMConfig struct {
	Provider    string  `yaml:"provider"`
	APIKey      string  `yaml:"api_key"`
	BaseURL     string  `yaml:"base_url"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
}

type OutputConfig struct {
	Dir     string   `yaml:"dir"`
	Formats []string `yaml:"formats"`
}

type FilterConfig struct {
	IgnoreExtensions   []string `yaml:"ignore_extensions"`
	IgnoreContentTypes []string `yaml:"ignore_content_types"`
	IgnorePaths        []string `yaml:"ignore_paths"`
}

type SanitizeConfig struct {
	Headers     []string `yaml:"headers"`
	BodyFields  []string `yaml:"body_fields"`
	Replacement string   `yaml:"replacement"`
}

type ServerConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	CORSExtensionID string `yaml:"cors_extension_id"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type Config struct {
	LLM      LLMConfig      `yaml:"llm"`
	Output   OutputConfig   `yaml:"output"`
	Filter   FilterConfig   `yaml:"filter"`
	Sanitize SanitizeConfig `yaml:"sanitize"`
	Server   ServerConfig   `yaml:"server"`
	Log      LogConfig      `yaml:"log"`
}

// Load loads YAML config, then applies env overrides.
func Load(configPath string) (*Config, error) {
	cfg := &Config{}
	cfg.SetDefaults()

	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		configPath = filepath.Join(home, defaultConfigRelPath)
	}

	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config: %w", err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

func (c *Config) SetDefaults() {
	if c.LLM.Provider == "" {
		c.LLM.Provider = "openai"
	}
	if c.LLM.BaseURL == "" {
		c.LLM.BaseURL = "https://api.openai.com/v1"
	}
	if c.LLM.Model == "" {
		c.LLM.Model = "gpt-4o"
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 4096
	}
	if c.LLM.Temperature == 0 {
		c.LLM.Temperature = 0.2
	}
	if c.Output.Dir == "" {
		c.Output.Dir = "./output"
	}
	if len(c.Output.Formats) == 0 {
		c.Output.Formats = []string{"markdown", "openapi"}
	}
	if len(c.Filter.IgnoreExtensions) == 0 {
		c.Filter.IgnoreExtensions = []string{".js", ".css", ".png", ".jpg", ".gif", ".svg", ".woff", ".woff2", ".ico", ".map"}
	}
	if len(c.Filter.IgnoreContentTypes) == 0 {
		c.Filter.IgnoreContentTypes = []string{"text/html", "text/css", "image/*", "font/*", "application/javascript"}
	}
	if len(c.Filter.IgnorePaths) == 0 {
		c.Filter.IgnorePaths = []string{"/static/", "/assets/", "/favicon"}
	}
	if len(c.Sanitize.Headers) == 0 {
		c.Sanitize.Headers = []string{"Authorization", "Cookie", "Set-Cookie", "X-Api-Key", "X-Auth-Token"}
	}
	if len(c.Sanitize.BodyFields) == 0 {
		c.Sanitize.BodyFields = []string{"password", "secret", "token", "api_key", "access_token", "refresh_token", "credential"}
	}
	if c.Sanitize.Replacement == "" {
		c.Sanitize.Replacement = "***REDACTED***"
	}
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 3000
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.Output.Dir) == "" {
		return errors.New("output.dir cannot be empty")
	}

	if err := ensureWritableDir(c.Output.Dir); err != nil {
		return fmt.Errorf("output.dir not writable: %w", err)
	}
	return nil
}

// ValidateGenerate enforces generate-specific requirements.
func (c *Config) ValidateGenerate() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.LLM.APIKey) == "" {
		return errors.New("llm.api_key cannot be empty")
	}
	return nil
}

func ensureWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".writable-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

func applyEnvOverrides(c *Config) {
	setString(&c.LLM.Provider, "APIDOC_LLM_PROVIDER")
	setString(&c.LLM.APIKey, "APIDOC_LLM_API_KEY")
	setString(&c.LLM.BaseURL, "APIDOC_LLM_BASE_URL")
	setString(&c.LLM.Model, "APIDOC_LLM_MODEL")
	setInt(&c.LLM.MaxTokens, "APIDOC_LLM_MAX_TOKENS")
	setFloat(&c.LLM.Temperature, "APIDOC_LLM_TEMPERATURE")
	setString(&c.Output.Dir, "APIDOC_OUTPUT_DIR")
	setString(&c.Server.Host, "APIDOC_SERVER_HOST")
	setInt(&c.Server.Port, "APIDOC_SERVER_PORT")
	setString(&c.Log.Level, "APIDOC_LOG_LEVEL")
}

func setString(dst *string, key string) {
	if v, ok := os.LookupEnv(key); ok {
		*dst = v
	}
}

func setInt(dst *int, key string) {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func setFloat(dst *float64, key string) {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			*dst = n
		}
	}
}

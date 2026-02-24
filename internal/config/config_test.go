package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetDefaults(t *testing.T) {
	c := &Config{}
	c.SetDefaults()
	if c.LLM.Model != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", c.LLM.Model)
	}
	if c.Server.Port != 3000 {
		t.Fatalf("expected port 3000")
	}
	if c.Server.Host != "127.0.0.1" {
		t.Fatalf("expected default host")
	}
	if c.Log.Level != "info" {
		t.Fatalf("expected info level")
	}
}

func TestLoadFromYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("llm:\n  model: gpt-4.1\nserver:\n  port: 8080\noutput:\n  dir: ./out\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != "gpt-4.1" {
		t.Fatalf("unexpected model %s", cfg.LLM.Model)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("unexpected port %d", cfg.Server.Port)
	}
}

func TestValidate(t *testing.T) {
	c := &Config{}
	c.SetDefaults()
	c.Output.Dir = t.TempDir()
	if err := c.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	c.LLM.APIKey = ""
	if err := c.ValidateGenerate(); err == nil {
		t.Fatalf("expected generate validation error")
	}
}

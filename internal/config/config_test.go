package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulasdursun/job-hunter/internal/config"
)

func TestTemplateProfileGuardRejectsDefaultYourName(t *testing.T) {
	templatePath := filepath.Join("..", "..", "config.yaml")
	_, err := config.Load(templatePath)
	if err == nil {
		t.Fatalf("expected error when loading default template profile 'Your Name', got nil")
	}
	if !strings.Contains(err.Error(), config.TemplateProfileErrorMsg) {
		t.Errorf("expected error to contain %q, got %q", config.TemplateProfileErrorMsg, err.Error())
	}
}

func TestLoadTemplateConfigWithRealCandidateName(t *testing.T) {
	templateBytes, err := os.ReadFile(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to read template config.yaml: %v", err)
	}

	customized := strings.Replace(string(templateBytes), `name: "Your Name"`, `name: "Real Candidate"`, 1)
	tmpFile := filepath.Join(t.TempDir(), "config.custom.yaml")
	if err := os.WriteFile(tmpFile, []byte(customized), 0600); err != nil {
		t.Fatalf("failed to write customized config: %v", err)
	}

	cfg, err := config.Load(tmpFile)
	if err != nil {
		t.Fatalf("failed to load customized template config: %v", err)
	}

	if cfg.Candidate.Name != "Real Candidate" {
		t.Errorf("expected candidate.name 'Real Candidate', got %q", cfg.Candidate.Name)
	}
	if !cfg.Sources.KariyerNet.Enabled {
		t.Errorf("expected sources.kariyer_net.enabled to be true in template config")
	}
	if !cfg.Sources.Coderspace.Enabled {
		t.Errorf("expected sources.coderspace.enabled to be true in template config")
	}
	if !cfg.Sources.ATS.Enabled || !cfg.Sources.JobSpy.Enabled {
		t.Errorf("expected ATS and JobSpy sources to be enabled in template config")
	}
	if len(cfg.Candidate.Experience) == 0 || len(cfg.Candidate.Projects) == 0 {
		t.Errorf("expected template config to include sample experience and projects")
	}
}

func TestLoadLocalOverrideAndFallback(t *testing.T) {
	templateBytes, err := os.ReadFile(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to read template config.yaml: %v", err)
	}

	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmpDir: %v", err)
	}

	// 1. Sadece şablon config.yaml ("Your Name") varken ve config.local.yaml yokken şablon profil kalkanı devreye girmeli
	if err := os.WriteFile("config.yaml", templateBytes, 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if _, err := config.Load("config.yaml"); err == nil || !strings.Contains(err.Error(), config.TemplateProfileErrorMsg) {
		t.Fatalf("expected template profile guard error when config.local.yaml is missing, got: %v", err)
	}

	// 2. config.local.yaml eklendiğinde otomatik olarak config.local.yaml öncelikli okunmalı ve geçmeli
	localBytes := []byte("candidate:\n  name: \"Local Override User\"\n  target_roles:\n    primary: [\"Backend Engineer\"]\nscoring:\n  telegram_threshold_primary: 8.0\nredis:\n  stream_raw: \"jobs:raw\"\nserver:\n  metrics_port: 9091\n")
	if err := os.WriteFile("config.local.yaml", localBytes, 0600); err != nil {
		t.Fatalf("write config.local.yaml: %v", err)
	}
	cfgLocal, err := config.Load("config.yaml")
	if err != nil {
		t.Fatalf("Load local override failed: %v", err)
	}
	if cfgLocal.Candidate.Name != "Local Override User" {
		t.Errorf("expected local override candidate name 'Local Override User', got %q", cfgLocal.Candidate.Name)
	}
}

func TestPostgresDSN(t *testing.T) {
	t.Setenv("POSTGRES_USER", "jobhunter")
	t.Setenv("POSTGRES_PASSWORD", "jobhunter")
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "")
	t.Setenv("POSTGRES_DB", "jobhunter")

	dsn := config.PostgresDSN()
	if !strings.HasPrefix(dsn, "postgres://") {
		t.Fatalf("expected DSN to start with 'postgres://', got: %s", dsn)
	}
	if !strings.Contains(dsn, "@localhost:5433/jobhunter?sslmode=disable") {
		t.Fatalf("expected default fallback port 5433 in DSN, got: %s", dsn)
	}
	if strings.Contains(dsn, "%!(EXTRA") {
		t.Fatalf("detected format string parameter mismatch in DSN: %s", dsn)
	}

	masked := config.MaskedPostgresDSN()
	if !strings.Contains(masked, ":%2A%2A%2A@") && !strings.Contains(masked, ":***@") {
		t.Fatalf("expected masked DSN to hide password, got: %s", masked)
	}

	t.Setenv("POSTGRES_PORT", "5439")
	customDSN := config.PostgresDSN()
	if !strings.Contains(customDSN, "@localhost:5439/jobhunter?sslmode=disable") {
		t.Fatalf("expected custom port 5439 in DSN, got: %s", customDSN)
	}
}

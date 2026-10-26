package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ulasdursun/job-hunter/internal/config"
)

func TestLoadTemplateConfig(t *testing.T) {
	templatePath := filepath.Join("..", "..", "config.yaml")
	cfg, err := config.Load(templatePath)
	if err != nil {
		t.Fatalf("failed to load template config.yaml: %v", err)
	}

	if cfg.Candidate.Name == "" {
		t.Errorf("expected non-empty candidate.name in template config")
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

	// 1. Sadece config.yaml varken fallback olarak config.yaml okunmalı
	if err := os.WriteFile("config.yaml", templateBytes, 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	cfgFallback, err := config.Load("config.yaml")
	if err != nil {
		t.Fatalf("Load fallback failed: %v", err)
	}
	if cfgFallback.Candidate.Name != "Your Name" {
		t.Errorf("expected fallback candidate name 'Your Name', got %q", cfgFallback.Candidate.Name)
	}

	// 2. config.local.yaml eklendiğinde otomatik olarak config.local.yaml öncelikli okunmalı
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

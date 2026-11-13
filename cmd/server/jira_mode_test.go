package main

import (
	"testing"
)

func TestJiraDisabledByDefault(t *testing.T) {
	t.Setenv("JIRA_MODE", "")
	cfg := loadConfig().Jira
	if cfg.Mode != "off" {
		t.Fatalf("default Jira mode = %q, want off", cfg.Mode)
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("off mode should not require credentials: %v", err)
	}
}

func TestJiraMockModeAcceptedWithoutLiveCredentials(t *testing.T) {
	t.Setenv("JIRA_MODE", "mock")
	t.Setenv("JIRA_BASE_URL", "http://mock-jira:8080")
	t.Setenv("JIRA_PROJECT_KEY", "SCRUM")
	cfg := loadConfig().Jira
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Mode = "live"
	if err := cfg.validate(); err == nil {
		t.Fatal("live mode must be rejected")
	}
}

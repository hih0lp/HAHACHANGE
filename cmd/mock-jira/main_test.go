package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureReloadAndPersonalList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	write := func(summary string) {
		t.Helper()
		b := []byte(`{"project_key":"SCRUM","issues":[{"key":"SCRUM-7","summary":"` + summary + `","status":"В работе","description":"","assignee_account_id":"person"},{"key":"SCRUM-6","summary":"Other","status":"К выполнению","description":""}]}`)
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("First")
	f, e := load(path)
	if e != nil {
		t.Fatal(e)
	}
	got := list(f, "person", 50)["issues"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["summary"] != "First" {
		t.Fatal(got)
	}
	write("Changed")
	f, e = load(path)
	if e != nil {
		t.Fatal(e)
	}
	v, e := find(f, "SCRUM-7")
	if e != nil || v.Summary != "Changed" || v.Description != "" {
		t.Fatalf("%+v %v", v, e)
	}
	if e = os.WriteFile(path, []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = load(path); e == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestFixtureRejectsInvalidDate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jira.json")
	data := []byte(`{"project_key":"SCRUM","issues":[{"key":"SCRUM-7","summary":"Task","status":"In progress","due_date":"2026-11-31"}]}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err == nil {
		t.Fatal("invalid date accepted")
	}
}

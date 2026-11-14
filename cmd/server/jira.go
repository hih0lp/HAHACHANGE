package main

import (
	"errors"
	"net/url"
)

type jiraConfig struct{ Mode, BaseURL, ProjectKey string }

func (c jiraConfig) validate() error {
	if c.Mode != "off" && c.Mode != "mock" {
		return errors.New("JIRA_MODE must be off or mock")
	}
	if c.Mode == "off" {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || c.ProjectKey == "" {
		return errors.New("mock Jira needs an internal HTTP origin and project key")
	}
	return nil
}

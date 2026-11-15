package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRenameBranchUpdatesReferences(t *testing.T) {
	r := httptest.NewRequest("PUT", "/v1/mock-data/gitlab/projects/2/branches/docs/demo", nil)
	r.SetPathValue("project", "2")
	r.SetPathValue("name", "docs/demo")
	data, _ := json.Marshal(defaultMockData())
	changed, err := renameMockBranch(data, r, []byte(`{"name":"docs/review"}`))
	if err != nil {
		t.Fatal(err)
	}
	f, err := parseMockData(changed)
	if err != nil || f.Projects[0].Branches[1].Name != "docs/review" || f.Projects[0].MRs[0].Source != "docs/review" {
		t.Fatalf("changed: %+v %v", f, err)
	}
	if _, err := renameMockBranch(data, r, []byte(`{"name":"main"}`)); err == nil {
		t.Fatal("duplicate branch accepted")
	}
}

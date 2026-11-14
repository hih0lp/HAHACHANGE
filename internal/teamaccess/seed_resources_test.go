package teamaccess

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSeedResources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources.json")
	write := func(data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"resources":[{"id":"test-resource","team_id":"team","kind":"service","name":"Test","external_ref":"project","url":"https://example.test/","capabilities":["read"],"approver_id":"mentor"}]}`)
	resources, err := loadSeedResources(path)
	if err != nil || len(resources) != 1 || resources[0].Capabilities[0] != "read" {
		t.Fatalf("valid resources rejected: %+v, %v", resources, err)
	}
	write(`{"resources":[{"id":"test-resource","team_id":"team","kind":"service","name":"Test","external_ref":"project","url":"http://internal/","capabilities":["read"],"approver_id":"mentor"}]}`)
	if _, err := loadSeedResources(path); err == nil {
		t.Fatal("unsafe URL accepted")
	}
	write(`{"resources":`)
	if _, err := loadSeedResources(path); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

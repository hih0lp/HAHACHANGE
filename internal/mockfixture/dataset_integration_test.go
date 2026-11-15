package mockfixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/hackchange/avito-rag/internal/testsupport"
)

func TestDatasetSavesValidatedVersionsForMentor(t *testing.T) {
	db := testsupport.Postgres(t)
	ctx := context.Background()
	if err := createTable(ctx, db); err != nil {
		t.Fatal(err)
	}
	seed := func() ([]byte, error) { return []byte(`{"value":"initial"}`), nil }
	validate := func(data []byte) error {
		var doc struct{ Value string }
		if json.Unmarshal(data, &doc) != nil || doc.Value == "" {
			return errors.New("value is required")
		}
		return nil
	}
	d, err := Seed(ctx, db, "test-dataset", seed, validate)
	if err != nil {
		t.Fatal(err)
	}
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer mentor" {
			_, _ = w.Write([]byte(`{"id":"approver"}`))
		} else {
			_, _ = w.Write([]byte(`{"id":"intern"}`))
		}
	}))
	defer directory.Close()
	mux := http.NewServeMux()
	d.Register(mux, scenario.Client{BaseURL: directory.URL})
	request := func(method, token, body string, want int) snapshot {
		t.Helper()
		req := httptest.NewRequest(method, "/v1/mock-data/test-dataset", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		result := httptest.NewRecorder()
		mux.ServeHTTP(result, req)
		if result.Code != want {
			t.Fatalf("%s %s: got %d, wanted %d (%s)", method, token, result.Code, want, result.Body.String())
		}
		var out snapshot
		_ = json.Unmarshal(result.Body.Bytes(), &out)
		return out
	}
	if request("GET", "mentor", "", 200).Version != 1 {
		t.Fatal("seed starts at version 1")
	}
	request("GET", "intern", "", 403)
	request("PUT", "intern", `{"version":1,"data":{"value":"unauthorized"}}`, 403)
	request("PUT", "mentor", `{"version":1,"data":{}}`, 400)
	saved := request("PUT", "mentor", `{"version":1,"data":{"value":"edited"}}`, 200)
	if saved.Version != 2 || !bytes.Contains(saved.Data, []byte("edited")) {
		t.Fatalf("save returned %+v", saved)
	}
	request("PUT", "mentor", `{"version":1,"data":{"value":"stale"}}`, 409)
	reopened, err := Seed(ctx, db, "test-dataset", seed, validate)
	if err != nil {
		t.Fatal(err)
	}
	data, err := reopened.Bytes(ctx)
	if err != nil || !bytes.Contains(data, []byte("edited")) {
		t.Fatalf("reopened data=%s err=%v", data, err)
	}
}

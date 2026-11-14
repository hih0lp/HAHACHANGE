package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchParameters(t *testing.T) {
	good := searchArgs{Query: "каталог", From: "2026-11-13T00:00:00+03:00", To: "2026-11-14T00:00:00+03:00"}
	from, to, limit, err := searchParameters(good)
	if err != nil || to.Sub(from) != 24*time.Hour || limit != 15 {
		t.Fatalf("bad defaults: %v %v %d %v", from, to, limit, err)
	}
	for _, change := range []func(*searchArgs){
		func(a *searchArgs) { a.Query = " " },
		func(a *searchArgs) { a.To = a.From },
		func(a *searchArgs) { a.From = "yesterday" },
		func(a *searchArgs) { a.To = "2027-01-01T00:00:00Z" },
		func(a *searchArgs) { a.Limit = 31 },
	} {
		args := good
		change(&args)
		if _, _, _, err := searchParameters(args); err == nil {
			t.Fatalf("accepted invalid arguments: %+v", args)
		}
	}
}

func TestEmbedder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		vectors [][]float32
		wantErr bool
	}{
		{"valid", 200, [][]float32{append([]float32{3, 4}, make([]float32, 382)...)}, false},
		{"bad dimensions", 200, [][]float32{{1, 2}}, true},
		{"bad count", 200, [][]float32{}, true},
		{"zero norm", 200, [][]float32{make([]float32, 384)}, true},
		{"unavailable", 503, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(tc.vectors)
			}))
			defer server.Close()
			vectors, err := (embedder{server.URL, server.Client()}).embed(context.Background(), "query", []string{"каталог"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && (vectors[0][0] != 0.6 || vectors[0][1] != 0.8) {
				t.Fatalf("vector not normalized: %v", vectors[0][:2])
			}
		})
	}
}

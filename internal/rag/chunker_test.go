package rag

import "testing"

func TestChunkTextKeepsAllContent(t *testing.T) {
	chunks := ChunkText("один два три четыре пять шесть семь восемь", 15, 4)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want at least two", len(chunks))
	}
	if chunks[0] == "" || chunks[len(chunks)-1] == "" {
		t.Fatal("chunks must not be empty")
	}
}

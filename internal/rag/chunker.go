package rag

import "strings"

// ChunkText preserves paragraph boundaries where possible and uses a small
// overlap, so a policy rule split near a boundary remains understandable.
func ChunkText(text string, size, overlap int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	paragraphs := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' })
	chunks := make([]string, 0)
	current := ""
	appendChunk := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && (len(chunks) == 0 || chunks[len(chunks)-1] != value) {
			chunks = append(chunks, value)
		}
	}

	for _, paragraph := range paragraphs {
		candidate := paragraph
		if current != "" {
			candidate = current + "\n\n" + paragraph
		}
		if len(candidate) <= size {
			current = candidate
			continue
		}
		appendChunk(current)
		remaining := paragraph
		for len(remaining) > size {
			cut := strings.LastIndex(remaining[:size], "\n")
			if cut <= size/2 {
				cut = strings.LastIndex(remaining[:size], " ")
			}
			if cut <= 0 {
				cut = size
			}
			appendChunk(remaining[:cut])
			start := cut - overlap
			// An overlap cannot be as long as the emitted portion: that would
			// leave remaining unchanged for a short first word.
			if start <= 0 {
				start = cut
			}
			remaining = strings.TrimSpace(remaining[start:])
		}
		current = remaining
	}
	appendChunk(current)
	return chunks
}

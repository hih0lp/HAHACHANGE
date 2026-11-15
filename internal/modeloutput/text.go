// Package modeloutput separates marked reasoning from public answer text.
package modeloutput

import "strings"

// PublicText removes Qwen's marked reasoning. Some providers omit the opening
// tag, so an unmatched closing tag also discards the preceding text.
func PublicText(text string) string {
	const open, close = "<think>", "</think>"
	var public strings.Builder
	for {
		start := strings.Index(text, open)
		end := strings.Index(text, close)
		switch {
		case end >= 0 && (start < 0 || end < start):
			public.Reset()
			text = text[end+len(close):]
		case start >= 0:
			public.WriteString(text[:start])
			rest := text[start+len(open):]
			end = strings.Index(rest, close)
			if end < 0 {
				return strings.TrimSpace(public.String())
			}
			text = rest[end+len(close):]
		default:
			public.WriteString(text)
			return strings.TrimSpace(public.String())
		}
	}
}

// PublicTextLimit applies a character limit after removing marked reasoning.
func PublicTextLimit(text string, limit int) string {
	public := PublicText(text)
	count := 0
	for i := range public {
		if count == limit {
			return public[:i]
		}
		count++
	}
	return public
}

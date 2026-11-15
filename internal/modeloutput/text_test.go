package modeloutput

import "testing"

func TestPublicText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{" Обычный ответ. ", "Обычный ответ."},
		{"<think>служебный текст\n</think>\nОтвет.", "Ответ."},
		{"служебный текст\n</think>\nОтвет.", "Ответ."},
		{"<think>один</think><think>два</think>Ответ.", "Ответ."},
		{"<think>незавершённое рассуждение", ""},
		{"служебный текст</think>", ""},
	} {
		if got := PublicText(tc.in); got != tc.want {
			t.Errorf("PublicText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPublicTextLimitCutsOnlyPublicRunes(t *testing.T) {
	if got := PublicTextLimit("служебный текст</think>Привет", 3); got != "При" {
		t.Fatalf("got %q", got)
	}
}

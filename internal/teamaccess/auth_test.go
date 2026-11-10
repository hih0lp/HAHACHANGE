package teamaccess

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
)

func TestJWTSignatureAndAudience(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{key: key, issuer: "team-access"}
	token, err := s.sign("intern")
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := s.verify(token); !ok || id != "intern" {
		t.Fatalf("valid token rejected: %q %v", id, ok)
	}
	parts := strings.Split(token, ".")
	parts[1] = parts[1][:len(parts[1])-1] + "A"
	if _, ok := s.verify(strings.Join(parts, ".")); ok {
		t.Fatal("tampered token accepted")
	}
	if _, ok := (&Service{key: key, issuer: "other"}).verify(token); ok {
		t.Fatal("wrong issuer accepted")
	}
}

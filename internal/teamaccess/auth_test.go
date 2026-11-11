package teamaccess

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	if id, ok := (&Service{key: key, issuer: "team-access", role: "rights"}).verify(token); !ok || id != "intern" {
		t.Fatalf("rights audience rejected: %q %v", id, ok)
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

func TestDemoTokenLifetime(t *testing.T) {
	t.Setenv("TEAM_ACCESS_TOKEN_TTL_HOURS", "8760")
	if tokenLifetime() != 365*24*time.Hour {
		t.Fatal("expected one-year lifetime")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token, err := (&Service{key: key, issuer: "team-access"}).sign("intern")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	body, err := enc.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claim claims
	if err := json.Unmarshal(body, &claim); err != nil {
		t.Fatal(err)
	}
	if claim.Expiry-claim.Issued != int64((365 * 24 * time.Hour).Seconds()) {
		t.Fatal("incorrect JWT expiry")
	}
}

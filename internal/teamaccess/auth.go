package teamaccess

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type identityKey struct{}
type claims struct {
	Issuer    string   `json:"iss"`
	Audience  []string `json:"aud"`
	Subject   string   `json:"sub"`
	Issued    int64    `json:"iat"`
	NotBefore int64    `json:"nbf"`
	Expiry    int64    `json:"exp"`
	ID        string   `json:"jti"`
}

var enc = base64.RawURLEncoding

func (s *Service) sign(subject string) (string, error) {
	c := claims{Issuer: s.issuer, Audience: []string{"ai-buddy", "team-access", "access-rights"}, Subject: subject, Issued: time.Now().Unix(), NotBefore: time.Now().Unix(), Expiry: time.Now().Add(time.Hour).Unix(), ID: newID()}
	head, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "team-access-1"})
	body, _ := json.Marshal(c)
	raw := enc.EncodeToString(head) + "." + enc.EncodeToString(body)
	digest := sha256.Sum256([]byte(raw))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return raw + "." + enc.EncodeToString(sig), nil
}
func (s *Service) verify(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	hb, e := enc.DecodeString(parts[0])
	if e != nil {
		return "", false
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(hb, &header) != nil || header.Alg != "RS256" || header.Kid != "team-access-1" {
		return "", false
	}
	cb, e := enc.DecodeString(parts[1])
	if e != nil {
		return "", false
	}
	var c claims
	if json.Unmarshal(cb, &c) != nil {
		return "", false
	}
	sig, e := enc.DecodeString(parts[2])
	if e != nil {
		return "", false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		return "", false
	}
	t := time.Now().Unix()
	if c.Issuer != s.issuer || c.Subject == "" || c.ID == "" || c.Issued > t || c.NotBefore > t || c.Expiry <= t {
		return "", false
	}
	neededAudience := "team-access"
	if s.role == "rights" {
		neededAudience = "access-rights"
	}
	found := false
	for _, aud := range c.Audience {
		if aud == neededAudience {
			found = true
		}
	}
	return c.Subject, found
}
func (s *Service) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			fail(w, 401, "unauthorized", "Bearer token required")
			return
		}
		id, ok := s.verify(strings.TrimPrefix(h, "Bearer "))
		if !ok {
			fail(w, 401, "unauthorized", "Invalid or expired token")
			return
		}
		if s.role == "rights" {
			next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
			return
		}
		var exists bool
		err := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ta_users WHERE id=$1)`, id).Scan(&exists)
		if err != nil {
			fail(w, 503, "dependency_unavailable", "Identity lookup unavailable")
			return
		}
		if !exists {
			fail(w, 401, "unauthorized", "Unknown user")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}
}
func user(r *http.Request) string { return r.Context().Value(identityKey{}).(string) }
func (s *Service) jwks(w http.ResponseWriter, r *http.Request) {
	n := enc.EncodeToString(s.key.PublicKey.N.Bytes())
	e := big.NewInt(int64(s.key.PublicKey.E)).Bytes()
	respond(w, 200, map[string]any{"keys": []any{map[string]string{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "team-access-1", "n": n, "e": enc.EncodeToString(e)}}})
}
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	var id, hash string
	err := s.DB.QueryRow(r.Context(), `SELECT id,password_hash FROM ta_users WHERE username=$1`, in.Username).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		fail(w, 401, "unauthorized", "Invalid credentials")
		return
	}
	token, err := s.sign(id)
	if err != nil {
		fail(w, 503, "signing_unavailable", "Cannot issue token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 3600})
}

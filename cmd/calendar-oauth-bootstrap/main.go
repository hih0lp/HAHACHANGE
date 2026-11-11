package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func randomString() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > 4096 {
		return "", errors.New("OAuth configuration file is too large")
	}
	return strings.TrimSpace(string(data)), nil
}

func run() error {
	clientIDPath := flag.String("client-id-file", "", "path to Google Desktop OAuth client ID")
	clientSecretPath := flag.String("client-secret-file", "", "path to Google Desktop OAuth client secret")
	outputPath := flag.String("refresh-token-file", "", "new private file for the refresh token")
	flag.Parse()
	if *clientIDPath == "" || *clientSecretPath == "" || *outputPath == "" {
		return errors.New("all three file paths are required")
	}
	clientID, err := readFile(*clientIDPath)
	if err != nil {
		return err
	}
	clientSecret, err := readFile(*clientSecretPath)
	if err != nil {
		return err
	}
	if clientID == "" || clientSecret == "" {
		return errors.New("OAuth client files are empty")
	}
	if _, err := os.Stat(*outputPath); err == nil {
		return errors.New("refresh token file already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	state, err := randomString()
	if err != nil {
		return err
	}
	verifier, err := randomString()
	if err != nil {
		return err
	}
	challenge := sha256.Sum256([]byte(verifier))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	redirect := "http://" + listener.Addr().String() + "/callback"
	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "OAuth state mismatch", http.StatusForbidden)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "OAuth authorization not completed", http.StatusBadRequest)
			return
		}
		select {
		case codeCh <- code:
			fmt.Fprint(w, "Бобби получил разрешение. Можно закрыть эту вкладку.")
		default:
			http.Error(w, "OAuth authorization already received", http.StatusConflict)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	params := url.Values{"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {"https://www.googleapis.com/auth/calendar.readonly"}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	fmt.Println("Откройте ссылку в браузере, выберите нужный Google-аккаунт и подтвердите доступ только на чтение календаря:")
	fmt.Println("https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode())
	var code string
	select {
	case code = <-codeCh:
	case <-time.After(5 * time.Minute):
		return errors.New("OAuth authorization timed out")
	}
	form := url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}, "grant_type": {"authorization_code"}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Google token exchange failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Google token exchange returned HTTP %d", resp.StatusCode)
	}
	var token struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&token); err != nil || token.RefreshToken == "" {
		return errors.New("Google did not return a refresh token")
	}
	file, err := os.OpenFile(*outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(token.RefreshToken + "\n"); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Println("Refresh token сохранён в указанный файл; его значение не выводится.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

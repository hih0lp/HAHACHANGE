package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}
type RemoteError struct{ Status int }
type LoginResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (c Client) Login(ctx context.Context, username, password string) (LoginResult, error) {
	var v LoginResult
	e := c.call(ctx, "", "POST", "/v1/auth/login", "", map[string]string{"username": username, "password": password}, &v)
	return v, e
}
func (e *RemoteError) Error() string {
	return fmt.Sprintf("team access service returned HTTP %d", e.Status)
}
func (c Client) call(ctx context.Context, token, method, path, idem string, input, out any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &RemoteError{Status: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
func (c Client) Profile(ctx context.Context, t string) (Profile, error) {
	var v Profile
	e := c.call(ctx, t, "GET", "/v1/me", "", nil, &v)
	if e == nil && v.ID == "" {
		e = fmt.Errorf("profile is missing user ID")
	}
	return v, e
}
func (c Client) Resources(ctx context.Context, t, team string) ([]Resource, error) {
	var v struct {
		Resources []Resource `json:"resources"`
	}
	e := c.call(ctx, t, "GET", "/v1/teams/"+url.PathEscape(team)+"/resources", "", nil, &v)
	return v.Resources, e
}
func (c Client) Check(ctx context.Context, t, id, capability string) (Check, error) {
	var v Check
	e := c.call(ctx, t, "POST", "/v1/access/check", "", map[string]string{"resource_id": id, "capability": capability}, &v)
	return v, e
}
func (c Client) CreateRequest(ctx context.Context, t, idem string, input AccessInput) (Request, error) {
	var v Request
	e := c.call(ctx, t, "POST", "/v1/access-requests", idem, input, &v)
	return v, e
}

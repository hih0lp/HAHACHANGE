package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type query struct {
	TimeMin string `json:"time_min" jsonschema:"RFC3339 start of range"`
	TimeMax string `json:"time_max" jsonschema:"RFC3339 end of range"`
}

type bridge struct {
	directory, subject, eventsID, mentorID string
	clientID, clientSecret, refreshToken   string
	tokenURL, apiURL                       string
	client                                 *http.Client
}

func (b bridge) authorize(r *http.Request) bool {
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || bearer == "" {
		return false
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, b.directory+"/v1/me", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := b.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var profile struct {
		ID string `json:"id"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&profile) == nil && profile.ID == b.subject
}

func (b bridge) accessToken(ctx context.Context) (string, error) {
	form := url.Values{"client_id": {b.clientID}, "client_secret": {b.clientSecret}, "refresh_token": {b.refreshToken}, "grant_type": {"refresh_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client.Do(req)
	if err != nil {
		return "", errors.New("Google authorization is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google authorization returned HTTP %d", resp.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&token); err != nil || token.AccessToken == "" {
		return "", errors.New("Google authorization response is invalid")
	}
	return token.AccessToken, nil
}

func (b bridge) call(ctx context.Context, name string, in query) (map[string]any, error) {
	start, err := time.Parse(time.RFC3339, in.TimeMin)
	if err != nil {
		return nil, errors.New("time_min must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, in.TimeMax)
	if err != nil || !end.After(start) || end.Sub(start) > 31*24*time.Hour {
		return nil, errors.New("range must be positive and at most 31 days")
	}
	token, err := b.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	var req *http.Request
	switch name {
	case "calendar_onboarding_events":
		v := url.Values{"timeMin": {in.TimeMin}, "timeMax": {in.TimeMax}, "singleEvents": {"true"}, "maxResults": {"100"}, "orderBy": {"startTime"}}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, b.apiURL+"/calendars/"+url.PathEscape(b.mentorID)+"/events?"+v.Encode(), nil)
	case "calendar_mentor_freebusy":
		payload, _ := json.Marshal(map[string]any{"timeMin": in.TimeMin, "timeMax": in.TimeMax, "items": []map[string]string{{"id": b.mentorID}}})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL+"/freeBusy", bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	default:
		return nil, errors.New("unknown calendar tool")
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, errors.New("Google Calendar is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Calendar returned HTTP %d", resp.StatusCode)
	}
	const limit = 128 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(data) > limit || !json.Valid(data) {
		return nil, errors.New("Google Calendar response is invalid or too large")
	}
	if name == "calendar_mentor_freebusy" {
		var response struct {
			Calendars map[string]struct {
				Errors []json.RawMessage `json:"errors"`
			} `json:"calendars"`
		}
		if json.Unmarshal(data, &response) != nil {
			return nil, errors.New("mentor availability is unknown")
		}
		entry, present := response.Calendars[b.mentorID]
		if !present || len(entry.Errors) > 0 {
			return nil, errors.New("mentor availability is unknown")
		}
	}
	return map[string]any{"data": json.RawMessage(data), "source": "Google Calendar demo", "time_min": in.TimeMin, "time_max": in.TimeMax}, nil
}

func (b bridge) handler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "bobby-calendar", Version: "0.1.0"}, nil)
	for _, tool := range []struct{ name, description string }{
		{"calendar_onboarding_events", "Read events from the selected onboarding calendar in a bounded date range."},
		{"calendar_mentor_freebusy", "Read only busy intervals from the selected mentor demo calendar; never reveals event titles."},
	} {
		mcp.AddTool(server, &mcp.Tool{Name: tool.name, Description: tool.description}, func(ctx context.Context, _ *mcp.CallToolRequest, in query) (*mcp.CallToolResult, map[string]any, error) {
			result, err := b.call(ctx, tool.name, in)
			return nil, result, err
		})
	}
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.authorize(r) {
			http.Error(w, "calendar binding unavailable", http.StatusForbidden)
			return
		}
		stream.ServeHTTP(w, r)
	})
}

func readSecret(name string) string {
	data, err := os.ReadFile(os.Getenv(name))
	if err != nil {
		log.Fatalf("%s file is required", name)
	}
	return strings.TrimSpace(string(data))
}

func main() {
	b := bridge{
		directory: strings.TrimRight(os.Getenv("TEAM_DIRECTORY_URL"), "/"), subject: os.Getenv("CALENDAR_DEMO_SUBJECT"),
		eventsID: os.Getenv("CALENDAR_MENTOR_ID"), mentorID: os.Getenv("CALENDAR_MENTOR_ID"),
		clientID: readSecret("GOOGLE_CLIENT_ID_FILE"), clientSecret: readSecret("GOOGLE_CLIENT_SECRET_FILE"), refreshToken: readSecret("GOOGLE_REFRESH_TOKEN_FILE"),
		tokenURL: "https://oauth2.googleapis.com/token", apiURL: "https://www.googleapis.com/calendar/v3",
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	if b.subject == "" || b.eventsID == "" || b.mentorID == "" || b.clientID == "" || b.clientSecret == "" || b.refreshToken == "" {
		log.Fatal("calendar demo configuration is incomplete")
	}
	u, err := url.Parse(b.directory)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		log.Fatal("team directory must be an HTTP or HTTPS origin")
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", b.handler())
	log.Fatal((&http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

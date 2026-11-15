package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditedGitLabProjectRespectsDemoRole(t *testing.T) {
	data := defaultMockData()
	data.Projects[0].Owner = "intern2"
	data.Projects[0].MRs[0].Approved = true
	read := func(context.Context) (mockData, error) { return data, nil }
	for _, tc := range []struct {
		path, user, contains string
		status               int
	}{
		{"/api/v4/projects", "intern", "[]", 200},
		{"/api/v4/projects/2/merge_requests/1/approval_state", "intern", "Not Found", 404},
		{"/api/v4/projects/2/merge_requests/1/approval_state", "intern2", `"approved":true`, 200},
		{"/api/v4/projects/2/merge_requests/1/approval_state", "approver", `"approved":true`, 200},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("PRIVATE-TOKEN", tc.user)
		w := httptest.NewRecorder()
		serveMockGitLab(w, r, read)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("%s as %s: status=%d body=%s", tc.path, tc.user, w.Code, w.Body.String())
		}
	}
}

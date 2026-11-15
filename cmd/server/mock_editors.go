package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func registerMockEditors(mux *http.ServeMux, key string, endpoints map[string]string, gitlab string) {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for name, endpoint := range endpoints {
		target, err := url.Parse(endpoint)
		if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
			continue
		}
		path := "/v1/mock-data/" + name
		target.Path, target.RawPath, target.RawQuery = path, "", ""
		proxy := authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := teamBearer(r); !ok {
				writeError(w, 401, errText("Требуется демосессия."))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
			upstream := *target
			upstream.Path, upstream.RawPath = r.URL.Path, r.URL.RawPath
			req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), r.Body)
			if err != nil {
				writeError(w, 503, errText("Учебный сервис временно недоступен."))
				return
			}
			req.Header.Set("Authorization", r.Header.Get("Authorization"))
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				writeError(w, 503, errText("Учебный сервис временно недоступен."))
				return
			}
			defer resp.Body.Close()
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
				writeError(w, 503, errText("Редактор учебного сервиса временно недоступен."))
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
		}))
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			mux.Handle(method+" "+path, proxy)
		}
		if name != "company-calendar" {
			mux.Handle("PUT "+path+"/{tail...}", proxy)
		}
	}
	if gitlab == "" {
		return
	}
	mux.Handle("GET /v1/tasks/gitlab", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := teamBearer(r)
		if !ok {
			writeError(w, 401, errText("Требуется демосессия."))
			return
		}
		result, err := mcpTeamTool(r.Context(), gitlab, "", token, "gitlab_projects", map[string]any{})
		if err != nil {
			writeError(w, 503, errText("Не удалось загрузить проекты GitLab."))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, result)
	})))
	mux.Handle("GET /v1/tasks/gitlab/{project}/branches", authorize(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := teamBearer(r)
		if !ok {
			writeError(w, 401, errText("Требуется демосессия."))
			return
		}
		project := r.PathValue("project")
		if project == "" || strings.Contains(project, "/") {
			writeError(w, 400, errText("Некорректный проект GitLab."))
			return
		}
		result, err := mcpTeamTool(r.Context(), gitlab, "", token, "gitlab_branches", map[string]any{"project": project})
		if err != nil {
			writeError(w, 503, errText("Не удалось загрузить ветки GitLab."))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, result)
	})))
}

func teamBearer(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token, ok && token != ""
}

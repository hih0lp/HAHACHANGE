package main

import (
	"encoding/json"
	"errors"
	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
)

func registerScenarioRoutes(mux *http.ServeMux, store scenario.Store, api scenario.Client) {
	mux.HandleFunc("POST /v1/scenarios/login", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.Username == "" || input.Password == "" {
			writeError(w, 400, errText("username and password are required"))
			return
		}
		result, err := api.Login(r.Context(), input.Username, input.Password)
		if err != nil {
			scenarioError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, result)
	})
	auth := func(w http.ResponseWriter, r *http.Request) (string, scenario.Profile, bool) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || token == r.Header.Get("Authorization") {
			writeError(w, 401, errText("Bearer token is required"))
			return "", scenario.Profile{}, false
		}
		profile, err := api.Profile(r.Context(), token)
		if err != nil {
			scenarioError(w, err)
			return "", profile, false
		}
		return token, profile, true
	}
	mux.HandleFunc("POST /v1/scenarios", func(w http.ResponseWriter, r *http.Request) {
		token, profile, ok := auth(w, r)
		if !ok {
			return
		}
		id, err := distributed.NewID()
		if err != nil {
			scenarioError(w, err)
			return
		}
		state := scenario.State{SchemaVersion: 1, ID: id, OwnerID: profile.ID, Revision: 1, Goal: "Первая задача → PR", Status: "discovering", Checks: []scenario.Check{}, UpdatedAt: time.Now().UTC()}
		state, err = scenario.Advance(r.Context(), api, token, state, profile)
		if err != nil {
			scenarioError(w, err)
			return
		}
		if err = store.Create(r.Context(), state); err != nil {
			scenarioError(w, err)
			return
		}
		w.Header().Set("Location", "/v1/scenarios/"+id)
		writeJSON(w, 201, state)
	})
	mux.HandleFunc("GET /v1/scenarios/{id}", func(w http.ResponseWriter, r *http.Request) {
		token, profile, ok := auth(w, r)
		if !ok {
			return
		}
		state, err := store.Get(r.Context(), profile.ID, r.PathValue("id"))
		if err != nil {
			scenarioError(w, err)
			return
		}
		if state.Status == "needs_refresh" {
			state, err = scenario.Advance(r.Context(), api, token, state, profile)
			if err == nil {
				state, err = store.Save(r.Context(), state)
			}
			if errors.Is(err, scenario.ErrConflict) {
				state, err = store.Get(r.Context(), profile.ID, r.PathValue("id"))
			}
			if err != nil {
				scenarioError(w, err)
				return
			}
		}
		writeJSON(w, 200, state)
	})
	for _, action := range []string{"advance", "access-requests"} {
		action := action
		mux.HandleFunc("POST /v1/scenarios/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			token, profile, ok := auth(w, r)
			if !ok {
				return
			}
			state, err := store.Get(r.Context(), profile.ID, r.PathValue("id"))
			if err != nil {
				scenarioError(w, err)
				return
			}
			var input struct {
				Revision   int               `json:"revision"`
				Reason     string            `json:"reason"`
				Confirmed  bool              `json:"confirmed"`
				FormValues map[string]string `json:"form_values"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
				writeError(w, 400, errText("invalid input"))
				return
			}
			if input.Revision != state.Revision {
				scenarioError(w, scenario.ErrConflict)
				return
			}
			if action == "access-requests" {
				if state.Widget.Type != "access_request_form" || state.Widget.Resource == nil {
					writeError(w, 409, errText("access request is not the current action"))
					return
				}
				idem := r.Header.Get("Idempotency-Key")
				if !input.Confirmed || len(strings.TrimSpace(input.Reason)) < 2 || len(input.Reason) > 1000 || len(idem) < 8 || len(idem) > 128 {
					writeError(w, 400, errText("confirmation, reason and Idempotency-Key are required"))
					return
				}
				request, err := api.CreateRequest(r.Context(), token, idem, scenario.AccessInput{ResourceID: state.Widget.Resource.ID, Capability: state.Widget.Capability, Reason: input.Reason, Confirmed: true, FormValues: input.FormValues})
				if err != nil {
					scenarioError(w, err)
					return
				}
				// Save the external result before any further remote call. A later refresh
				// checks the actual grant; creation never implies approval.
				state.Status = "waiting_access"
				state.Message = "Заявка отправлена. Ожидаем решения согласующего."
				state.Widget.Type = "approval_status"
				state.Widget.Action = "refresh"
				state.Widget.RequestID = request.ID
			} else {
				state, err = scenario.Advance(r.Context(), api, token, state, profile)
				if err != nil {
					scenarioError(w, err)
					return
				}
			}
			state, err = store.Save(r.Context(), state)
			if err != nil {
				scenarioError(w, err)
				return
			}
			writeJSON(w, 200, state)
		})
	}
}
func scenarioError(w http.ResponseWriter, err error) {
	var remote *scenario.RemoteError
	switch {
	case errors.Is(err, scenario.ErrConflict):
		writeError(w, 409, errText("scenario changed; reload before retrying"))
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, 404, errText("scenario not found"))
	case errors.As(err, &remote):
		status := remote.Status
		if status != 401 && status != 403 && status != 409 && status != 422 {
			status = 503
		}
		writeError(w, status, errText("team access service could not complete the request"))
	default:
		writeError(w, 503, errText("scenario is temporarily unavailable"))
	}
}

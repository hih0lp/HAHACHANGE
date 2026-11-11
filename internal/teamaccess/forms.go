package teamaccess

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
)

type formDefinition struct {
	ID          string          `json:"id"`
	Version     int             `json:"version"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Schema      json.RawMessage `json:"schema"`
	UISchema    json.RawMessage `json:"ui_schema"`
}

func (s *Service) listForms(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT DISTINCT ON (id) id,version,title,description,kind,schema,ui_schema FROM form_definitions WHERE published ORDER BY id,version DESC`)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Forms unavailable")
		return
	}
	defer rows.Close()
	forms := []formDefinition{}
	for rows.Next() {
		var f formDefinition
		if err := rows.Scan(&f.ID, &f.Version, &f.Title, &f.Description, &f.Kind, &f.Schema, &f.UISchema); err != nil {
			fail(w, 503, "dependency_unavailable", "Forms unavailable")
			return
		}
		forms = append(forms, f)
	}
	if rows.Err() != nil {
		fail(w, 503, "dependency_unavailable", "Forms unavailable")
		return
	}
	respond(w, 200, map[string]any{"forms": forms})
}

func (s *Service) form(w http.ResponseWriter, r *http.Request) (formDefinition, bool) {
	var f formDefinition
	err := s.DB.QueryRow(r.Context(), `SELECT id,version,title,description,kind,schema,ui_schema FROM form_definitions WHERE id=$1 AND published ORDER BY version DESC LIMIT 1`, r.PathValue("id")).Scan(&f.ID, &f.Version, &f.Title, &f.Description, &f.Kind, &f.Schema, &f.UISchema)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Form not found")
		return f, false
	}
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Form unavailable")
		return f, false
	}
	return f, true
}

func (s *Service) getForm(w http.ResponseWriter, r *http.Request) {
	if f, ok := s.form(w, r); ok {
		respond(w, 200, f)
	}
}

func validateForm(schemaJSON json.RawMessage, values map[string]any) error {
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	return resolved.Validate(values)
}

func hasExternalReference(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if key == "$ref" || key == "$dynamicRef" {
				ref, ok := item.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					return true
				}
			}
			if hasExternalReference(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if hasExternalReference(item) {
				return true
			}
		}
	}
	return false
}

func (s *Service) validateFormRequest(w http.ResponseWriter, r *http.Request) {
	f, ok := s.form(w, r)
	if !ok {
		return
	}
	var in struct {
		Values map[string]any `json:"values"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := validateForm(f.Schema, in.Values); err != nil {
		fail(w, 400, "invalid_form", err.Error())
		return
	}
	respond(w, 200, map[string]any{"valid": true, "form_id": f.ID, "version": f.Version})
}

func (s *Service) submitForm(w http.ResponseWriter, r *http.Request) {
	f, ok := s.form(w, r)
	if !ok {
		return
	}
	var in struct {
		Values    map[string]any `json:"values"`
		Confirmed bool           `json:"confirmed"`
		Version   int            `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !in.Confirmed || in.Version != f.Version || len(idem) < 8 || len(idem) > 128 {
		fail(w, 400, "invalid_input", "Confirmation, current version and Idempotency-Key required")
		return
	}
	if err := validateForm(f.Schema, in.Values); err != nil {
		fail(w, 400, "invalid_form", err.Error())
		return
	}
	if f.Kind == "access" {
		field := func(key string) string { value, _ := in.Values[key].(string); return value }
		body, _ := json.Marshal(requestInput{ResourceID: field("resource_id"), Capability: field("capability"), Reason: field("reason"), Confirmed: true, FormID: f.ID, FormVersion: f.Version})
		next := r.Clone(r.Context())
		next.Body = io.NopCloser(bytes.NewReader(body))
		s.createRequest(w, next)
		return
	}
	values, _ := json.Marshal(in.Values)
	hash := sha256.Sum256(append([]byte(f.ID+":"+strconv.Itoa(f.Version)+":"), values...))
	inputHash := hex.EncodeToString(hash[:])
	var id, previousHash string
	err := s.DB.QueryRow(r.Context(), `SELECT id,input_hash FROM form_submissions WHERE user_id=$1 AND idempotency_key=$2`, user(r), idem).Scan(&id, &previousHash)
	if err == nil {
		if previousHash != inputHash {
			fail(w, 409, "idempotency_conflict", "Key used for different input")
			return
		}
		respond(w, 200, map[string]any{"id": id, "status": "submitted", "form_id": f.ID, "version": f.Version})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		fail(w, 503, "dependency_unavailable", "Submission unavailable")
		return
	}
	id = newID()
	_, err = s.DB.Exec(r.Context(), `INSERT INTO form_submissions(id,form_id,form_version,user_id,values,idempotency_key,input_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, f.ID, f.Version, user(r), values, idem, inputHash)
	if err != nil {
		fail(w, 409, "duplicate_submission", "Submission could not be saved")
		return
	}
	respond(w, 201, map[string]any{"id": id, "status": "submitted", "form_id": f.ID, "version": f.Version})
}

func (s *Service) getFormSubmission(w http.ResponseWriter, r *http.Request) {
	var id, formID, status string
	var version int
	var created time.Time
	err := s.DB.QueryRow(r.Context(), `SELECT id,form_id,form_version,status,created_at FROM form_submissions WHERE id=$1 AND user_id=$2`, r.PathValue("id"), user(r)).Scan(&id, &formID, &version, &status, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Submission not found")
		return
	}
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Submission unavailable")
		return
	}
	respond(w, 200, map[string]any{"id": id, "form_id": formID, "version": version, "status": status, "created_at": created})
}

func (s *Service) publishForm(w http.ResponseWriter, r *http.Request) {
	// Form publication is restricted to authenticated approvers in the directory.
	if s.teamURL == "" {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(s.teamURL, "/")+"/v1/me", nil)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 403, "forbidden", "Form publisher role required")
		return
	}
	var profile struct {
		Roles []string `json:"roles"`
	}
	if json.NewDecoder(resp.Body).Decode(&profile) != nil {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	allowed := false
	for _, role := range profile.Roles {
		if role == "approver" {
			allowed = true
		}
	}
	if !allowed {
		fail(w, 403, "forbidden", "Form publisher role required")
		return
	}
	var in formDefinition
	if !decode(w, r, &in) {
		return
	}
	if in.ID == "" || in.Title == "" || (in.Kind != "access" && in.Kind != "general") || in.Version < 1 || len(in.Schema) == 0 {
		fail(w, 400, "invalid_input", "Form definition invalid")
		return
	}
	var schema jsonschema.Schema
	var rawSchema any
	if json.Unmarshal(in.Schema, &rawSchema) != nil || hasExternalReference(rawSchema) {
		fail(w, 400, "invalid_schema", "External schema references are not allowed")
		return
	}
	if json.Unmarshal(in.Schema, &schema) != nil {
		fail(w, 400, "invalid_schema", "Schema invalid")
		return
	}
	if _, err := schema.Resolve(nil); err != nil {
		fail(w, 400, "invalid_schema", err.Error())
		return
	}
	if len(in.UISchema) == 0 {
		in.UISchema = json.RawMessage(`{}`)
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO form_definitions(id,version,title,description,kind,schema,ui_schema,published) VALUES($1,$2,$3,$4,$5,$6,$7,true)`, in.ID, in.Version, in.Title, in.Description, in.Kind, in.Schema, in.UISchema)
	if err != nil {
		fail(w, 409, "version_conflict", "Form version already exists")
		return
	}
	respond(w, 201, in)
}

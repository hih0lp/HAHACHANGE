package teamaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, msg string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg, "request_id": newID()}})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		fail(w, 400, "invalid_input", "Invalid JSON input")
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		fail(w, 400, "invalid_input", "Only one JSON object is allowed")
		return false
	}
	return true
}
func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if s.DB.Ping(r.Context()) != nil {
			fail(w, 503, "dependency_unavailable", "Database unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /.well-known/jwks.json", s.jwks)
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("GET /v1/me", s.auth(s.me))
	mux.HandleFunc("GET /v1/teams/{team_id}/members", s.auth(s.members))
	mux.HandleFunc("GET /v1/teams/{team_id}/resources", s.auth(s.resources))
	mux.HandleFunc("POST /v1/access/check", s.auth(s.check))
	mux.HandleFunc("POST /v1/access-requests", s.auth(s.createRequest))
	mux.HandleFunc("GET /v1/access-requests", s.auth(s.listRequests))
	mux.HandleFunc("GET /v1/access-requests/{id}", s.auth(s.getRequest))
	mux.HandleFunc("GET /v1/approvals", s.auth(s.approvals))
	mux.HandleFunc("POST /v1/access-requests/{id}/decision", s.auth(s.decision))
	return mux
}
func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	id := user(r)
	var name string
	if err := s.DB.QueryRow(r.Context(), `SELECT display_name FROM ta_users WHERE id=$1`, id).Scan(&name); err != nil {
		fail(w, 503, "dependency_unavailable", "Profile unavailable")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT t.id,t.name,COALESCE(t.mentor_id,''),COALESCE(t.lead_id,''),m.role FROM ta_memberships m JOIN ta_teams t ON t.id=m.team_id WHERE m.user_id=$1 ORDER BY t.id`, id)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Teams unavailable")
		return
	}
	defer rows.Close()
	teams := []any{}
	roles := []string{}
	current := ""
	for rows.Next() {
		var tid, tn, mentor, lead, role string
		if rows.Scan(&tid, &tn, &mentor, &lead, &role) != nil {
			fail(w, 503, "dependency_unavailable", "Teams unavailable")
			return
		}
		teams = append(teams, map[string]any{"id": tid, "name": tn, "mentor_id": mentor, "lead_id": lead})
		roles = append(roles, role)
		if current == "" {
			current = tid
		}
	}
	if rows.Err() != nil {
		fail(w, 503, "dependency_unavailable", "Teams unavailable")
		return
	}
	respond(w, 200, map[string]any{"id": id, "display_name": name, "roles": roles, "teams": teams, "current_team_id": current})
}

// members returns only the directory of a team the caller belongs to. Possession
// of an arbitrary team ID does not grant access to another team's people.
func (s *Service) members(w http.ResponseWriter, r *http.Request) {
	team := r.PathValue("team_id")
	var allowed bool
	if err := s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ta_memberships WHERE user_id=$1 AND team_id=$2)`, user(r), team).Scan(&allowed); err != nil {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	if !allowed {
		fail(w, 404, "not_found", "Team not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT u.id, u.display_name, m.role, t.mentor_id=u.id, t.lead_id=u.id
		FROM ta_memberships m
		JOIN ta_users u ON u.id=m.user_id
		JOIN ta_teams t ON t.id=m.team_id
		WHERE m.team_id=$1
		ORDER BY (t.lead_id=u.id) DESC, (t.mentor_id=u.id) DESC, u.display_name, u.id`, team)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	defer rows.Close()
	members := []any{}
	for rows.Next() {
		var id, name, role string
		var mentor, lead bool
		if err := rows.Scan(&id, &name, &role, &mentor, &lead); err != nil {
			fail(w, 503, "dependency_unavailable", "Team directory unavailable")
			return
		}
		members = append(members, map[string]any{"id": id, "display_name": name, "role": role, "is_mentor": mentor, "is_lead": lead})
	}
	if err := rows.Err(); err != nil {
		fail(w, 503, "dependency_unavailable", "Team directory unavailable")
		return
	}
	respond(w, 200, map[string]any{"team_id": team, "members": members})
}

func (s *Service) resources(w http.ResponseWriter, r *http.Request) {
	team := r.PathValue("team_id")
	var member bool
	if s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ta_memberships WHERE user_id=$1 AND team_id=$2)`, user(r), team).Scan(&member) != nil {
		fail(w, 503, "dependency_unavailable", "Catalog unavailable")
		return
	}
	if !member {
		fail(w, 404, "not_found", "Team not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,kind,name,external_ref,url,capabilities,approver_id FROM ta_resources WHERE team_id=$1 ORDER BY id`, team)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Catalog unavailable")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, kind, name, ref, url, approver string
		var caps []string
		if rows.Scan(&id, &kind, &name, &ref, &url, &caps, &approver) != nil {
			fail(w, 503, "dependency_unavailable", "Catalog unavailable")
			return
		}
		items = append(items, map[string]any{"id": id, "kind": kind, "name": name, "external_ref": ref, "url": url, "capabilities": caps, "approver_id": approver})
	}
	if rows.Err() != nil {
		fail(w, 503, "dependency_unavailable", "Catalog unavailable")
		return
	}
	respond(w, 200, map[string]any{"resources": items})
}
func (s *Service) resource(w http.ResponseWriter, r *http.Request, id, cap string) (string, string, bool) {
	var team, approver string
	var caps []string
	err := s.DB.QueryRow(r.Context(), `SELECT x.team_id,x.approver_id,x.capabilities FROM ta_resources x JOIN ta_memberships m ON m.team_id=x.team_id AND m.user_id=$1 WHERE x.id=$2`, user(r), id).Scan(&team, &approver, &caps)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Resource not found")
		return "", "", false
	}
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Resource unavailable")
		return "", "", false
	}
	for _, v := range caps {
		if v == cap {
			return team, approver, true
		}
	}
	fail(w, 400, "invalid_capability", "Capability is not available")
	return "", "", false
}
func (s *Service) check(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ResourceID string `json:"resource_id"`
		Capability string `json:"capability"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ResourceID == "" || in.Capability == "" {
		fail(w, 400, "invalid_input", "Resource and capability required")
		return
	}
	if _, _, ok := s.resource(w, r, in.ResourceID, in.Capability); !ok {
		return
	}
	var granted bool
	if s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ta_grants WHERE user_id=$1 AND resource_id=$2 AND capability=$3)`, user(r), in.ResourceID, in.Capability).Scan(&granted) != nil {
		fail(w, 503, "dependency_unavailable", "Access status unavailable")
		return
	}
	state := "missing"
	requestID := ""
	if granted {
		state = "granted"
	} else {
		err := s.DB.QueryRow(r.Context(), `SELECT id FROM ta_requests WHERE user_id=$1 AND resource_id=$2 AND capability=$3 AND status='pending'`, user(r), in.ResourceID, in.Capability).Scan(&requestID)
		if err == nil {
			state = "pending"
		} else if !errors.Is(err, pgx.ErrNoRows) {
			fail(w, 503, "dependency_unavailable", "Access status unavailable")
			return
		}
	}
	out := map[string]any{"state": state, "resource_id": in.ResourceID, "capability": in.Capability, "checked_at": now()}
	if requestID != "" {
		out["request_id"] = requestID
	}
	respond(w, 200, out)
}

type requestInput struct {
	ResourceID string            `json:"resource_id"`
	Capability string            `json:"capability"`
	Reason     string            `json:"reason"`
	FormValues map[string]string `json:"form_values"`
	Confirmed  bool              `json:"confirmed"`
}
type requestRecord struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	ResourceID     string    `json:"resource_id"`
	Capability     string    `json:"capability"`
	ApproverID     string    `json:"approver_id"`
	DecisionReason string    `json:"decision_reason,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func hashInput(in requestInput) string {
	b, _ := json.Marshal(in)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Service) createRequest(w http.ResponseWriter, r *http.Request) {
	var in requestInput
	if !decode(w, r, &in) {
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !in.Confirmed || len(strings.TrimSpace(in.Reason)) < 2 || len(in.Reason) > 1000 || len(idem) < 8 || len(idem) > 128 || in.ResourceID == "" || in.Capability == "" {
		fail(w, 400, "invalid_input", "Confirmation, reason and Idempotency-Key are required")
		return
	}
	team, approver, ok := s.resource(w, r, in.ResourceID, in.Capability)
	if !ok {
		return
	}
	if approver == user(r) {
		fail(w, 403, "forbidden", "Cannot approve own request")
		return
	}
	hash := hashInput(in)
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Storage unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	var existingID, existingHash string
	err = tx.QueryRow(r.Context(), `SELECT id,input_hash FROM ta_requests WHERE user_id=$1 AND idempotency_key=$2`, user(r), idem).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != hash {
			fail(w, 409, "idempotency_conflict", "Key used for different input")
			return
		}
		tx.Commit(r.Context())
		s.getByID(w, r, existingID)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		fail(w, 503, "dependency_unavailable", "Storage unavailable")
		return
	}
	var granted bool
	if tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ta_grants WHERE user_id=$1 AND resource_id=$2 AND capability=$3)`, user(r), in.ResourceID, in.Capability).Scan(&granted) != nil {
		fail(w, 503, "dependency_unavailable", "Storage unavailable")
		return
	}
	if granted {
		fail(w, 409, "already_granted", "Access already granted")
		return
	}
	form, _ := json.Marshal(in.FormValues)
	id := newID()
	_, err = tx.Exec(r.Context(), `INSERT INTO ta_requests(id,user_id,resource_id,capability,approver_id,reason,form_values,status,idempotency_key,input_hash) VALUES($1,$2,$3,$4,$5,$6,$7,'pending',$8,$9)`, id, user(r), in.ResourceID, in.Capability, approver, in.Reason, form, idem, hash)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			fail(w, 409, "duplicate_request", "Pending request or idempotency key already exists")
			return
		}
		fail(w, 503, "dependency_unavailable", "Storage unavailable")
		return
	}
	if err = s.event(r.Context(), tx, id, "access.requested", user(r), team, in.ResourceID, in.Capability, 1); err != nil {
		fail(w, 503, "dependency_unavailable", "Event storage unavailable")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO ta_audit(actor_id,subject_id,request_id,action) VALUES($1,$1,$2,'access.requested')`, user(r), id)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Audit unavailable")
		return
	}
	if tx.Commit(r.Context()) != nil {
		fail(w, 503, "dependency_unavailable", "Storage unavailable")
		return
	}
	w.Header().Set("Location", "/v1/access-requests/"+id)
	s.getByIDWithStatus(w, r, id, 201)
}
func (s *Service) getByIDWithStatus(w http.ResponseWriter, r *http.Request, id string, status int) {
	var v requestRecord
	err := s.DB.QueryRow(r.Context(), `SELECT id,status,resource_id,capability,approver_id,decision_reason,created_at,updated_at FROM ta_requests WHERE id=$1 AND (user_id=$2 OR approver_id=$2)`, id, user(r)).Scan(&v.ID, &v.Status, &v.ResourceID, &v.Capability, &v.ApproverID, &v.DecisionReason, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Request not found")
		return
	}
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Request unavailable")
		return
	}
	respond(w, status, v)
}
func (s *Service) getByID(w http.ResponseWriter, r *http.Request, id string) {
	s.getByIDWithStatus(w, r, id, 200)
}
func (s *Service) getRequest(w http.ResponseWriter, r *http.Request) {
	s.getByID(w, r, r.PathValue("id"))
}
func (s *Service) listRequests(w http.ResponseWriter, r *http.Request) { s.list(w, r, false) }
func (s *Service) approvals(w http.ResponseWriter, r *http.Request)    { s.list(w, r, true) }
func (s *Service) list(w http.ResponseWriter, r *http.Request, approvals bool) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			fail(w, 400, "invalid_limit", "limit must be 1..100")
			return
		}
		limit = n
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 {
			fail(w, 400, "invalid_offset", "offset must be nonnegative")
			return
		}
		offset = n
	}
	query := `SELECT id,status,resource_id,capability,approver_id,decision_reason,created_at,updated_at FROM ta_requests WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	if approvals {
		query = `SELECT id,status,resource_id,capability,approver_id,decision_reason,created_at,updated_at FROM ta_requests WHERE approver_id=$1 AND status='pending' ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	}
	rows, err := s.DB.Query(r.Context(), query, user(r), limit, offset)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Requests unavailable")
		return
	}
	defer rows.Close()
	items := []requestRecord{}
	for rows.Next() {
		var v requestRecord
		if rows.Scan(&v.ID, &v.Status, &v.ResourceID, &v.Capability, &v.ApproverID, &v.DecisionReason, &v.CreatedAt, &v.UpdatedAt) != nil {
			fail(w, 503, "dependency_unavailable", "Requests unavailable")
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		fail(w, 503, "dependency_unavailable", "Requests unavailable")
		return
	}
	respond(w, 200, map[string]any{"requests": items, "limit": limit, "offset": offset})
}
func (s *Service) decision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Decision != "approve" && in.Decision != "reject" {
		fail(w, 400, "invalid_decision", "Decision must be approve or reject")
		return
	}
	if len(in.Comment) > 1000 {
		fail(w, 400, "invalid_comment", "Comment too long")
		return
	}
	id := r.PathValue("id")
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Storage unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	var subject, resource, cap, approver, status, team string
	var revision int64
	err = tx.QueryRow(r.Context(), `SELECT q.user_id,q.resource_id,q.capability,q.approver_id,q.status,q.revision,x.team_id FROM ta_requests q JOIN ta_resources x ON x.id=q.resource_id WHERE q.id=$1 FOR UPDATE OF q`, id).Scan(&subject, &resource, &cap, &approver, &status, &revision, &team)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Request not found")
		return
	}
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Request unavailable")
		return
	}
	if approver != user(r) {
		fail(w, 404, "not_found", "Request not found")
		return
	}
	if subject == user(r) {
		fail(w, 403, "forbidden", "Cannot decide own request")
		return
	}
	var member bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ta_memberships WHERE user_id=$1 AND team_id=$2 AND role='approver')`, user(r), team).Scan(&member)
	if err != nil {
		fail(w, 503, "dependency_unavailable", "Membership unavailable")
		return
	}
	if !member {
		fail(w, 403, "forbidden", "Approver role required")
		return
	}
	if status != "pending" {
		fail(w, 422, "invalid_transition", "Request already decided")
		return
	}
	next := "approved"
	event := "access.approved"
	if in.Decision == "reject" {
		next = "rejected"
		event = "access.rejected"
	}
	_, err = tx.Exec(r.Context(), `UPDATE ta_requests SET status=$2,decision_reason=$3,revision=revision+1,updated_at=now() WHERE id=$1`, id, next, in.Comment)
	if err == nil && next == "approved" {
		_, err = tx.Exec(r.Context(), `INSERT INTO ta_grants(user_id,resource_id,capability,granted_by) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,resource_id,capability) DO UPDATE SET granted_by=$4,granted_at=now()`, subject, resource, cap, user(r))
	}
	if err == nil {
		err = s.event(r.Context(), tx, id, event, subject, team, resource, cap, revision+1)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO ta_audit(actor_id,subject_id,request_id,action) VALUES($1,$2,$3,$4)`, user(r), subject, id, event)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		fail(w, 503, "dependency_unavailable", "Decision could not be saved")
		return
	}
	s.getByID(w, r, id)
}

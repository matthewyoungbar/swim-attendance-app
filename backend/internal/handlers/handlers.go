package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	walib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/matthewyoungbar/swim-attendance-app/internal/auth"
	"github.com/matthewyoungbar/swim-attendance-app/internal/calendar"
	"github.com/matthewyoungbar/swim-attendance-app/internal/db"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

type contextKey string

const contextKeyEmail contextKey = "email"

type Handler struct {
	db  *db.Client
	cal *calendar.Client
	wa  *walib.WebAuthn
}

func New(dbClient *db.Client, calClient *calendar.Client, wa *walib.WebAuthn) *Handler {
	return &Handler{db: dbClient, cal: calClient, wa: wa}
}

func emailFromCtx(r *http.Request) string {
	v, _ := r.Context().Value(contextKeyEmail).(string)
	return v
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return claims.Email, true
}

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	email, ok := h.requireAuth(w, r)
	if !ok {
		return "", false
	}
	user, err := h.db.GetUser(r.Context(), email)
	if err != nil || user == nil || !user.IsAdmin {
		jsonError(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	return email, true
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/api")
	log.Printf("%s %s", r.Method, path)

	// Public auth routes
	switch {
	case r.Method == http.MethodGet && path == "/auth/check":
		h.checkUser(w, r)
		return
	case r.Method == http.MethodPost && path == "/auth/register/begin":
		h.registerBegin(w, r)
		return
	case r.Method == http.MethodPost && path == "/auth/register/complete":
		h.registerComplete(w, r)
		return
	case r.Method == http.MethodPost && path == "/auth/login/begin":
		h.loginBegin(w, r)
		return
	case r.Method == http.MethodPost && path == "/auth/login/complete":
		h.loginComplete(w, r)
		return
	case r.Method == http.MethodGet && path == "/auth/me":
		h.me(w, r)
		return
	}

	// All remaining routes require a valid JWT
	email, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	ctx := context.WithValue(r.Context(), contextKeyEmail, email)
	r = r.WithContext(ctx)

	switch {
	case r.Method == http.MethodGet && path == "/practices":
		h.listPractices(w, r)
	case r.Method == http.MethodPost && path == "/practices/sync":
		if _, ok := h.requireAdmin(w, r); !ok {
			return
		}
		h.syncPractices(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/practices/") && strings.HasSuffix(path, "/signups"):
		parts := strings.Split(path, "/")
		if len(parts) == 4 {
			h.listSignups(w, r, parts[2])
		} else {
			jsonError(w, "not found", http.StatusNotFound)
		}
	case r.Method == http.MethodPost && path == "/signups":
		h.createSignup(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/signups/"):
		practiceID := strings.TrimPrefix(path, "/signups/")
		h.deleteSignup(w, r, practiceID)
	case r.Method == http.MethodGet && path == "/my-signups":
		h.mySignups(w, r)
	case r.Method == http.MethodGet && path == "/swimmers":
		h.listSwimmers(w, r)
	case r.Method == http.MethodGet && path == "/practices/all":
		h.listAllPractices(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/attendance/"):
		h.getAttendance(w, r, strings.TrimPrefix(path, "/attendance/"))
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/attendance/"):
		h.saveAttendance(w, r, strings.TrimPrefix(path, "/attendance/"))
	case r.Method == http.MethodGet && path == "/passkeys":
		h.listPasskeys(w, r)
	case r.Method == http.MethodPost && path == "/passkeys/add/begin":
		h.addPasskeyBegin(w, r)
	case r.Method == http.MethodPost && path == "/passkeys/add/complete":
		h.addPasskeyComplete(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/passkeys/"):
		h.deletePasskey(w, r, strings.TrimPrefix(path, "/passkeys/"))
	case r.Method == http.MethodPost && path == "/admin/import-roster":
		h.importRoster(w, r)
	case r.Method == http.MethodGet && path == "/users":
		h.listUsers(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/users/") && strings.HasSuffix(path, "/roles"):
		target := strings.TrimSuffix(strings.TrimPrefix(path, "/users/"), "/roles")
		h.updateUserRoles(w, r, target)
	default:
		jsonError(w, "not found", http.StatusNotFound)
	}
}

// GET /auth/me
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	user, err := h.db.GetUser(r.Context(), email)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	jsonOK(w, user)
}

// GET /practices
func (h *Handler) listPractices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := emailFromCtx(r)

	practices, err := h.db.GetPractices(ctx)
	if err != nil {
		log.Printf("ERROR listPractices: %v", err)
		jsonError(w, "failed to fetch practices", http.StatusInternalServerError)
		return
	}

	var mySignupIDs map[string]bool
	if email != "" {
		if signups, err := h.db.GetSignupsForSwimmer(ctx, email); err == nil {
			mySignupIDs = make(map[string]bool, len(signups))
			for _, s := range signups {
				mySignupIDs[s.PracticeID] = true
			}
		}
	}

	result := make([]models.PracticeWithSignups, 0, len(practices))
	for _, p := range practices {
		result = append(result, models.PracticeWithSignups{
			Practice:   p,
			IsSignedUp: mySignupIDs[p.ID],
		})
	}
	jsonOK(w, result)
}

// RunSync fetches upcoming practices from the calendar and upserts them into the DB.
// It is called both by the HTTP handler and by the CloudWatch scheduled event handler.
func (h *Handler) RunSync(ctx context.Context) (map[string]int, error) {
	if h.cal == nil {
		return nil, fmt.Errorf("calendar not configured")
	}

	practices, err := h.cal.FetchUpcomingPractices(ctx, 7)
	if err != nil {
		return nil, fmt.Errorf("fetch from calendar: %w", err)
	}

	coachIndex := h.buildCoachIndex(ctx)

	var synced, failed, coachMatched int
	for _, p := range practices {
		p.CoachID, p.CoachName = matchCoach(p.Title, coachIndex)
		if p.CoachID != "" {
			coachMatched++
		}
		if err := h.db.UpsertPractice(ctx, p); err != nil {
			log.Printf("ERROR upsert practice %s: %v", p.ID, err)
			failed++
		} else {
			synced++
		}
	}
	log.Printf("Sync complete: %d synced, %d coach-matched, %d failed", synced, coachMatched, failed)
	return map[string]int{"synced": synced, "coachMatched": coachMatched, "failed": failed}, nil
}

// buildCoachIndex returns a map of lowercase name variants → (email, display name)
// for every user marked as a coach.
func (h *Handler) buildCoachIndex(ctx context.Context) map[string][2]string {
	coaches, err := h.db.ListCoaches(ctx)
	if err != nil {
		log.Printf("WARN buildCoachIndex: %v", err)
		return nil
	}
	log.Printf("Coach index: %d coaches loaded", len(coaches))
	index := make(map[string][2]string, len(coaches)*4)
	for _, c := range coaches {
		display := c.FirstName + " " + c.LastName
		if c.PreferredName != "" {
			display = c.PreferredName + " " + c.LastName
		}
		pk := "USER#" + c.Email
		entry := [2]string{pk, display}
		// Register all name forms so partial titles still match.
		for _, name := range []string{
			c.FirstName,
			c.PreferredName,
			c.FirstName + " " + c.LastName,
			c.PreferredName + " " + c.LastName,
		} {
			if name = strings.TrimSpace(strings.ToLower(name)); name != "" {
				index[name] = entry
				log.Printf("Coach index: registered %q → %s", name, pk)
			}
		}
	}
	return index
}

// separators covers the hyphen/dash variants commonly produced by calendar apps.
var separators = []string{" - ", " — ", " – ", " - "} // hyphen, em dash, en dash, figure dash

// matchCoach extracts the coach name from a title of the form "<name> - <location>"
// and looks it up in the index. Returns empty strings if no match is found.
func matchCoach(title string, index map[string][2]string) (coachID, coachName string) {
	if index == nil {
		return "", ""
	}
	var before string
	var found bool
	for _, sep := range separators {
		if before, _, found = strings.Cut(title, sep); found {
			break
		}
	}
	if !found {
		log.Printf("Coach match: no separator found in title %q", title)
		return "", ""
	}
	key := strings.ToLower(strings.TrimSpace(before))
	if entry, ok := index[key]; ok {
		log.Printf("Coach match: %q → %s", key, entry[0])
		return entry[0], entry[1]
	}
	log.Printf("Coach match: no match for extracted name %q (title: %q)", key, title)
	return "", ""
}

// POST /practices/sync
func (h *Handler) syncPractices(w http.ResponseWriter, r *http.Request) {
	result, err := h.RunSync(r.Context())
	if err != nil {
		log.Printf("ERROR syncPractices: %v", err)
		if err.Error() == "calendar not configured" {
			jsonError(w, "calendar not configured", http.StatusServiceUnavailable)
		} else {
			jsonError(w, "failed to fetch from calendar", http.StatusInternalServerError)
		}
		return
	}
	jsonOK(w, result)
}

// GET /practices/{id}/signups
func (h *Handler) listSignups(w http.ResponseWriter, r *http.Request, practiceID string) {
	ctx := r.Context()
	signups, err := h.db.GetSignupsForPractice(ctx, practiceID)
	if err != nil {
		log.Printf("ERROR listSignups: %v", err)
		jsonError(w, "failed to fetch signups", http.StatusInternalServerError)
		return
	}
	jsonOK(w, signups)
}

// POST /signups
func (h *Handler) createSignup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	swimmerEmail := emailFromCtx(r)

	var req models.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PracticeID == "" {
		jsonError(w, "practiceId is required", http.StatusBadRequest)
		return
	}

	user, err := h.db.GetUser(ctx, swimmerEmail)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	if !user.IsActive {
		jsonError(w, "your account is not active", http.StatusForbidden)
		return
	}
	swimmerName := user.FirstName + " " + user.LastName
	if user.PreferredName != "" {
		swimmerName = user.PreferredName + " " + user.LastName
	}

	practice, err := h.db.GetPractice(ctx, req.PracticeID)
	if err != nil || practice == nil {
		jsonError(w, "practice not found", http.StatusNotFound)
		return
	}
	if practice.Capacity > 0 && practice.SignupCount >= practice.Capacity {
		jsonError(w, "practice is full", http.StatusConflict)
		return
	}
	if practice.StartTime.Before(time.Now()) {
		jsonError(w, "practice has already started", http.StatusBadRequest)
		return
	}

	signup := models.Signup{
		PracticeID:   req.PracticeID,
		SwimmerEmail: swimmerEmail,
		SwimmerName:  swimmerName,
		RegisteredAt: time.Now().UTC(),
		Notes:        req.Notes,
	}
	if err := h.db.CreateSignup(ctx, signup); err != nil {
		if err.Error() == "already_signed_up" {
			jsonError(w, "you are already signed up for this practice", http.StatusConflict)
			return
		}
		log.Printf("ERROR createSignup: %v", err)
		jsonError(w, "failed to create signup", http.StatusInternalServerError)
		return
	}

	if err := h.db.IncrementSignupCount(ctx, req.PracticeID, 1); err != nil {
		log.Printf("WARN failed to increment signup count: %v", err)
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, signup)
}

// DELETE /signups/{practiceId}
func (h *Handler) deleteSignup(w http.ResponseWriter, r *http.Request, practiceID string) {
	ctx := r.Context()
	swimmerEmail := emailFromCtx(r)

	existing, err := h.db.GetSignup(ctx, practiceID, swimmerEmail)
	if err != nil || existing == nil {
		jsonError(w, "signup not found", http.StatusNotFound)
		return
	}

	if err := h.db.DeleteSignup(ctx, practiceID, swimmerEmail); err != nil {
		log.Printf("ERROR deleteSignup: %v", err)
		jsonError(w, "failed to cancel signup", http.StatusInternalServerError)
		return
	}

	if err := h.db.IncrementSignupCount(ctx, practiceID, -1); err != nil {
		log.Printf("WARN failed to decrement signup count: %v", err)
	}
	jsonOK(w, map[string]string{"message": "signup cancelled"})
}

// GET /my-signups
func (h *Handler) mySignups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := emailFromCtx(r)

	signups, err := h.db.GetSignupsForSwimmer(ctx, email)
	if err != nil {
		log.Printf("ERROR mySignups: %v", err)
		jsonError(w, "failed to fetch signups", http.StatusInternalServerError)
		return
	}
	jsonOK(w, signups)
}

// GET /users  (admin only)
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	users, err := h.db.ListUsers(r.Context())
	if err != nil {
		log.Printf("ERROR listUsers: %v", err)
		jsonError(w, "failed to fetch users", http.StatusInternalServerError)
		return
	}
	jsonOK(w, users)
}

// PUT /users/{email}/roles  (admin only)
func (h *Handler) updateUserRoles(w http.ResponseWriter, r *http.Request, targetEmail string) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var req models.UpdateRolesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.db.UpdateUserRoles(r.Context(), targetEmail, req.IsAdmin, req.IsCoach, req.IsActive); err != nil {
		if err.Error() == "user_not_found" {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("ERROR updateUserRoles: %v", err)
		jsonError(w, "failed to update roles", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"message": "roles updated"})
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.APIResponse{Success: true, Data: data})
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: false,
		Error:   fmt.Sprintf("%s", msg),
	})
}

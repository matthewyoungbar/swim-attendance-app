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
	default:
		jsonError(w, "not found", http.StatusNotFound)
	}
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

// POST /practices/sync
func (h *Handler) syncPractices(w http.ResponseWriter, r *http.Request) {
	if h.cal == nil {
		jsonError(w, "calendar not configured", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()

	practices, err := h.cal.FetchUpcomingPractices(ctx, 7)
	if err != nil {
		log.Printf("ERROR syncPractices fetch: %v", err)
		jsonError(w, "failed to fetch from calendar", http.StatusInternalServerError)
		return
	}

	var synced, failed int
	for _, p := range practices {
		if err := h.db.UpsertPractice(ctx, p); err != nil {
			log.Printf("ERROR upsert practice %s: %v", p.ID, err)
			failed++
		} else {
			synced++
		}
	}
	jsonOK(w, map[string]int{"synced": synced, "failed": failed})
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

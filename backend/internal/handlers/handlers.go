package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/matthewyoungbar/swim-attendance-app/internal/calendar"
	"github.com/matthewyoungbar/swim-attendance-app/internal/db"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

// Handler holds all dependencies and implements http.Handler.
type Handler struct {
	db  *db.Client
	cal *calendar.Client
}

func New(dbClient *db.Client, calClient *calendar.Client) *Handler {
	return &Handler{db: dbClient, cal: calClient}
}

// ServeHTTP routes requests to the correct handler method.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS headers (adjust origin in production)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Swimmer-Email")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimSuffix(r.URL.Path, "/")
	log.Printf("%s %s", r.Method, path)

	switch {
	case r.Method == http.MethodGet && path == "/practices":
		h.listPractices(w, r)

	case r.Method == http.MethodPost && path == "/practices/sync":
		h.syncPractices(w, r)

	case r.Method == http.MethodGet && strings.HasPrefix(path, "/practices/") && strings.HasSuffix(path, "/signups"):
		// GET /practices/{id}/signups
		parts := strings.Split(path, "/")
		if len(parts) == 4 {
			h.listSignups(w, r, parts[2])
		} else {
			jsonError(w, "not found", http.StatusNotFound)
		}

	case r.Method == http.MethodPost && path == "/signups":
		h.createSignup(w, r)

	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/signups/"):
		// DELETE /signups/{practiceId}  (swimmer email from header)
		practiceID := strings.TrimPrefix(path, "/signups/")
		h.deleteSignup(w, r, practiceID)

	case r.Method == http.MethodGet && path == "/my-signups":
		h.mySignups(w, r)

	default:
		jsonError(w, "not found", http.StatusNotFound)
	}
}

// GET /practices
// Query params: ?email=swimmer@example.com (to check signup status)
func (h *Handler) listPractices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	practices, err := h.db.GetPractices(ctx)
	if err != nil {
		log.Printf("ERROR listPractices: %v", err)
		jsonError(w, "failed to fetch practices", http.StatusInternalServerError)
		return
	}

	// Optionally check signup status for a given swimmer
	email := r.URL.Query().Get("email")
	var mySignupIDs map[string]bool
	if email != "" {
		signups, err := h.db.GetSignupsForSwimmer(ctx, email)
		if err == nil {
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

// POST /practices/sync  — syncs next 60 days from Google Calendar into DynamoDB
func (h *Handler) syncPractices(w http.ResponseWriter, r *http.Request) {
	if h.cal == nil {
		jsonError(w, "calendar not configured", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()

	practices, err := h.cal.FetchUpcomingPractices(ctx, 60)
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

	var req models.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PracticeID == "" || req.SwimmerEmail == "" || req.SwimmerName == "" {
		jsonError(w, "practiceId, swimmerEmail, and swimmerName are required", http.StatusBadRequest)
		return
	}

	// Check practice exists and has capacity
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
		SwimmerEmail: req.SwimmerEmail,
		SwimmerName:  req.SwimmerName,
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

	// Increment signup count
	if err := h.db.IncrementSignupCount(ctx, req.PracticeID, 1); err != nil {
		log.Printf("WARN failed to increment signup count: %v", err)
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, signup)
}

// DELETE /signups/{practiceId}  — swimmer email read from X-Swimmer-Email header
func (h *Handler) deleteSignup(w http.ResponseWriter, r *http.Request, practiceID string) {
	ctx := r.Context()
	swimmerEmail := r.Header.Get("X-Swimmer-Email")
	if swimmerEmail == "" {
		swimmerEmail = r.URL.Query().Get("email")
	}
	if swimmerEmail == "" {
		jsonError(w, "X-Swimmer-Email header or ?email= required", http.StatusBadRequest)
		return
	}

	// Check signup exists
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

	// Decrement signup count
	if err := h.db.IncrementSignupCount(ctx, practiceID, -1); err != nil {
		log.Printf("WARN failed to decrement signup count: %v", err)
	}

	jsonOK(w, map[string]string{"message": "signup cancelled"})
}

// GET /my-signups?email=swimmer@example.com
func (h *Handler) mySignups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := r.URL.Query().Get("email")
	if email == "" {
		jsonError(w, "email query param required", http.StatusBadRequest)
		return
	}

	signups, err := h.db.GetSignupsForSwimmer(ctx, email)
	if err != nil {
		log.Printf("ERROR mySignups: %v", err)
		jsonError(w, "failed to fetch signups", http.StatusInternalServerError)
		return
	}
	jsonOK(w, signups)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

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

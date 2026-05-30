package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	walib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/matthewyoungbar/swim-attendance-app/internal/auth"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

// sessionBlob is stored in DynamoDB for the duration of a WebAuthn ceremony.
type sessionBlob struct {
	Profile *registrationProfile `json:"profile,omitempty"` // set only for registration
	Session walib.SessionData    `json:"session"`
}

type registrationProfile struct {
	Email         string `json:"email"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	PreferredName string `json:"preferredName,omitempty"`
	Phone         string `json:"phone,omitempty"`
	WebAuthnID    []byte `json:"webAuthnId"`
}

// webAuthnUser implements webauthn.User.
type webAuthnUser struct {
	id          []byte
	email       string
	displayName string
	credentials []walib.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte                      { return u.id }
func (u *webAuthnUser) WebAuthnName() string                    { return u.email }
func (u *webAuthnUser) WebAuthnDisplayName() string             { return u.displayName }
func (u *webAuthnUser) WebAuthnCredentials() []walib.Credential { return u.credentials }
func (u *webAuthnUser) WebAuthnIcon() string                    { return "" }

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// GET /auth/check?email=
func (h *Handler) checkUser(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		jsonError(w, "email required", http.StatusBadRequest)
		return
	}
	user, err := h.db.GetUser(r.Context(), email)
	if err != nil {
		log.Printf("ERROR checkUser: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		jsonOK(w, map[string]interface{}{"exists": false})
		return
	}
	passkeys, _ := h.db.GetPasskeys(r.Context(), user.WebAuthnID)
	jsonOK(w, map[string]interface{}{
		"exists":        true,
		"hasPasskey":    len(passkeys) > 0,
		"firstName":     user.FirstName,
		"lastName":      user.LastName,
		"preferredName": user.PreferredName,
	})
}

// POST /auth/register/begin
func (h *Handler) registerBegin(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.FirstName == "" || req.LastName == "" {
		jsonError(w, "email, firstName, and lastName are required", http.StatusBadRequest)
		return
	}

	var waID []byte
	existing, _ := h.db.GetUser(r.Context(), req.Email)
	if existing != nil {
		passkeys, _ := h.db.GetPasskeys(r.Context(), existing.WebAuthnID)
		if len(passkeys) > 0 {
			jsonError(w, "user already exists", http.StatusConflict)
			return
		}
		// Pre-created user (imported) with no passkey yet — reuse their WebAuthnID.
		waID = existing.WebAuthnID
	} else {
		waID = make([]byte, 16)
		rand.Read(waID)
	}

	displayName := req.FirstName + " " + req.LastName
	if req.PreferredName != "" {
		displayName = req.PreferredName
	}
	waUser := &webAuthnUser{id: waID, email: req.Email, displayName: displayName}

	// Require preferred resident key so the passkey is discoverable without an email prompt.
	options, sessionData, err := h.wa.BeginRegistration(waUser,
		walib.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
	)
	if err != nil {
		log.Printf("ERROR registerBegin: %v", err)
		jsonError(w, "failed to begin registration", http.StatusInternalServerError)
		return
	}

	blob := sessionBlob{
		Profile: &registrationProfile{
			Email:         req.Email,
			FirstName:     req.FirstName,
			LastName:      req.LastName,
			PreferredName: req.PreferredName,
			Phone:         req.Phone,
			WebAuthnID:    waID,
		},
		Session: *sessionData,
	}
	blobJSON, _ := json.Marshal(blob)
	sessionID := newSessionID()
	if err := h.db.SaveWebAuthnSession(r.Context(), sessionID, blobJSON); err != nil {
		log.Printf("ERROR registerBegin save session: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]interface{}{"sessionId": sessionID, "options": options})
}

// POST /auth/register/complete
func (h *Handler) registerComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID  string          `json:"sessionId"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	blobJSON, err := h.db.GetWebAuthnSession(r.Context(), req.SessionID)
	if err != nil {
		jsonError(w, "session not found or expired", http.StatusBadRequest)
		return
	}
	var blob sessionBlob
	json.Unmarshal(blobJSON, &blob)
	if blob.Profile == nil {
		jsonError(w, "invalid session type", http.StatusBadRequest)
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		jsonError(w, "invalid credential: "+err.Error(), http.StatusBadRequest)
		return
	}

	waUser := &webAuthnUser{
		id:    blob.Profile.WebAuthnID,
		email: blob.Profile.Email,
		displayName: func() string {
			if blob.Profile.PreferredName != "" {
				return blob.Profile.PreferredName
			}
			return blob.Profile.FirstName + " " + blob.Profile.LastName
		}(),
	}
	cred, err := h.wa.CreateCredential(waUser, blob.Session, parsed)
	if err != nil {
		jsonError(w, "registration failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	var responseUser models.User
	existingUser, _ := h.db.GetUser(r.Context(), blob.Profile.Email)
	if existingUser == nil {
		newUser := models.User{
			Email:         blob.Profile.Email,
			FirstName:     blob.Profile.FirstName,
			LastName:      blob.Profile.LastName,
			PreferredName: blob.Profile.PreferredName,
			Phone:         blob.Profile.Phone,
			WebAuthnID:    blob.Profile.WebAuthnID,
			CreatedAt:     time.Now().UTC(),
			IsActive:      true,
		}
		if err := h.db.CreateUser(r.Context(), newUser); err != nil {
			log.Printf("ERROR registerComplete CreateUser: %v", err)
			jsonError(w, "failed to create user", http.StatusInternalServerError)
			return
		}
		responseUser = newUser
	} else {
		// Pre-created user completing their passkey setup — update profile with submitted data.
		h.db.UpdateUserProfile(r.Context(), blob.Profile.Email,
			blob.Profile.FirstName, blob.Profile.LastName,
			blob.Profile.PreferredName, blob.Profile.Phone)
		responseUser = *existingUser
		responseUser.FirstName = blob.Profile.FirstName
		responseUser.LastName = blob.Profile.LastName
		responseUser.PreferredName = blob.Profile.PreferredName
		responseUser.Phone = blob.Profile.Phone
	}

	if err := h.db.SavePasskey(r.Context(), blob.Profile.WebAuthnID, blob.Profile.Email, *cred); err != nil {
		log.Printf("ERROR registerComplete SavePasskey: %v", err)
		jsonError(w, "failed to save passkey", http.StatusInternalServerError)
		return
	}

	h.db.DeleteWebAuthnSession(r.Context(), req.SessionID)

	token, err := auth.IssueToken(responseUser.Email)
	if err != nil {
		log.Printf("ERROR registerComplete IssueToken: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]interface{}{"token": token, "user": responseUser})
}

// POST /auth/login/begin — starts a discoverable (email-free) passkey login.
func (h *Handler) loginBegin(w http.ResponseWriter, r *http.Request) {
	options, sessionData, err := h.wa.BeginDiscoverableLogin()
	if err != nil {
		log.Printf("ERROR loginBegin: %v", err)
		jsonError(w, "failed to begin login", http.StatusInternalServerError)
		return
	}

	blob := sessionBlob{Session: *sessionData}
	blobJSON, _ := json.Marshal(blob)
	sessionID := newSessionID()
	if err := h.db.SaveWebAuthnSession(r.Context(), sessionID, blobJSON); err != nil {
		log.Printf("ERROR loginBegin save session: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]interface{}{"sessionId": sessionID, "options": options})
}

// POST /auth/login/complete
func (h *Handler) loginComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID  string          `json:"sessionId"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	blobJSON, err := h.db.GetWebAuthnSession(r.Context(), req.SessionID)
	if err != nil {
		jsonError(w, "session not found or expired", http.StatusBadRequest)
		return
	}
	var blob sessionBlob
	json.Unmarshal(blobJSON, &blob)

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		jsonError(w, "invalid credential: "+err.Error(), http.StatusBadRequest)
		return
	}

	var resolvedUser *models.User

	handler := func(rawID, userHandle []byte) (walib.User, error) {
		passkeys, err := h.db.GetPasskeys(r.Context(), userHandle)
		if err != nil || len(passkeys) == 0 {
			return nil, fmt.Errorf("user not found")
		}
		user, err := h.db.GetUser(r.Context(), passkeys[0].UserEmail)
		if err != nil || user == nil {
			return nil, fmt.Errorf("user not found")
		}
		creds := make([]walib.Credential, 0, len(passkeys))
		for _, pk := range passkeys {
			var cred walib.Credential
			if err := json.Unmarshal([]byte(pk.CredentialJSON), &cred); err == nil {
				creds = append(creds, cred)
			}
		}
		resolvedUser = user
		return &webAuthnUser{
			id:          user.WebAuthnID,
			email:       user.Email,
			displayName: user.FirstName + " " + user.LastName,
			credentials: creds,
		}, nil
	}

	updatedCred, err := h.wa.ValidateDiscoverableLogin(handler, blob.Session, parsed)
	if err != nil {
		jsonError(w, "login failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	if resolvedUser == nil {
		jsonError(w, "login failed", http.StatusUnauthorized)
		return
	}

	if err := h.db.UpdatePasskey(r.Context(), resolvedUser.WebAuthnID, *updatedCred); err != nil {
		log.Printf("WARN loginComplete UpdatePasskey: %v", err)
	}
	h.db.DeleteWebAuthnSession(r.Context(), req.SessionID)

	token, err := auth.IssueToken(resolvedUser.Email)
	if err != nil {
		log.Printf("ERROR loginComplete IssueToken: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]interface{}{"token": token, "user": resolvedUser})
}

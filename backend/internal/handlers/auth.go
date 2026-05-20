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
	Email   string               `json:"email"`
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

func (u *webAuthnUser) WebAuthnID() []byte                        { return u.id }
func (u *webAuthnUser) WebAuthnName() string                      { return u.email }
func (u *webAuthnUser) WebAuthnDisplayName() string               { return u.displayName }
func (u *webAuthnUser) WebAuthnCredentials() []walib.Credential   { return u.credentials }
func (u *webAuthnUser) WebAuthnIcon() string                      { return "" }

func newSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func unmarshalCredentials(s string) ([]walib.Credential, error) {
	if s == "" {
		return nil, nil
	}
	var creds []walib.Credential
	return creds, json.Unmarshal([]byte(s), &creds)
}

func updateCredential(creds []walib.Credential, updated walib.Credential) []walib.Credential {
	for i, c := range creds {
		if bytes.Equal(c.ID, updated.ID) {
			creds[i] = updated
			return creds
		}
	}
	return append(creds, updated)
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
	jsonOK(w, map[string]bool{"exists": user != nil})
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

	existing, _ := h.db.GetUser(r.Context(), req.Email)
	if existing != nil {
		jsonError(w, "user already exists", http.StatusConflict)
		return
	}

	waID := make([]byte, 16)
	rand.Read(waID)

	displayName := req.FirstName + " " + req.LastName
	if req.PreferredName != "" {
		displayName = req.PreferredName
	}
	waUser := &webAuthnUser{id: waID, email: req.Email, displayName: displayName}

	options, sessionData, err := h.wa.BeginRegistration(waUser)
	if err != nil {
		log.Printf("ERROR registerBegin: %v", err)
		jsonError(w, "failed to begin registration", http.StatusInternalServerError)
		return
	}

	blob := sessionBlob{
		Email: req.Email,
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

	credsJSON, _ := json.Marshal([]walib.Credential{*cred})
	user := models.User{
		Email:           blob.Profile.Email,
		FirstName:       blob.Profile.FirstName,
		LastName:        blob.Profile.LastName,
		PreferredName:   blob.Profile.PreferredName,
		Phone:           blob.Profile.Phone,
		WebAuthnID:      blob.Profile.WebAuthnID,
		CredentialsJSON: string(credsJSON),
		CreatedAt:       time.Now().UTC(),
	}
	if err := h.db.CreateUser(r.Context(), user); err != nil {
		log.Printf("ERROR registerComplete CreateUser: %v", err)
		jsonError(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	h.db.DeleteWebAuthnSession(r.Context(), req.SessionID)

	token, err := auth.IssueToken(user.Email)
	if err != nil {
		log.Printf("ERROR registerComplete IssueToken: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]interface{}{"token": token, "user": user})
}

// POST /auth/login/begin
func (h *Handler) loginBegin(w http.ResponseWriter, r *http.Request) {
	var req models.LoginBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		jsonError(w, "email required", http.StatusBadRequest)
		return
	}

	user, err := h.db.GetUser(r.Context(), req.Email)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	creds, err := unmarshalCredentials(user.CredentialsJSON)
	if err != nil || len(creds) == 0 {
		jsonError(w, "no passkey registered", http.StatusBadRequest)
		return
	}

	waUser := &webAuthnUser{
		id:          user.WebAuthnID,
		email:       user.Email,
		displayName: user.FirstName + " " + user.LastName,
		credentials: creds,
	}

	options, sessionData, err := h.wa.BeginLogin(waUser)
	if err != nil {
		log.Printf("ERROR loginBegin: %v", err)
		jsonError(w, "failed to begin login", http.StatusInternalServerError)
		return
	}

	blob := sessionBlob{Email: req.Email, Session: *sessionData}
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

	user, err := h.db.GetUser(r.Context(), blob.Email)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	creds, _ := unmarshalCredentials(user.CredentialsJSON)
	waUser := &webAuthnUser{
		id:          user.WebAuthnID,
		email:       user.Email,
		displayName: user.FirstName + " " + user.LastName,
		credentials: creds,
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		jsonError(w, "invalid credential: "+err.Error(), http.StatusBadRequest)
		return
	}

	updatedCred, err := h.wa.ValidateLogin(waUser, blob.Session, parsed)
	if err != nil {
		jsonError(w, "login failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	newCreds := updateCredential(creds, *updatedCred)
	newCredsJSON, _ := json.Marshal(newCreds)
	h.db.UpdateUserCredentials(r.Context(), user.Email, string(newCredsJSON))
	h.db.DeleteWebAuthnSession(r.Context(), req.SessionID)

	token, err := auth.IssueToken(user.Email)
	if err != nil {
		log.Printf("ERROR loginComplete IssueToken: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]interface{}{"token": token, "user": user})
}
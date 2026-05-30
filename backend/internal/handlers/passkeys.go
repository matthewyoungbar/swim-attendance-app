package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	walib "github.com/go-webauthn/webauthn/webauthn"
)

// GET /passkeys
func (h *Handler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	email := emailFromCtx(r)
	user, err := h.db.GetUser(r.Context(), email)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	passkeys, err := h.db.GetPasskeys(r.Context(), user.WebAuthnID)
	if err != nil {
		log.Printf("ERROR listPasskeys: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	for i := range passkeys {
		var cred walib.Credential
		if err := json.Unmarshal([]byte(passkeys[i].CredentialJSON), &cred); err != nil {
			continue
		}
		passkeys[i].ID = strings.TrimPrefix(passkeys[i].SK, "PASSKEY#")
		transports := make([]string, len(cred.Transport))
		for j, t := range cred.Transport {
			transports[j] = string(t)
		}
		passkeys[i].Transport = transports
	}
	jsonOK(w, passkeys)
}

// POST /passkeys/add/begin
func (h *Handler) addPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	email := emailFromCtx(r)
	user, err := h.db.GetUser(r.Context(), email)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	passkeys, _ := h.db.GetPasskeys(r.Context(), user.WebAuthnID)
	creds := make([]walib.Credential, 0, len(passkeys))
	for _, pk := range passkeys {
		var cred walib.Credential
		if err := json.Unmarshal([]byte(pk.CredentialJSON), &cred); err == nil {
			creds = append(creds, cred)
		}
	}
	waUser := &webAuthnUser{
		id:          user.WebAuthnID,
		email:       user.Email,
		displayName: user.FirstName + " " + user.LastName,
		credentials: creds,
	}
	options, sessionData, err := h.wa.BeginRegistration(waUser,
		walib.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
	)
	if err != nil {
		log.Printf("ERROR addPasskeyBegin: %v", err)
		jsonError(w, "failed to begin", http.StatusInternalServerError)
		return
	}
	blob := sessionBlob{
		Profile: &registrationProfile{
			Email:      user.Email,
			WebAuthnID: user.WebAuthnID,
		},
		Session: *sessionData,
	}
	blobJSON, _ := json.Marshal(blob)
	sessionID := newSessionID()
	if err := h.db.SaveWebAuthnSession(r.Context(), sessionID, blobJSON); err != nil {
		log.Printf("ERROR addPasskeyBegin save session: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"sessionId": sessionID, "options": options})
}

// POST /passkeys/add/complete
func (h *Handler) addPasskeyComplete(w http.ResponseWriter, r *http.Request) {
	email := emailFromCtx(r)
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
	if blob.Profile == nil || blob.Profile.Email != email {
		jsonError(w, "invalid session", http.StatusBadRequest)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		jsonError(w, "invalid credential: "+err.Error(), http.StatusBadRequest)
		return
	}
	waUser := &webAuthnUser{id: blob.Profile.WebAuthnID, email: blob.Profile.Email}
	cred, err := h.wa.CreateCredential(waUser, blob.Session, parsed)
	if err != nil {
		jsonError(w, "failed to add passkey: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.db.SavePasskey(r.Context(), blob.Profile.WebAuthnID, email, *cred); err != nil {
		log.Printf("ERROR addPasskeyComplete SavePasskey: %v", err)
		jsonError(w, "failed to save passkey", http.StatusInternalServerError)
		return
	}
	h.db.DeleteWebAuthnSession(r.Context(), req.SessionID)
	jsonOK(w, map[string]string{"message": "passkey added"})
}

// DELETE /passkeys/{credentialId}
func (h *Handler) deletePasskey(w http.ResponseWriter, r *http.Request, credID string) {
	email := emailFromCtx(r)
	user, err := h.db.GetUser(r.Context(), email)
	if err != nil || user == nil {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}
	passkeys, err := h.db.GetPasskeys(r.Context(), user.WebAuthnID)
	if err != nil {
		log.Printf("ERROR deletePasskey GetPasskeys: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(passkeys) <= 1 {
		jsonError(w, "cannot delete your only passkey", http.StatusBadRequest)
		return
	}
	if err := h.db.DeletePasskey(r.Context(), user.WebAuthnID, credID); err != nil {
		log.Printf("ERROR deletePasskey: %v", err)
		jsonError(w, "failed to delete passkey", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"message": "passkey deleted"})
}
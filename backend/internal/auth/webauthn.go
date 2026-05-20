package auth

import (
	"os"

	"github.com/go-webauthn/webauthn/webauthn"
)

func NewWebAuthn() (*webauthn.WebAuthn, error) {
	rpid := os.Getenv("WEBAUTHN_RPID")
	if rpid == "" {
		rpid = "localhost"
	}
	origin := os.Getenv("WEBAUTHN_ORIGIN")
	if origin == "" {
		origin = "http://localhost:5173"
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "SwimSignup",
		RPID:          rpid,
		RPOrigins:     []string{origin},
	})
}
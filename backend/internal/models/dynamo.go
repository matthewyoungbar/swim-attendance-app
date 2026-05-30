package models

import "time"

const (
	PracticeSK        = "PRACTICE"
	UserSK            = "USER"
	WebAuthnSessionSK = "SESSION"
)

type Practice struct {
	PK          string    `json:"-" dynamodbav:"pk"`
	SK          string    `json:"-" dynamodbav:"sk"`
	ID          string    `json:"id" dynamodbav:"id"`
	Title       string    `json:"title" dynamodbav:"title"`
	Description string    `json:"description" dynamodbav:"description"`
	Location    string    `json:"location" dynamodbav:"location"`
	StartTime   time.Time `json:"startTime" dynamodbav:"startTime"`
	EndTime     time.Time `json:"endTime" dynamodbav:"endTime"`
	Capacity    int       `json:"capacity" dynamodbav:"capacity"`
	SignupCount int       `json:"signupCount" dynamodbav:"signupCount"`
	Theme       string    `json:"theme,omitempty" dynamodbav:"theme,omitempty"`
	CoachID     string    `json:"coachId,omitempty" dynamodbav:"coachId,omitempty"`
	CoachName   string    `json:"coachName,omitempty" dynamodbav:"coachName,omitempty"`
	TTL         int64     `json:"-" dynamodbav:"ttl"`
}

type Signup struct {
	PK           string    `json:"-" dynamodbav:"pk"`
	SK           string    `json:"-" dynamodbav:"sk"`
	PracticeID   string    `json:"practiceId" dynamodbav:"practiceId"`
	SwimmerEmail string    `json:"swimmerEmail" dynamodbav:"swimmerEmail"`
	SwimmerName  string    `json:"swimmerName" dynamodbav:"swimmerName"`
	RegisteredAt time.Time `json:"registeredAt" dynamodbav:"registeredAt"`
	Notes        string    `json:"notes,omitempty" dynamodbav:"notes,omitempty"`
}

type User struct {
	PK            string    `json:"-" dynamodbav:"pk"`
	SK            string    `json:"-" dynamodbav:"sk"`
	Email         string    `json:"email" dynamodbav:"email"`
	FirstName     string    `json:"firstName" dynamodbav:"firstName"`
	LastName      string    `json:"lastName" dynamodbav:"lastName"`
	PreferredName string    `json:"preferredName,omitempty" dynamodbav:"preferredName,omitempty"`
	Phone         string    `json:"phone,omitempty" dynamodbav:"phone,omitempty"`
	IsAdmin       bool      `json:"isAdmin" dynamodbav:"isAdmin,omitempty"`
	IsCoach       bool      `json:"isCoach" dynamodbav:"isCoach,omitempty"`
	IsActive      bool      `json:"isActive" dynamodbav:"isActive"`
	WebAuthnID    []byte    `json:"-" dynamodbav:"webAuthnId"`
	CreatedAt     time.Time `json:"createdAt" dynamodbav:"createdAt"`
}

// Passkey stores a single WebAuthn credential for a user.
// PK = "PASSKEY#" + base64url(webAuthnID), SK = "PASSKEY#" + base64url(credentialID)
type Passkey struct {
	PK             string    `json:"-" dynamodbav:"pk"`
	SK             string    `json:"-" dynamodbav:"sk"`
	UserEmail      string    `json:"-" dynamodbav:"userEmail"`
	CredentialJSON string    `json:"-" dynamodbav:"credentialJSON"`
	CreatedAt      time.Time `json:"createdAt" dynamodbav:"createdAt"`
	// Populated after load from CredentialJSON; not stored.
	ID        string   `json:"id,omitempty" dynamodbav:"-"`
	Transport []string `json:"transport,omitempty" dynamodbav:"-"`
}

type WebAuthnSession struct {
	PK   string `dynamodbav:"pk"`
	SK   string `dynamodbav:"sk"`
	Data string `dynamodbav:"data"`
	TTL  int64  `dynamodbav:"ttl"`
}
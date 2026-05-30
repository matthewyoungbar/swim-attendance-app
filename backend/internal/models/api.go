package models

type SignupRequest struct {
	PracticeID string `json:"practiceId"`
	Notes      string `json:"notes,omitempty"`
}

type RegisterBeginRequest struct {
	Email         string `json:"email"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	PreferredName string `json:"preferredName,omitempty"`
	Phone         string `json:"phone,omitempty"`
}

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type PracticeWithSignups struct {
	Practice
	IsSignedUp bool     `json:"isSignedUp"`
	Signups    []Signup `json:"signups,omitempty"`
}

type UpdateRolesRequest struct {
	IsAdmin  bool `json:"isAdmin"`
	IsCoach  bool `json:"isCoach"`
	IsActive bool `json:"isActive"`
}
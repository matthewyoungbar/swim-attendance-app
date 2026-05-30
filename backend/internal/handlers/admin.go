package handlers

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

type importedUser struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// POST /admin/import-roster
func (h *Handler) importRoster(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		jsonError(w, "failed to parse form", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		jsonError(w, "invalid xlsx file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		jsonError(w, "no sheets found in file", http.StatusBadRequest)
		return
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		jsonError(w, "failed to read sheet", http.StatusBadRequest)
		return
	}

	rosterEmails := make(map[string]bool)
	created := make([]importedUser, 0)
	skipped := 0
	errs := make([]string, 0)

	// Row 0 = title, Row 1 = headers, data starts at Row 2
	for i, row := range rows {
		if i < 2 {
			continue
		}

		email := strings.ToLower(col(row, 11)) // L
		if email == "" || !strings.Contains(email, "@") {
			continue
		}
		rosterEmails[email] = true

		firstName := col(row, 2) // C
		preferred := col(row, 3) // D
		lastName  := col(row, 4) // E
		phone     := col(row, 7) // H (Cell)
		if phone == "" { phone = col(row, 5) } // F (Home)
		if phone == "" { phone = col(row, 6) } // G (Work)

		if firstName == "" || lastName == "" {
			skipped++
			continue
		}
		if strings.EqualFold(preferred, firstName) {
			preferred = ""
		}

		existing, _ := h.db.GetUser(r.Context(), email)
		if existing != nil {
			skipped++
			continue
		}

		waID := make([]byte, 16)
		rand.Read(waID)

		displayName := firstName + " " + lastName
		if preferred != "" {
			displayName = preferred + " " + lastName
		}

		user := models.User{
			Email:         email,
			FirstName:     firstName,
			LastName:      lastName,
			PreferredName: preferred,
			Phone:         phone,
			WebAuthnID:    waID,
			IsActive:      true,
			CreatedAt:     time.Now().UTC(),
		}
		if err := h.db.CreateUser(r.Context(), user); err != nil {
			log.Printf("WARN importRoster %s: %v", email, err)
			errs = append(errs, fmt.Sprintf("%s: %v", email, err))
		} else {
			created = append(created, importedUser{Email: email, Name: displayName})
		}
	}

	// Deactivate active non-admin users not present in the roster.
	deactivated := make([]importedUser, 0)
	allUsers, err := h.db.ListUsers(r.Context())
	if err != nil {
		log.Printf("WARN importRoster ListUsers: %v", err)
	} else {
		for _, u := range allUsers {
			if u.IsAdmin {
				continue
			}
			if rosterEmails[strings.ToLower(u.Email)] {
				continue
			}
			if !u.IsActive {
				continue
			}
			if err := h.db.UpdateUserRoles(r.Context(), u.Email, u.IsAdmin, u.IsCoach, false); err != nil {
				log.Printf("WARN importRoster deactivate %s: %v", u.Email, err)
				errs = append(errs, fmt.Sprintf("deactivate %s: %v", u.Email, err))
			} else {
				name := u.FirstName + " " + u.LastName
				if u.PreferredName != "" {
					name = u.PreferredName + " " + u.LastName
				}
				deactivated = append(deactivated, importedUser{Email: u.Email, Name: name})
			}
		}
	}

	jsonOK(w, map[string]interface{}{
		"created":     created,
		"deactivated": deactivated,
		"skipped":     skipped,
		"errors":      errs,
	})
}

func col(row []string, idx int) string {
	if idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

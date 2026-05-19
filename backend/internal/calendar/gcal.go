package calendar

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/yourorg/swim-signup/internal/models"
)

const defaultCapacity = 20

type Client struct {
	calendarID string
}

// NewClient creates a calendar client for a public Google Calendar.
// Requires only GOOGLE_CALENDAR_ID — no API key or credentials needed.
func NewClient(_ context.Context) (*Client, error) {
	calendarID := os.Getenv("GOOGLE_CALENDAR_ID")
	if calendarID == "" {
		return nil, fmt.Errorf("GOOGLE_CALENDAR_ID env var not set")
	}
	return &Client{calendarID: calendarID}, nil
}

// FetchUpcomingPractices fetches events from the public ICS feed for the next N days.
func (c *Client) FetchUpcomingPractices(ctx context.Context, days int) ([]models.Practice, error) {
	icsURL := "https://calendar.google.com/calendar/ical/" +
		url.PathEscape(c.calendarID) + "/public/basic.ics"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, icsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build ICS request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch ICS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch ICS: HTTP %d", resp.StatusCode)
	}

	all, err := parseICS(resp.Body)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	cutoff := now.Add(time.Duration(days) * 24 * time.Hour)

	var upcoming []models.Practice
	for _, p := range all {
		if !p.StartTime.Before(now) && !p.StartTime.After(cutoff) {
			upcoming = append(upcoming, p)
		}
	}
	return upcoming, nil
}

// ─── ICS parsing ─────────────────────────────────────────────────────────────

type icsProp struct {
	value  string
	params map[string]string
}

func parseICS(r io.Reader) ([]models.Practice, error) {
	scanner := bufio.NewScanner(r)

	// Unfold continuation lines (RFC 5545 §3.1)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(lines) > 0 {
				lines[len(lines)-1] += strings.TrimLeft(line, " \t")
			}
		} else {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read ICS: %w", err)
	}

	var practices []models.Practice
	var inEvent bool
	var props map[string]icsProp

	for _, line := range lines {
		switch line {
		case "BEGIN:VEVENT":
			inEvent = true
			props = make(map[string]icsProp)
		case "END:VEVENT":
			if inEvent {
				if p, err := eventToPractice(props); err == nil {
					practices = append(practices, p)
				}
				inEvent = false
			}
		default:
			if !inEvent {
				continue
			}
			name, prop := parseLine(line)
			if name != "" {
				props[name] = prop
			}
		}
	}

	return practices, nil
}

func parseLine(line string) (string, icsProp) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", icsProp{}
	}
	nameAndParams := line[:idx]
	raw := line[idx+1:]

	// Unescape ICS text values
	value := strings.NewReplacer(
		`\n`, "\n", `\N`, "\n",
		`\,`, ",", `\;`, ";",
		`\\`, `\`,
	).Replace(raw)

	parts := strings.Split(nameAndParams, ";")
	name := strings.ToUpper(parts[0])
	params := make(map[string]string, len(parts)-1)
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			params[strings.ToUpper(k)] = v
		}
	}
	return name, icsProp{value: value, params: params}
}

func eventToPractice(props map[string]icsProp) (models.Practice, error) {
	startProp, ok := props["DTSTART"]
	if !ok {
		return models.Practice{}, fmt.Errorf("missing DTSTART")
	}
	endProp, ok := props["DTEND"]
	if !ok {
		return models.Practice{}, fmt.Errorf("missing DTEND")
	}

	startTime, err := parseICSTime(startProp)
	if err != nil {
		return models.Practice{}, fmt.Errorf("parse DTSTART: %w", err)
	}
	endTime, err := parseICSTime(endProp)
	if err != nil {
		return models.Practice{}, fmt.Errorf("parse DTEND: %w", err)
	}

	description := props["DESCRIPTION"].value
	capacity := defaultCapacity
	if description != "" {
		var cap int
		if _, e := fmt.Sscanf(description, "Capacity: %d", &cap); e == nil && cap > 0 {
			capacity = cap
		}
	}

	return models.Practice{
		ID:          props["UID"].value,
		Title:       props["SUMMARY"].value,
		Description: description,
		Location:    props["LOCATION"].value,
		StartTime:   startTime,
		EndTime:     endTime,
		Capacity:    capacity,
		TTL:         endTime.Add(7 * 24 * time.Hour).Unix(),
	}, nil
}

func parseICSTime(prop icsProp) (time.Time, error) {
	v := prop.value

	// Date-only: 20060102
	if len(v) == 8 {
		return time.Parse("20060102", v)
	}
	// UTC: 20060102T150405Z
	if strings.HasSuffix(v, "Z") {
		return time.Parse("20060102T150405Z", v)
	}
	// Local with TZID: DTSTART;TZID=America/New_York:20060102T150405
	if tzid := prop.params["TZID"]; tzid != "" {
		loc, err := time.LoadLocation(tzid)
		if err != nil {
			// Unknown timezone — parse as UTC rather than fail
			t, err2 := time.Parse("20060102T150405", v)
			if err2 != nil {
				return time.Time{}, fmt.Errorf("unknown timezone %q: %w", tzid, err)
			}
			return t.UTC(), nil
		}
		t, err := time.ParseInLocation("20060102T150405", v, loc)
		if err != nil {
			return time.Time{}, err
		}
		return t.UTC(), nil
	}
	// No timezone info — treat as UTC
	return time.Parse("20060102T150405", v)
}

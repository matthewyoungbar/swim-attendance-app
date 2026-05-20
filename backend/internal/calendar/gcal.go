package calendar

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

const defaultCapacity = 30

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
		return nil, fmt.Errorf("fetch ICS %s: HTTP %d", icsURL, resp.StatusCode)
	}

	all, themes, err := parseICS(resp.Body)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	now := time.Now().UTC()

	var upcoming []models.Practice
	for _, p := range all {
		if !p.StartTime.After(cutoff) && p.EndTime.After(now) {
			if theme, ok := themes[p.StartTime.Format("2006-01-02")]; ok {
				p.Theme = theme
			}
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

func parseICS(r io.Reader) ([]models.Practice, map[string]string, error) {
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
		return nil, nil, fmt.Errorf("read ICS: %w", err)
	}

	var practices []models.Practice
	themes := make(map[string]string) // "2006-01-02" → theme title
	var inEvent bool
	var props map[string]icsProp

	for _, line := range lines {
		switch line {
		case "BEGIN:VEVENT":
			inEvent = true
			props = make(map[string]icsProp)
		case "END:VEVENT":
			if inEvent {
				dtstart := props["DTSTART"]
				if len(dtstart.value) == 8 {
					// All-day event: treat as theme for that date
					if t, err := time.Parse("20060102", dtstart.value); err == nil {
						title := props["SUMMARY"].value
						if rrule, ok := props["RRULE"]; ok {
							expandThemeDates(t, rrule.value, title, themes)
						} else {
							themes[t.Format("2006-01-02")] = title
						}
					}
				} else if p, err := eventToPractice(props); err == nil {
					if rrule, ok := props["RRULE"]; ok {
						practices = append(practices, expandRRULE(p, rrule.value)...)
					} else {
						practices = append(practices, p)
					}
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

	return practices, themes, nil
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

var weekdayAbbrev = [7]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// expandThemeDates populates the themes map for each date a recurring all-day event falls on.
func expandThemeDates(start time.Time, rrule, title string, themes map[string]string) {
	params := map[string]string{}
	for _, part := range strings.Split(rrule, ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			params[k] = v
		}
	}

	freq := params["FREQ"]
	if freq != "DAILY" && freq != "WEEKLY" {
		themes[start.Format("2006-01-02")] = title
		return
	}

	byday := map[string]bool{}
	for _, d := range strings.Split(params["BYDAY"], ",") {
		if d != "" {
			byday[d] = true
		}
	}

	var until time.Time
	if u := params["UNTIL"]; u != "" {
		if len(u) == 8 {
			until, _ = time.Parse("20060102", u)
		} else {
			until, _ = time.Parse("20060102T150405Z", u)
		}
	}
	if until.IsZero() {
		until = time.Now().UTC().Add(90 * 24 * time.Hour)
	}

	count := 0
	if c := params["COUNT"]; c != "" {
		count, _ = strconv.Atoi(c)
	}

	cur := start
	n := 0
	for {
		if count > 0 && n >= count {
			break
		}
		if cur.After(until) {
			break
		}
		themes[cur.Format("2006-01-02")] = title
		n++

		switch freq {
		case "DAILY":
			cur = cur.AddDate(0, 0, 1)
		case "WEEKLY":
			if len(byday) == 0 {
				cur = cur.AddDate(0, 0, 7)
			} else {
				for i := 1; i <= 7; i++ {
					next := cur.AddDate(0, 0, i)
					if byday[weekdayAbbrev[next.Weekday()]] {
						cur = next
						break
					}
				}
			}
		}
	}
}

// expandRRULE generates individual Practice instances from a recurring event.
// Handles FREQ=DAILY and FREQ=WEEKLY (with BYDAY), plus UNTIL and COUNT.
func expandRRULE(base models.Practice, rrule string) []models.Practice {
	params := map[string]string{}
	for _, part := range strings.Split(rrule, ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			params[k] = v
		}
	}

	freq := params["FREQ"]
	if freq != "DAILY" && freq != "WEEKLY" {
		return []models.Practice{base}
	}

	byday := map[string]bool{}
	for _, d := range strings.Split(params["BYDAY"], ",") {
		if d != "" {
			byday[d] = true
		}
	}

	var until time.Time
	if u := params["UNTIL"]; u != "" {
		if len(u) == 8 {
			until, _ = time.Parse("20060102", u)
		} else {
			until, _ = time.Parse("20060102T150405Z", u)
		}
	}
	if until.IsZero() {
		until = time.Now().UTC().Add(90 * 24 * time.Hour)
	}

	count := 0
	if c := params["COUNT"]; c != "" {
		count, _ = strconv.Atoi(c)
	}

	duration := base.EndTime.Sub(base.StartTime)
	var out []models.Practice
	cur := base.StartTime
	n := 0

	for {
		if count > 0 && n >= count {
			break
		}
		if cur.After(until) {
			break
		}

		p := base
		p.StartTime = cur
		p.EndTime = cur.Add(duration)
		p.ID = fmt.Sprintf("%s_%s", base.ID, cur.UTC().Format("20060102T150405"))
		p.TTL = p.EndTime.Add(7 * 24 * time.Hour).Unix()
		out = append(out, p)
		n++

		// Advance to next occurrence
		switch freq {
		case "DAILY":
			cur = cur.AddDate(0, 0, 1)
		case "WEEKLY":
			if len(byday) == 0 {
				cur = cur.AddDate(0, 0, 7)
			} else {
				for i := 1; i <= 7; i++ {
					next := cur.AddDate(0, 0, i)
					if byday[weekdayAbbrev[next.Weekday()]] {
						cur = next
						break
					}
				}
			}
		}
	}

	return out
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

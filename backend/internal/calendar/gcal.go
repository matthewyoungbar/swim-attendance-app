package calendar

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
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

	recurring, individual, themes, err := parseICS(resp.Body)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	now := time.Now().UTC()

	byStart := make(map[time.Time]models.Practice)

	// Recurring expansions first (lower priority).
	for _, p := range recurring {
		if !p.StartTime.After(cutoff) && p.EndTime.After(now) {
			if _, exists := byStart[p.StartTime]; !exists {
				byStart[p.StartTime] = p
			}
		}
	}
	// Individual events override recurring ones for the same time slot.
	for _, p := range individual {
		if !p.StartTime.After(cutoff) && p.EndTime.After(now) {
			byStart[p.StartTime] = p
		}
	}

	upcoming := make([]models.Practice, 0, len(byStart))
	for _, p := range byStart {
		if theme, ok := themes[p.StartTime.Format("2006-01-02")]; ok {
			p.Theme = theme
		}
		upcoming = append(upcoming, p)
	}
	return upcoming, nil
}

// ─── ICS parsing ─────────────────────────────────────────────────────────────

type icsProp struct {
	value  string
	params map[string]string
}

func parseICS(r io.Reader) (recurring []models.Practice, individual []models.Practice, themes map[string]string, err error) {
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
		return nil, nil, nil, fmt.Errorf("read ICS: %w", err)
	}

	type allDayEv struct {
		date  time.Time
		title string
		rrule string
	}

	// Collect all VEVENT prop maps in a first pass so we can build the
	// RECURRENCE-ID exception map before expanding any recurring events.
	var allVEvents []map[string]icsProp
	var allDayEvs []allDayEv
	var inEvent bool
	var props map[string]icsProp

	for _, line := range lines {
		switch line {
		case "BEGIN:VEVENT":
			inEvent = true
			props = make(map[string]icsProp)
		case "END:VEVENT":
			if inEvent {
				allVEvents = append(allVEvents, props)
				dtstart := props["DTSTART"]
				if len(dtstart.value) == 8 {
					if t, parseErr := time.Parse("20060102", dtstart.value); parseErr == nil {
						ev := allDayEv{date: t, title: props["SUMMARY"].value}
						if rrule, ok := props["RRULE"]; ok {
							ev.rrule = rrule.value
						}
						allDayEvs = append(allDayEvs, ev)
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

	// Build RECURRENCE-ID exception map: uid → set of original occurrence times.
	// When expanding a recurring event, skip any occurrence at these times because
	// a specific exception VEVENT overrides it (possibly at a different time).
	exceptions := make(map[string]map[time.Time]bool)
	for _, ev := range allVEvents {
		if rid, ok := ev["RECURRENCE-ID"]; ok {
			uid := ev["UID"].value
			if t, parseErr := parseICSTime(rid); parseErr == nil {
				if exceptions[uid] == nil {
					exceptions[uid] = make(map[time.Time]bool)
				}
				exceptions[uid][t] = true
			}
		}
	}

	// Second pass: process timed VEVENTs into recurring / individual slices.
	var recurringPractices []models.Practice
	var individualPractices []models.Practice
	for _, ev := range allVEvents {
		dtstart := ev["DTSTART"]
		if len(dtstart.value) == 8 {
			continue // all-day events already collected above
		}
		p, parseErr := eventToPractice(ev)
		if parseErr != nil {
			continue
		}
		if rrule, ok := ev["RRULE"]; ok {
			uid := ev["UID"].value
			recurringPractices = append(recurringPractices, expandRRULE(p, rrule.value, exceptions[uid])...)
		} else {
			individualPractices = append(individualPractices, p)
		}
	}

	// Sort recurring events by start date so the most recently started series
	// overwrites older ones deterministically (ICS order varies per request).
	sort.Slice(allDayEvs, func(i, j int) bool {
		return allDayEvs[i].date.Before(allDayEvs[j].date)
	})

	// Recurring events first (general defaults, earliest→latest so newest wins),
	// then non-recurring individual events override specific dates.
	themes = make(map[string]string)
	for _, ev := range allDayEvs {
		if ev.rrule != "" {
			expandThemeDates(ev.date, ev.rrule, ev.title, themes)
		}
	}
	for _, ev := range allDayEvs {
		if ev.rrule == "" {
			themes[ev.date.Format("2006-01-02")] = ev.title
		}
	}

	return recurringPractices, individualPractices, themes, nil
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
// It fast-forwards to today before iterating so past-starting series don't run thousands of steps.
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

	interval := 1
	if iv := params["INTERVAL"]; iv != "" {
		if n, err := strconv.Atoi(iv); err == nil && n > 0 {
			interval = n
		}
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

	today := time.Now().UTC().Truncate(24 * time.Hour)
	cur := fastForwardDate(start, freq, interval, byday, today)
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
			cur = cur.AddDate(0, 0, interval)
		case "WEEKLY":
			if len(byday) == 0 {
				cur = cur.AddDate(0, 0, 7*interval)
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

// fastForwardDate returns the first occurrence of a recurring date sequence on or after horizon.
func fastForwardDate(start time.Time, freq string, interval int, byday map[string]bool, horizon time.Time) time.Time {
	if !start.Before(horizon) {
		return start
	}
	switch freq {
	case "DAILY":
		// Use ceiling division to find the first occurrence on or after horizon,
		// respecting the interval so each series stays in its correct cycle position.
		diff := int(horizon.Sub(start).Hours() / 24)
		n := (diff + interval - 1) / interval
		return start.AddDate(0, 0, n*interval)
	case "WEEKLY":
		valid := byday
		if len(valid) == 0 {
			valid = map[string]bool{weekdayAbbrev[start.Weekday()]: true}
		}
		for i := 0; i <= 7; i++ {
			candidate := horizon.AddDate(0, 0, i)
			if valid[weekdayAbbrev[candidate.Weekday()]] {
				return candidate
			}
		}
	}
	return horizon
}

// expandRRULE generates individual Practice instances from a recurring event.
// Handles FREQ=DAILY and FREQ=WEEKLY (with BYDAY), plus UNTIL and COUNT.
func expandRRULE(base models.Practice, rrule string, exceptions map[time.Time]bool) []models.Practice {
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

		if !exceptions[cur] {
			p := base
			p.StartTime = cur
			p.EndTime = cur.Add(duration)
			p.ID = fmt.Sprintf("%s_%s", base.ID, cur.UTC().Format("20060102T150405"))
			p.TTL = p.EndTime.Add(7 * 24 * time.Hour).Unix()
			out = append(out, p)
		}
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

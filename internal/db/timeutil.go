package db

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CanonicalTimeLayout is the timestamp format td writes to SQLite. It matches
// modernc.org/sqlite's `_time_format=sqlite` writer and its primary read-side
// parser, so values written this way always round-trip. Using a fixed numeric
// offset (`-07:00`) avoids the fragile Go time.Time.String() rendering
// (monotonic-clock suffixes and zone *names* like "PDT") that older/default
// driver configs produced and could not reliably parse back.
const CanonicalTimeLayout = "2006-01-02 15:04:05.999999999-07:00"

// legacyTimeLayouts are the formats accepted when reading a timestamp that may
// have been written by an older td build (or the modernc default writer, which
// used time.Time.String()). Ordered most-canonical first. The monotonic-clock
// suffix (" m=+...") is stripped before these are tried.
var legacyTimeLayouts = []string{
	CanonicalTimeLayout,
	"2006-01-02T15:04:05.999999999-07:00",
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999 -0700 MST", // time.Time.String(), named zone
	"2006-01-02 15:04:05.999999999 -0700",     // offset only (name stripped)
	"2006-01-02 15:04:05.999999999",           // SQLite CURRENT_TIMESTAMP w/ fraction
	"2006-01-02 15:04:05",                     // SQLite CURRENT_TIMESTAMP
	"2006-01-02",
}

// goTimeStringRe matches the tail of a time.Time.String() rendering: a numeric
// offset followed by a zone name (letters, e.g. "PDT") or a repeated numeric
// offset (when the zone has no name, e.g. "-0700 -0700"). Used to detect values
// that were serialized with time.Time.String() so a data-repair pass can target
// only corrupted values and leave canonical / CURRENT_TIMESTAMP values alone.
var goTimeStringRe = regexp.MustCompile(`[+-]\d{4} ([A-Za-z]{2,}|[+-]\d{4})$`)

// LooksLikeGoTimeString reports whether s appears to have been produced by
// time.Time.String() rather than the canonical SQLite layout. Such values carry
// a monotonic-clock suffix (" m=+...") and/or a trailing zone name, neither of
// which the SQLite driver can reliably parse back into a time.Time.
func LooksLikeGoTimeString(s string) bool {
	s = strings.TrimSpace(s)
	if strings.Contains(s, " m=+") || strings.Contains(s, " m=-") {
		return true
	}
	return goTimeStringRe.MatchString(s)
}

// stripMonotonic removes the trailing monotonic-clock reading that
// time.Time.String() appends (e.g. " m=+0.100833084").
func stripMonotonic(s string) string {
	if before, _, found := strings.Cut(s, " m=+"); found {
		return before
	}
	if before, _, found := strings.Cut(s, " m=-"); found {
		return before
	}
	return s
}

// ParseLenientTime parses a timestamp string that may be in the canonical
// SQLite layout or a legacy Go time.Time.String() rendering. It strips the
// monotonic-clock suffix, tolerates a nameless zone rendered as a repeated
// numeric offset, and tries a range of layouts. Returns ok=false if the value
// cannot be parsed by any known layout, so callers can degrade gracefully
// rather than fail hard.
func ParseLenientTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(stripMonotonic(strings.TrimSpace(s)))
	if s == "" {
		return time.Time{}, false
	}

	// A nameless zone renders as "... -0700 -0700"; drop the duplicated
	// trailing offset so the offset-only layout can match.
	if m := goTimeStringRe.FindString(s); m != "" {
		if parts := strings.Fields(m); len(parts) == 2 {
			if _, err := time.Parse("-0700", parts[1]); err == nil {
				s = strings.TrimSuffix(s, " "+parts[1])
			}
		}
	}

	for _, layout := range legacyTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// FormatCanonicalTime renders t in the canonical SQLite layout td writes.
func FormatCanonicalTime(t time.Time) string {
	return t.Format(CanonicalTimeLayout)
}

// lenientTime is a database/sql Scanner for DATETIME columns that tolerates
// values written in a legacy Go time.Time.String() format. The SQLite driver
// hands such values back as raw strings it could not parse; a plain time.Time
// or sql.NullTime destination then fails the whole Scan (and, for a
// multi-column row, the whole query — e.g. a session lookup). lenientTime
// instead records the raw text and leaves Valid=false, so one malformed row
// degrades gracefully instead of bricking the read.
type lenientTime struct {
	Time  time.Time
	Valid bool
	Raw   string
}

// Scan implements sql.Scanner. It never returns an error for any input: an
// unparseable or unexpected value is reported via Valid=false (with the raw
// rendering kept in Raw) so callers decide the fallback (e.g. substitute
// started_at, or the zero time). The whole point is that one malformed cell can
// never fail the surrounding multi-column Scan and brick the query.
func (lt *lenientTime) Scan(src any) error {
	lt.Time, lt.Valid, lt.Raw = time.Time{}, false, ""
	switch v := src.(type) {
	case nil:
		return nil
	case time.Time:
		lt.Time, lt.Valid = v, true
		return nil
	case []byte:
		return lt.scanString(string(v))
	case string:
		return lt.scanString(v)
	case int64:
		// A DATETIME cell holding an integer (e.g. a Unix-epoch bind) is
		// interpreted as seconds since the epoch rather than failing the scan.
		lt.Time, lt.Valid, lt.Raw = time.Unix(v, 0), true, fmt.Sprintf("%d", v)
		return nil
	default:
		// Unknown driver type: degrade gracefully instead of erroring.
		lt.Raw = fmt.Sprintf("%v", src)
		return nil
	}
}

func (lt *lenientTime) scanString(s string) error {
	lt.Raw = s
	if t, ok := ParseLenientTime(s); ok {
		lt.Time, lt.Valid = t, true
	}
	return nil
}

// ensure lenientTime satisfies the Scanner contract at compile time.
var _ interface{ Scan(any) error } = (*lenientTime)(nil)

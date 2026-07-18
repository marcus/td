package db

import (
	"testing"
	"time"
)

func TestParseLenientTime_RealWorldValues(t *testing.T) {
	// Values observed in a corrupted issues.db, written by modernc's default
	// time.Time.String() writer. All must parse back to a valid time.
	cases := []string{
		"2026-07-18 07:29:04.73272 -0700 PDT m=+0.100833084",  // monotonic + named zone
		"2026-07-18 07:37:49.808218 -0700 PDT m=+0.041542501", // monotonic + named zone
		"2025-12-22 20:23:36.006985 -0800 PST",                // named zone, no monotonic
		"2026-01-09 11:28:00.067291 -0700 -0700",              // nameless zone (repeated offset)
		"2026-03-12 20:20:13.380398 -0700 PDT m=+0.096238960",
	}
	for _, c := range cases {
		got, ok := ParseLenientTime(c)
		if !ok {
			t.Errorf("ParseLenientTime(%q) failed to parse", c)
			continue
		}
		if got.IsZero() {
			t.Errorf("ParseLenientTime(%q) returned zero time", c)
		}
	}
}

func TestParseLenientTime_CanonicalAndSQLite(t *testing.T) {
	cases := []string{
		"2026-07-18 07:29:04.73272-07:00", // canonical
		"2026-07-18 14:29:04",             // SQLite CURRENT_TIMESTAMP (UTC)
		"2026-07-18 14:29:04.5",           // CURRENT_TIMESTAMP with fraction
		"2026-07-18",                      // date only
	}
	for _, c := range cases {
		if _, ok := ParseLenientTime(c); !ok {
			t.Errorf("ParseLenientTime(%q) failed to parse", c)
		}
	}
}

func TestParseLenientTime_RoundTrip(t *testing.T) {
	// A time serialized via Go String() must parse back to the same instant.
	orig := time.Date(2026, 3, 12, 20, 20, 13, 380398000, time.FixedZone("PDT", -7*3600))
	s := orig.String()
	got, ok := ParseLenientTime(s)
	if !ok {
		t.Fatalf("failed to parse %q", s)
	}
	if !got.Equal(orig) {
		t.Errorf("round trip mismatch: got %v want %v (from %q)", got, orig, s)
	}
}

func TestParseLenientTime_Unparseable(t *testing.T) {
	for _, c := range []string{"", "   ", "not a time", "garbage 123"} {
		if tm, ok := ParseLenientTime(c); ok {
			t.Errorf("ParseLenientTime(%q) unexpectedly parsed to %v", c, tm)
		}
	}
}

func TestLooksLikeGoTimeString(t *testing.T) {
	goFormat := []string{
		"2026-07-18 07:29:04.73272 -0700 PDT m=+0.100833084",
		"2025-12-22 20:23:36.006985 -0800 PST",
		"2026-01-09 11:28:00.067291 -0700 -0700",
	}
	for _, s := range goFormat {
		if !LooksLikeGoTimeString(s) {
			t.Errorf("LooksLikeGoTimeString(%q) = false, want true", s)
		}
	}

	// Canonical and CURRENT_TIMESTAMP values must NOT be flagged for rewrite.
	notGoFormat := []string{
		"2026-07-18 07:29:04.73272-07:00",
		"2026-07-18 14:29:04",
		"2026-07-18",
		"",
	}
	for _, s := range notGoFormat {
		if LooksLikeGoTimeString(s) {
			t.Errorf("LooksLikeGoTimeString(%q) = true, want false", s)
		}
	}
}

func TestLenientTimeScanner(t *testing.T) {
	var lt lenientTime

	// Parseable string -> Valid, correct time.
	if err := lt.Scan("2026-03-12 20:20:13.380398 -0700 PDT m=+0.09"); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !lt.Valid {
		t.Error("expected Valid=true for parseable string")
	}

	// Unparseable string -> no error, Valid=false, Raw preserved.
	if err := lt.Scan("total garbage"); err != nil {
		t.Fatalf("Scan on garbage returned error (should degrade gracefully): %v", err)
	}
	if lt.Valid {
		t.Error("expected Valid=false for unparseable string")
	}
	if lt.Raw != "total garbage" {
		t.Errorf("Raw = %q, want %q", lt.Raw, "total garbage")
	}

	// time.Time source (driver already parsed it) -> Valid.
	now := time.Now()
	if err := lt.Scan(now); err != nil {
		t.Fatalf("Scan(time.Time) returned error: %v", err)
	}
	if !lt.Valid || !lt.Time.Equal(now) {
		t.Error("expected Valid=true and equal time for time.Time source")
	}

	// nil source -> Valid=false, no error.
	if err := lt.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) returned error: %v", err)
	}
	if lt.Valid {
		t.Error("expected Valid=false for nil source")
	}

	// int64 source -> interpreted as Unix seconds, Valid.
	if err := lt.Scan(int64(1_700_000_000)); err != nil {
		t.Fatalf("Scan(int64) returned error: %v", err)
	}
	if !lt.Valid || !lt.Time.Equal(time.Unix(1_700_000_000, 0)) {
		t.Errorf("expected Unix-seconds interpretation, got valid=%v time=%v", lt.Valid, lt.Time)
	}

	// Unknown driver type -> never errors; Valid=false, Raw captured.
	if err := lt.Scan(3.14); err != nil {
		t.Fatalf("Scan(float64) must not error: %v", err)
	}
	if lt.Valid {
		t.Error("expected Valid=false for unknown source type")
	}
}

func TestQuoteIdent(t *testing.T) {
	cases := map[string]string{
		"sessions":      `"sessions"`,
		"last_activity": `"last_activity"`,
		`we"ird`:        `"we""ird"`,
	}
	for in, want := range cases {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

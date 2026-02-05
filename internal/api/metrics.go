package api

import (
	"sync/atomic"
	"time"
)

// Metrics collects in-memory server metrics using atomic counters.
type Metrics struct {
	startTime    time.Time
	requests     atomic.Int64
	serverErrors atomic.Int64
	clientErrors atomic.Int64
	pushEvents   atomic.Int64
	pullRequests atomic.Int64
}

// MetricsSnapshot is a point-in-time view of server metrics.
type MetricsSnapshot struct {
	UptimeSeconds      float64 `json:"uptime_seconds"`
	Requests           int64   `json:"requests"`
	ServerErrors       int64   `json:"server_errors"`
	ClientErrors       int64   `json:"client_errors"`
	PushEventsAccepted int64   `json:"push_events_accepted"`
	PullRequests       int64   `json:"pull_requests"`
}

// NewMetrics creates a new Metrics instance with the current time as start.
func NewMetrics() *Metrics {
	return &Metrics{startTime: time.Now()}
}

// RecordRequest increments the total request counter.
func (m *Metrics) RecordRequest() {
	m.requests.Add(1)
}

// RecordError increments the server error (5xx) counter.
func (m *Metrics) RecordError() {
	m.serverErrors.Add(1)
}

// RecordClientError increments the client error (4xx) counter.
func (m *Metrics) RecordClientError() {
	m.clientErrors.Add(1)
}

// RecordPushEvents adds n to the accepted push events counter.
func (m *Metrics) RecordPushEvents(n int64) {
	m.pushEvents.Add(n)
}

// RecordPullRequest increments the pull request counter.
func (m *Metrics) RecordPullRequest() {
	m.pullRequests.Add(1)
}

// Snapshot returns a point-in-time copy of the metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		UptimeSeconds:      time.Since(m.startTime).Seconds(),
		Requests:           m.requests.Load(),
		ServerErrors:       m.serverErrors.Load(),
		ClientErrors:       m.clientErrors.Load(),
		PushEventsAccepted: m.pushEvents.Load(),
		PullRequests:       m.pullRequests.Load(),
	}
}

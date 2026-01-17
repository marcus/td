# Memory Leak Analysis Report

**Date**: 2026-01-17
**Analyzer**: Claude Opus 4.5
**Scope**: Full codebase scan for memory leaks and unbounded growth patterns

---

## Executive Summary

This codebase has **no true memory leaks**. The architecture is well-designed for a CLI tool with an optional TUI monitor. Memory usage is predictable and bounded. The main growth vectors (issue count, activity feed) are already capped with reasonable limits.

---

## Analysis Results

### No True Memory Leaks Found

#### 1. Goroutines (SAFE)
- Only one production goroutine: `LogCommandUsageAsync()` at `internal/db/analytics.go:95`
- This is a fire-and-forget pattern with proper timeout (500ms lock acquisition)
- Pattern: `go func() { defer recover(); _ = LogCommandUsage(...) }()`
- Will always complete - cannot block indefinitely due to lock timeout

#### 2. Channels (SAFE)
- No channels used in production code
- All channel usage is confined to test files

#### 3. Timers/Tickers (SAFE)
- No `time.Ticker` or `time.NewTimer` usage
- Only `time.Sleep` for lock backoff with exponential cap (5ms→50ms)
- Bubble Tea's `tea.Tick` handles its own cleanup

#### 4. Context (N/A)
- No `context.Context` usage in the codebase (reasonable for a CLI tool)

#### 5. sync.Pool / sync.Map (N/A)
- Not used

#### 6. HTTP Clients (SAFE)
- `internal/version/version.go:42`: Client has 5s timeout
- `internal/version/version.go:50`: `defer resp.Body.Close()` properly used

#### 7. Database Connections (SAFE)
- Single `*sql.DB` connection pooled by Go's database/sql
- `db.Close()` exposed via `Model.Close()` for embedded usage
- SQLite with WAL mode and 500ms busy timeout

---

## Memory Growth Risks (Not Leaks, But Worth Monitoring)

### 1. Query Evaluator In-Memory Filtering

| Attribute | Value |
|-----------|-------|
| Location | `internal/query/execute.go:12-16` |
| Risk | Default limit of 10,000 issues loaded into memory for filtering |
| Why | TDQ queries with cross-entity conditions require in-memory filtering |
| Scalability | Linear with issue count until cap; acceptable for CLI tool |

**Evidence**:
```go
const (
    DefaultMaxResults = 10000  // Could reduce to 1000 for smaller memory footprint
    MaxDescendantDepth = 100
)
```

**Mitigation**: Already capped; consider lowering for memory-constrained environments.

### 2. Glamour Renderer Recreation

| Attribute | Value |
|-----------|-------|
| Location | `pkg/monitor/modal.go:332` |
| Risk | New renderer created per markdown render call |
| Why | `glamour.NewTermRenderer()` allocates internal styles/parsers |
| Impact | Transient memory spikes during modal rendering |
| Frequency | Only when opening modals or resizing window |

**Current pattern**:
```go
// Creates new renderer each time:
renderer, err := glamour.NewTermRenderer(...)
```

**Potential optimization**:
```go
// Cache renderer by width:
var rendererCache = map[int]*glamour.TermRenderer{}
```

### 3. Activity Feed + Task List Refresh

| Attribute | Value |
|-----------|-------|
| Location | `pkg/monitor/data.go:72-145` |
| Risk | 50 items × 3 types = up to 150 items refreshed every 2 seconds |
| Why | Complete replacement on each tick (no delta updates) |
| Impact | Predictable, bounded memory usage |

**Mitigation**: Already bounded by limit; memory is recycled each refresh.

---

## Profiling Recommendations

### 1. Heap Profile (pprof)

```bash
# Add to main.go for profiling build:
import _ "net/http/pprof"

# Then run:
go tool pprof http://localhost:6060/debug/pprof/heap
```

### 2. Goroutine Profile

```bash
# Check for goroutine accumulation:
go tool pprof http://localhost:6060/debug/pprof/goroutine

# In pprof:
(pprof) top
(pprof) list LogCommandUsageAsync
```

### 3. Memory Comparison Over Time

```bash
# Take snapshots before/after running monitor for extended period:
curl -o heap1.prof http://localhost:6060/debug/pprof/heap
# ... run for 30 minutes ...
curl -o heap2.prof http://localhost:6060/debug/pprof/heap

go tool pprof -base heap1.prof heap2.prof
```

### 4. Runtime Stats (Quick Check)

```go
// Add to any command for quick memory stats:
import "runtime"

var m runtime.MemStats
runtime.ReadMemStats(&m)
fmt.Printf("Alloc = %v MiB", m.Alloc / 1024 / 1024)
fmt.Printf("Sys = %v MiB", m.Sys / 1024 / 1024)
fmt.Printf("NumGoroutine = %d", runtime.NumGoroutine())
```

---

## Summary Table

| Pattern | Status | Location | Notes |
|---------|--------|----------|-------|
| Goroutine leaks | ✅ Safe | `analytics.go:95` | Timeout-bounded, fire-and-forget |
| Channel leaks | ✅ N/A | (tests only) | No production channels |
| Timer/Ticker leaks | ✅ Safe | None | Uses Bubble Tea's managed ticks |
| HTTP body leaks | ✅ Safe | `version.go:50` | Proper `defer resp.Body.Close()` |
| DB connection leaks | ✅ Safe | `db.go:123` | Single pooled connection |
| Unbounded caches | ✅ Safe | None | All caches are disk-based |
| Unbounded slices | ⚠️ Bounded | `execute.go` | Max 10k, refreshed each cycle |
| Context misuse | ✅ N/A | None | No contexts used |

---

## Further Performance Explorations

The following areas warrant additional investigation for comprehensive performance optimization:

### 1. CPU Profiling

**Goal**: Identify hot paths in the TUI rendering loop and database queries.

```bash
# CPU profile during monitor usage:
go test -cpuprofile=cpu.prof -bench=. ./pkg/monitor/...
go tool pprof cpu.prof

# Or runtime profiling:
import "runtime/pprof"
f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()
```

**Key areas to profile**:
- `renderView()` in `pkg/monitor/view.go` (called every frame)
- `FetchData()` in `pkg/monitor/data.go` (called every 2s)
- TDQ query parsing and evaluation

### 2. Database Query Performance

**Goal**: Identify slow queries and optimize indexes.

```bash
# Enable SQLite query timing:
PRAGMA query_only = false;
.timer on
```

**Candidates for investigation**:
- `GetAllDependencies()` - fetches entire dependency graph
- `SearchIssuesRanked()` - full-text search with ranking
- `GetRejectedInProgressIssueIDs()` - joins across tables
- Consider adding `EXPLAIN QUERY PLAN` logging for slow queries

### 3. Allocation Profiling

**Goal**: Reduce GC pressure from allocations in hot paths.

```bash
go test -benchmem -bench=. ./pkg/monitor/...
go tool pprof -alloc_space heap.prof
```

**Potential optimization targets**:
- String concatenation in view rendering (use `strings.Builder`)
- Slice pre-allocation in `FetchData()` (already partially done)
- Reuse of `lipgloss.Style` objects vs recreation

### 4. Execution Tracing

**Goal**: Understand goroutine scheduling and identify blocking.

```bash
go test -trace=trace.out ./...
go tool trace trace.out
```

**Look for**:
- Time spent in GC pauses
- Goroutine blocking on locks
- Scheduler latency

### 5. Benchmark Suite

**Goal**: Establish performance baselines and catch regressions.

**Recommended benchmarks to create**:
```go
// pkg/monitor/benchmark_test.go
func BenchmarkFetchData(b *testing.B)
func BenchmarkRenderView(b *testing.B)
func BenchmarkModalRender(b *testing.B)

// internal/query/benchmark_test.go
func BenchmarkTDQParse(b *testing.B)
func BenchmarkTDQExecute(b *testing.B)
func BenchmarkCrossEntityFilter(b *testing.B)

// internal/db/benchmark_test.go
func BenchmarkListIssues(b *testing.B)
func BenchmarkSearchRanked(b *testing.B)
```

### 6. TUI Rendering Optimization

**Goal**: Reduce frame render time for smoother UI.

**Areas to investigate**:
- Lipgloss style caching (styles are currently recreated)
- Partial re-rendering (only changed panels)
- Virtual scrolling for very long lists
- Debouncing rapid resize events

### 7. SQLite Configuration Tuning

**Goal**: Optimize database performance for the workload.

**Experiments to run**:
```sql
-- Current settings (verify):
PRAGMA journal_mode;      -- Should be WAL
PRAGMA synchronous;       -- Currently NORMAL
PRAGMA cache_size;        -- Default is 2000 pages

-- Potential optimizations:
PRAGMA mmap_size = 268435456;  -- 256MB memory-mapped I/O
PRAGMA cache_size = -64000;    -- 64MB page cache
PRAGMA temp_store = MEMORY;    -- Temp tables in memory
```

### 8. Startup Time Analysis

**Goal**: Reduce time-to-first-render for monitor.

```bash
# Trace startup:
time td monitor --help  # Baseline
hyperfine 'td monitor --help'  # Repeated measurements
```

**Areas to investigate**:
- Database migration check overhead
- Session detection (multiple env var checks, process inspection)
- Keymap registration
- Initial data fetch parallelization

### 9. Memory Pressure Testing

**Goal**: Validate behavior under memory constraints.

```bash
# Run with limited memory:
systemd-run --scope -p MemoryMax=50M td monitor

# Or use cgroups directly:
cgcreate -g memory:tdtest
echo 50M > /sys/fs/cgroup/memory/tdtest/memory.limit_in_bytes
cgexec -g memory:tdtest td monitor
```

### 10. Long-Running Stability Test

**Goal**: Confirm no memory growth over extended periods.

```bash
# Run monitor for 24 hours, sampling memory every minute:
#!/bin/bash
td monitor &
PID=$!
while kill -0 $PID 2>/dev/null; do
    ps -o rss= -p $PID >> memory.log
    sleep 60
done
```

**Expected outcome**: Memory should plateau, not grow linearly.

---

## Conclusion

This codebase demonstrates solid memory management practices. The primary opportunities for performance work lie in CPU optimization (rendering, queries) rather than memory leak remediation. The suggested explorations above provide a roadmap for comprehensive performance characterization.

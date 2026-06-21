package serve

import (
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/workdir"
)

// HandlerConfig carries optional, request-scoped configuration consumed by
// handlers. It exists so that handlers do not depend on the local
// `*serve.Server` (and its `ServeConfig`) and can be invoked by other servers
// — notably td-sync's per-project HTTP routes.
//
// Fields are added as handlers are converted to take a HandlerContext. For the
// read-handler refactor (S1.1) no config is required; later streams (S1.2,
// S1.3) will populate fields such as title length limits.
type HandlerConfig struct {
	// TitleMin and TitleMax bound issue title length when set (>0). Zero
	// means "use default". Populated by S1.2.
	TitleMin int
	TitleMax int

	// NotifyChange, when non-nil, is invoked by write handlers after a
	// successful mutation. The local `td serve` Server uses this to broadcast
	// SSE refresh events and trigger debounced autosync. td-sync leaves this
	// nil — its REST routes handle event-log promotion at the route adapter
	// layer instead.
	NotifyChange func()
}

// HandlerContext is the request-scoped dependency bundle that pure handler
// functions receive in place of a `*Server` receiver. It lets td-sync mount the
// same handlers against per-project DBs without standing up a `*serve.Server`.
//
// `BaseDir` is empty when there is no on-disk td root (e.g. td-sync). Handlers
// that need local-only process state (focus, title-limit lookup) must guard on
// `BaseDir != ""` or fall back to `Config`.
type HandlerContext struct {
	DB         *db.DB
	SessionID  string
	WorktreeID string
	BaseDir    string
	Config     HandlerConfig
}

// handlerContext builds a HandlerContext from the Server's fields. Used by the
// thin method wrappers that delegate to the pure HandleXxx functions.
func (s *Server) handlerContext() HandlerContext {
	return HandlerContext{
		DB:         s.db,
		SessionID:  s.sessionID,
		WorktreeID: s.worktreeID,
		BaseDir:    s.baseDir,
		Config: HandlerConfig{
			// Title limits come from on-disk config; defaults applied in
			// titleLengthLimitsFor when both are zero.
			NotifyChange: s.NotifyChange,
		},
	}
}

func sessionStateScopeFor(ctx HandlerContext) db.SessionStateScope {
	return db.SessionStateScope{
		SessionID:                  ctx.SessionID,
		WorktreeID:                 ctx.WorktreeID,
		ConfigBaseDir:              ctx.BaseDir,
		LegacyGetFocus:             config.GetFocus,
		LegacyGetActiveWorkSession: config.GetActiveWorkSession,
	}
}

func worktreeIDForBaseDir(baseDir string) string {
	wt, err := workdir.WorktreeForPath(baseDir)
	if err != nil {
		return ""
	}
	return wt.WorktreeID
}

// titleLengthLimitsFor resolves the effective min/max title length for a
// handler context. When ctx.Config.TitleMin/TitleMax are zero (e.g. td-sync
// callers that don't carry per-project title rules), it falls back to the
// on-disk config at ctx.BaseDir if available, then to the package defaults.
func titleLengthLimitsFor(ctx HandlerContext) (min, max int) {
	min = ctx.Config.TitleMin
	max = ctx.Config.TitleMax
	if min > 0 && max > 0 {
		return min, max
	}
	if ctx.BaseDir != "" {
		cmin, cmax, _ := config.GetTitleLengthLimits(ctx.BaseDir)
		if min <= 0 {
			min = cmin
		}
		if max <= 0 {
			max = cmax
		}
	}
	if min <= 0 {
		min = config.DefaultTitleMinLength
	}
	if max <= 0 {
		max = config.DefaultTitleMaxLength
	}
	return min, max
}

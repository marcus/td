package serve

import (
	"github.com/marcus/td/internal/db"
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
}

// HandlerContext is the request-scoped dependency bundle that pure handler
// functions receive in place of a `*Server` receiver. It lets td-sync mount the
// same handlers against per-project DBs without standing up a `*serve.Server`.
//
// `BaseDir` is empty when there is no on-disk td root (e.g. td-sync). Handlers
// that touch on-disk config (focus, title-limit lookup) must guard on
// `BaseDir != ""` or fall back to `Config`.
type HandlerContext struct {
	DB        *db.DB
	SessionID string
	BaseDir   string
	Config    HandlerConfig
}

// handlerContext builds a HandlerContext from the Server's fields. Used by the
// thin method wrappers that delegate to the pure HandleXxx functions.
func (s *Server) handlerContext() HandlerContext {
	return HandlerContext{
		DB:        s.db,
		SessionID: s.sessionID,
		BaseDir:   s.baseDir,
		Config: HandlerConfig{
			// Read handlers don't read config today; populated in S1.2.
		},
	}
}

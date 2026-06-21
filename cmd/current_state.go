package cmd

import (
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/session"
)

func currentStateScope(baseDir string, sess *session.Session) db.SessionStateScope {
	return db.SessionStateScope{
		SessionID:                  sess.ID,
		WorktreeID:                 sess.WorktreeID,
		ConfigBaseDir:              baseDir,
		LegacyGetFocus:             config.GetFocus,
		LegacyGetActiveWorkSession: config.GetActiveWorkSession,
	}
}

func getCurrentStateSession(database *db.DB, baseDir string) (*session.Session, db.SessionStateScope, error) {
	sess, err := session.GetOrCreate(database)
	if err != nil {
		return nil, db.SessionStateScope{}, err
	}
	return sess, currentStateScope(baseDir, sess), nil
}

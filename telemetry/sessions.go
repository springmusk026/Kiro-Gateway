package telemetry

import (
	"context"
	"time"
)

// AdminSession is a console login. Only a hash of the session id is stored;
// PasswordCheck binds the session to the admin password it was created with
// (a digest over the raw session id and the password, so it cannot be used to
// recover the password without the id).
type AdminSession struct {
	IDHash        string
	PasswordCheck string
	Remember      bool
	CreatedAt     time.Time
	LastSeen      time.Time
	ExpiresAt     time.Time // absolute expiry
	IP            string
	UserAgent     string
}

// AdminSessionStore persists console sessions so restarts don't sign
// everyone out (database backend only).
type AdminSessionStore interface {
	SaveAdminSession(ctx context.Context, s *AdminSession) error
	// LoadAdminSession returns (nil, nil) when the session does not exist.
	LoadAdminSession(ctx context.Context, idHash string) (*AdminSession, error)
	TouchAdminSession(ctx context.Context, idHash string, lastSeen time.Time) error
	DeleteAdminSession(ctx context.Context, idHash string) error
	DeleteAllAdminSessions(ctx context.Context) error
	PurgeAdminSessions(ctx context.Context, before time.Time) error
}

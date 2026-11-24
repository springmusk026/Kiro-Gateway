package storage

import (
	"context"
	"testing"
	"time"

	"kiro-go/telemetry"
)

func TestSessionStore(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	s, err := NewSessionStore(ctx, db) // the table already exists after Open; creating it again is a no-op
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAllAdminSessions(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DeleteAllAdminSessions(ctx) })
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := &telemetry.AdminSession{IDHash: "h1", PasswordCheck: "pc", Remember: true, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(time.Hour), IP: "10.0.0.1", UserAgent: "ua"}
	if err := s.SaveAdminSession(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadAdminSession(ctx, "h1")
	if err != nil || got == nil || got.PasswordCheck != "pc" || !got.Remember || !got.ExpiresAt.Equal(a.ExpiresAt) {
		t.Fatalf("load: %+v %v", got, err)
	}
	later := now.Add(time.Minute)
	if err := s.TouchAdminSession(ctx, "h1", later); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.LoadAdminSession(ctx, "h1"); !got.LastSeen.Equal(later) {
		t.Fatalf("touch: %v", got.LastSeen)
	}
	expired := &telemetry.AdminSession{IDHash: "h2", PasswordCheck: "pc", CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(-time.Minute)}
	s.SaveAdminSession(ctx, expired)
	if err := s.PurgeAdminSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadAdminSession(ctx, "h2"); got != nil {
		t.Fatal("expired session not purged")
	}
	if err := s.DeleteAdminSession(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadAdminSession(ctx, "h1"); got != nil || err != nil {
		t.Fatalf("delete: %+v %v", got, err)
	}
}

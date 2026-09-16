package cli

import (
	"testing"
	"time"
)

func TestSessionWithoutCookieExpiryDoesNotExpireLocally(t *testing.T) {
	s := &Session{SavedAt: time.Now().Add(-30 * 24 * time.Hour).Format(time.RFC3339)}

	expired, reason := s.Expired()
	if expired {
		t.Fatalf("session cookie without explicit expiry should remain usable, got reason %q", reason)
	}
}

func TestSessionWithExpiredCookieExpires(t *testing.T) {
	s := &Session{ExpiresAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}

	expired, reason := s.Expired()
	if !expired {
		t.Fatal("cookie with an explicit expiry in the past should expire")
	}
	if reason != "cookie_expired" {
		t.Fatalf("unexpected expiry reason: %q", reason)
	}
}

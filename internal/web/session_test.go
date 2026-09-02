package web

import (
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
)

func TestSessionStoreLifecycle(t *testing.T) {
	store := NewSessionStore(time.Hour)

	id, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(id) != 64 { // 32 random bytes, hex-encoded
		t.Fatalf("session id looks wrong: %q (len %d)", id, len(id))
	}

	if got := store.Get(id); got == nil || got.ID != id {
		t.Fatalf("Get after Create returned %+v", got)
	}
	if store.Get("nope") != nil {
		t.Error("Get with unknown id should be nil")
	}
	if store.Get("") != nil {
		t.Error("Get with empty id should be nil")
	}

	ok := store.Update(id, func(s *Session) {
		s.Step = "email"
		s.Profile = config.Profile{FirstName: "Ada"}
		s.ManualSend = true
	})
	if !ok {
		t.Fatal("Update returned false for a live session")
	}
	got := store.Get(id)
	if got.Step != "email" || got.Profile.FirstName != "Ada" || !got.ManualSend {
		t.Fatalf("Update did not persist: %+v", got)
	}
	if store.Update("nope", func(*Session) {}) {
		t.Error("Update on unknown id should return false")
	}

	store.Delete(id)
	if store.Get(id) != nil {
		t.Error("Get after Delete should be nil")
	}
}

func TestSessionExpiry(t *testing.T) {
	store := NewSessionStore(20 * time.Millisecond)

	id, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	if store.Get(id) == nil {
		t.Fatal("session should be live immediately after Create")
	}

	time.Sleep(40 * time.Millisecond)

	if store.Get(id) != nil {
		t.Error("expired session should not be returned by Get")
	}
	if store.Update(id, func(*Session) {}) {
		t.Error("expired session should not be updatable")
	}
}

func TestSessionUpdateExtendsExpiry(t *testing.T) {
	store := NewSessionStore(60 * time.Millisecond)

	id, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}

	// Keep touching it past the original TTL; each Update should push the
	// expiry out, so it stays alive.
	for i := 0; i < 4; i++ {
		time.Sleep(30 * time.Millisecond)
		if !store.Update(id, func(s *Session) { s.Step = "still here" }) {
			t.Fatalf("session expired despite being updated (iteration %d)", i)
		}
	}
}

func TestSessionCleanupRemovesExpired(t *testing.T) {
	store := NewSessionStore(10 * time.Millisecond)

	for i := 0; i < 5; i++ {
		if _, err := store.Create(); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(30 * time.Millisecond)
	store.cleanup()

	store.mu.RLock()
	remaining := len(store.sessions)
	store.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("cleanup left %d expired sessions", remaining)
	}
}

func TestGenerateSessionIDIsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		id, err := generateSessionID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("duplicate session id: %s", id)
		}
		seen[id] = true
	}
}

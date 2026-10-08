//go:build postgres_integration

package fiscal

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TryLock (lot D, phase 3) : tant qu'un détenteur tient le verrou, un second
// passe son tour sans attendre ; une fois relâché, il l'obtient.
func TestTryLock_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const key = "itest:try-lock"

	release, ok, err := TryLock(ctx, db, key)
	if err != nil || !ok {
		t.Fatalf("first holder: ok=%v err=%v", ok, err)
	}
	if _, ok2, err := TryLock(ctx, db, key); err != nil || ok2 {
		t.Fatalf("second holder while held: ok=%v err=%v, want skipped", ok2, err)
	}
	release()
	release2, ok3, err := TryLock(ctx, db, key)
	if err != nil || !ok3 {
		t.Fatalf("after release: ok=%v err=%v", ok3, err)
	}
	release2()
}

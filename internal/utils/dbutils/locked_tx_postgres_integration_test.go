//go:build postgres_integration

package dbutils_test

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/utils/dbutils"
)

// RunInTxLocked sérialise les traitements de même clé, sans gêner les autres clés.
func TestRunInTxLocked_Serializes_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	holding := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- dbutils.RunInTxLocked(ctx, db, "itest-lock:same", func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	secondRan := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- dbutils.RunInTxLocked(ctx, db, "itest-lock:same", func(context.Context) error {
			close(secondRan)
			return nil
		})
	}()

	select {
	case <-secondRan:
		t.Fatal("un traitement de même clé s'est exécuté pendant que le premier tenait le verrou")
	case <-time.After(700 * time.Millisecond):
	}

	// une autre clé n'attend pas
	otherDone := make(chan error, 1)
	go func() {
		otherDone <- dbutils.RunInTxLocked(ctx, db, "itest-lock:other", func(context.Context) error { return nil })
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("clé différente : %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("un traitement de clé différente a été bloqué")
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("premier traitement : %v", err)
	}
	select {
	case <-secondRan:
	case <-time.After(5 * time.Second):
		t.Fatal("le second traitement ne s'est pas exécuté après la libération du verrou")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second traitement : %v", err)
	}
}

// Une erreur annule la transaction et libère le verrou.
func TestRunInTxLocked_ErrorReleases_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	if err := dbutils.RunInTxLocked(ctx, db, "itest-lock:err", func(context.Context) error { return context.Canceled }); err != context.Canceled {
		t.Fatalf("erreur = %v, want %v", err, context.Canceled)
	}
	done := make(chan error, 1)
	go func() {
		done <- dbutils.RunInTxLocked(ctx, db, "itest-lock:err", func(context.Context) error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("le verrou n'a pas été libéré après l'erreur")
	}
}

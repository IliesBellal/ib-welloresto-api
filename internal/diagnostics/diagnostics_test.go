package diagnostics

import (
	"context"
	"database/sql"
	"testing"

	"go.uber.org/zap"
)

// Sans Init (tests, outils en ligne de commande), Record ne doit ni paniquer ni
// bloquer : l'événement reste dans les logs applicatifs.
func TestRecord_WithoutInit(t *testing.T) {
	mu.Lock()
	current = nil
	mu.Unlock()

	Record(context.Background(), Event{Kind: "test_event", SubjectType: "order", SubjectID: "1", Data: map[string]any{"a": 1}})
}

// Base absente : Init ne démarre pas d'écriture, Record reste sans effet et
// sans blocage même au-delà de la taille de la file.
func TestRecord_NilDatabaseNeverBlocks(t *testing.T) {
	Init(nil, nil)
	for i := 0; i < queueSize*3; i++ {
		Record(context.Background(), Event{Kind: "test_event"})
	}
}

// File pleine (écrivain absent) : Record abandonne l'événement sans bloquer.
func TestRecord_FullQueueNeverBlocks(t *testing.T) {
	// Aucun écrivain démarré : la file se remplit, les événements suivants sont
	// abandonnés. Le *sql.DB zéro n'est jamais utilisé.
	r := &recorder{db: &sql.DB{}, log: zap.NewNop(), queue: make(chan row, 1)}
	mu.Lock()
	current = r
	mu.Unlock()
	for i := 0; i < 10; i++ {
		Record(context.Background(), Event{Kind: "test_event"})
	}
}

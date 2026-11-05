package locations

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// testdata/publish_request_from_front.json est le payload que la bibliothèque du
// back-office (lib/floorplan/diff.ts, wello-back-office) produit pour un brouillon
// qui touche toutes les opérations : étage créé / renommé / supprimé, tables et
// obstacles créés sur un étage temporaire, modifiés (dont un déplacement vers un
// étage créé dans le même lot), supprimés, zone créée / modifiée / supprimée.
//
// Le décodage est STRICT (champ inconnu refusé) : un champ mal nommé côté front
// ne serait sinon ignoré en silence par encoding/json, et la modification
// disparaîtrait à l'enregistrement sans aucune erreur.
//
// Pour le régénérer, rejouer le brouillon décrit ci-dessus avec le reducer du
// front et sérialiser `computePublishPayload(base, present, revision)`.
func TestPublishRequestFromFrontendDecodesStrictlyAndValidates(t *testing.T) {
	raw, err := os.ReadFile("testdata/publish_request_from_front.json")
	if err != nil {
		t.Fatal(err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var req PublishPlanRequest
	if err := decoder.Decode(&req); err != nil {
		t.Fatalf("le payload du front n'est pas décodable par PublishPlanRequest : %v", err)
	}

	if err := validatePublishPlan(&req); err != nil {
		t.Fatalf("le payload du front est refusé par la validation : %v", err)
	}

	// Rien n'a été perdu au décodage : chaque famille d'opérations est bien présente.
	counts := map[string][3]int{
		"floors":    {len(req.Floors.Create), len(req.Floors.Update), len(req.Floors.Delete)},
		"tables":    {len(req.Tables.Create), len(req.Tables.Update), len(req.Tables.Delete)},
		"obstacles": {len(req.Obstacles.Create), len(req.Obstacles.Update), len(req.Obstacles.Delete)},
		"areas":     {len(req.Areas.Create), len(req.Areas.Update), len(req.Areas.Delete)},
	}
	want := map[string][3]int{
		"floors":    {1, 1, 1},
		"tables":    {2, 1, 1},
		"obstacles": {3, 2, 1},
		"areas":     {1, 1, 1},
	}
	for kind, expected := range want {
		if counts[kind] != expected {
			t.Errorf("%s (create/update/delete) = %v, want %v", kind, counts[kind], expected)
		}
	}
	if req.BaseRevision != 4 {
		t.Errorf("base_revision = %d, want 4", req.BaseRevision)
	}

	// Les valeurs sensibles survivent au décodage.
	table := req.Tables.Create[0]
	if table.Seats != 4 || table.Shape != "circle" || table.Attributes == nil || !table.Attributes.PMR || !table.Attributes.VIP {
		t.Errorf("table créée mal décodée : %+v", table)
	}
	if update := req.Tables.Update[0]; update.Seats == nil || *update.Seats != 8 || update.FloorID == nil || !isTempID(*update.FloorID) {
		t.Errorf("la table déplacée vers un étage créé dans le lot n'est pas décodée : %+v", update)
	}
	var door *PublishObstacleCreate
	for i := range req.Obstacles.Create {
		if req.Obstacles.Create[i].Type == ObstacleTypeDoor {
			door = &req.Obstacles.Create[i]
		}
	}
	if door == nil || door.Direction == nil || *door.Direction != 90 {
		t.Errorf("la porte créée a perdu son sens d'ouverture : %+v", door)
	}
}

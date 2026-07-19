//go:build postgres_integration

package tags

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestTagsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-tags-m1"
	tagID1 := "itest-tag-1"
	tagID2 := "itest-tag-2"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM tags WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	name1 := "ITest Tag One"
	color1 := "#112233"
	created, err := repo.CreateTag(ctx, merchantID, &CreateTagRequest{ID: &tagID1, Name: name1, Color: &color1})
	if err != nil {
		t.Fatalf("CreateTag failed against postgres: %v", err)
	}
	if created.DisplayOrder != 0 || created.Color != color1 {
		t.Fatalf("unexpected created tag: %+v", created)
	}

	name2 := "ITest Tag Two"
	color2 := "#445566"
	if _, err := repo.CreateTag(ctx, merchantID, &CreateTagRequest{ID: &tagID2, Name: name2, Color: &color2}); err != nil {
		t.Fatalf("CreateTag (2nd) failed against postgres: %v", err)
	}

	// Duplicate tag_id -> primary key violation, translated via dbx.IsDuplicateEntry.
	if _, err := repo.CreateTag(ctx, merchantID, &CreateTagRequest{ID: &tagID1, Name: "dup", Color: &color1}); err != models.ErrInvalidInput {
		t.Fatalf("expected ErrInvalidInput on duplicate tag_id, got %v", err)
	}

	belongs, err := repo.TagBelongsToMerchant(ctx, tagID1, merchantID)
	if err != nil {
		t.Fatalf("TagBelongsToMerchant failed against postgres: %v", err)
	}
	if !belongs {
		t.Fatal("expected tag to belong to merchant")
	}

	list, err := repo.ListTags(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListTags failed against postgres: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tags, got %d: %+v", len(list), list)
	}

	// UpdateTagsDisplayOrder: reorders tag2 before tag1.
	if err := repo.UpdateTagsDisplayOrder(ctx, merchantID, []TagDisplayOrderItem{{ID: tagID2}, {ID: tagID1}}); err != nil {
		t.Fatalf("UpdateTagsDisplayOrder failed against postgres: %v", err)
	}
	list, err = repo.ListTags(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListTags (after reorder) failed: %v", err)
	}
	if list[0].ID != tagID2 || list[1].ID != tagID1 {
		t.Fatalf("unexpected order after UpdateTagsDisplayOrder: %+v", list)
	}

	// UpdateTag: partial update (dynamic SET clause).
	newName := "ITest Tag One Renamed"
	updated, err := repo.UpdateTag(ctx, merchantID, tagID1, &UpdateTagRequest{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateTag failed against postgres: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("expected updated name %q, got %q", newName, updated.Name)
	}

	// UpdateTag with no fields set: returns current tag unchanged.
	same, err := repo.UpdateTag(ctx, merchantID, tagID1, &UpdateTagRequest{})
	if err != nil {
		t.Fatalf("UpdateTag (noop) failed against postgres: %v", err)
	}
	if same.Name != newName {
		t.Fatalf("expected unchanged name %q, got %q", newName, same.Name)
	}

	if err := repo.DeleteTag(ctx, merchantID, tagID2); err != nil {
		t.Fatalf("DeleteTag failed against postgres: %v", err)
	}
	if err := repo.DeleteTag(ctx, merchantID, tagID2); err != models.ErrForbidden {
		t.Fatalf("expected ErrForbidden deleting already-removed tag, got %v", err)
	}
}

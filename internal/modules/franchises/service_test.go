package franchises

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"welloresto-api/internal/models"
)

// fakeStore : franchises, gestionnaires et établissements en mémoire. Les
// transactions ne sont pas simulées (RunInTx exécute directement).
type fakeStore struct {
	franchises map[string]*Franchise
	members    map[string]map[string]string // franchise → user → role
	merchantOf map[string]*string           // merchant → franchise
	visible    map[string]bool
	admins     map[string]map[string]bool // user → merchants administrés
	emails     map[string]string          // email → user
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		franchises: map[string]*Franchise{},
		members:    map[string]map[string]string{},
		merchantOf: map[string]*string{},
		visible:    map[string]bool{},
		admins:     map[string]map[string]bool{},
		emails:     map[string]string{},
	}
}

func (f *fakeStore) addMerchant(id string, admins ...string) {
	f.merchantOf[id] = nil
	for _, u := range admins {
		if f.admins[u] == nil {
			f.admins[u] = map[string]bool{}
		}
		f.admins[u][id] = true
	}
}

func (f *fakeStore) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (f *fakeStore) GetFranchise(_ context.Context, id string) (*Franchise, error) {
	if fr, ok := f.franchises[id]; ok {
		cp := *fr
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeStore) SlugTaken(_ context.Context, slug, exclude string) (bool, error) {
	for id, fr := range f.franchises {
		if fr.Slug == slug && id != exclude {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) InsertFranchise(_ context.Context, fr *Franchise) error {
	cp := *fr
	f.franchises[fr.FranchiseID] = &cp
	return nil
}

func (f *fakeStore) UpdateFranchise(_ context.Context, fr *Franchise) error {
	cp := *fr
	f.franchises[fr.FranchiseID] = &cp
	return nil
}

func (f *fakeStore) SetImageURL(_ context.Context, id, column, url string) error {
	if column == "logo_url" {
		f.franchises[id].LogoURL = &url
	} else {
		f.franchises[id].BannerURL = &url
	}
	return nil
}

func (f *fakeStore) DeleteFranchise(_ context.Context, id string) error {
	for m, fr := range f.merchantOf {
		if fr != nil && *fr == id {
			f.merchantOf[m] = nil
		}
	}
	delete(f.members, id)
	delete(f.franchises, id)
	return nil
}

func (f *fakeStore) ListUserFranchises(_ context.Context, userID string, all bool) ([]FranchiseSummary, error) {
	out := []FranchiseSummary{}
	for id, fr := range f.franchises {
		role := f.members[id][userID]
		if role == "" && !all {
			continue
		}
		if role == "" {
			role = RoleOwner
		}
		out = append(out, FranchiseSummary{FranchiseID: id, Name: fr.Name, Slug: fr.Slug, Role: role})
	}
	return out, nil
}

func (f *fakeStore) GetMemberRole(_ context.Context, id, userID string) (string, error) {
	return f.members[id][userID], nil
}

func (f *fakeStore) ListMembers(_ context.Context, id string) ([]Member, error) {
	out := []Member{}
	for u, role := range f.members[id] {
		out = append(out, Member{UserID: u, Role: role})
	}
	return out, nil
}

func (f *fakeStore) CountOwners(_ context.Context, id string) (int, error) {
	n := 0
	for _, role := range f.members[id] {
		if role == RoleOwner {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) InsertMember(_ context.Context, id, userID, role string) error {
	if f.members[id] == nil {
		f.members[id] = map[string]string{}
	}
	f.members[id][userID] = role
	return nil
}

func (f *fakeStore) UpdateMemberRole(_ context.Context, id, userID, role string) error {
	f.members[id][userID] = role
	return nil
}

func (f *fakeStore) DeleteMember(_ context.Context, id, userID string) error {
	delete(f.members[id], userID)
	return nil
}

func (f *fakeStore) FindUserIDByEmail(_ context.Context, email string) (string, error) {
	return f.emails[email], nil
}

func (f *fakeStore) IsAdminOfAnyMerchant(_ context.Context, userID string) (bool, error) {
	return len(f.admins[userID]) > 0, nil
}

func (f *fakeStore) IsMerchantAdmin(_ context.Context, userID, merchantID string) (bool, error) {
	return f.admins[userID][merchantID], nil
}

func (f *fakeStore) ListAdminMerchants(_ context.Context, userID string) ([]CandidateMerchant, error) {
	out := []CandidateMerchant{}
	for m := range f.admins[userID] {
		out = append(out, CandidateMerchant{MerchantID: m, FranchiseID: f.merchantOf[m]})
	}
	return out, nil
}

func (f *fakeStore) ListFranchiseMerchants(_ context.Context, id string) ([]FranchiseMerchant, error) {
	out := []FranchiseMerchant{}
	for m, fr := range f.merchantOf {
		if fr != nil && *fr == id {
			out = append(out, FranchiseMerchant{MerchantID: m, HubVisible: f.visible[m]})
		}
	}
	return out, nil
}

func (f *fakeStore) MerchantIDsOf(_ context.Context, id string) ([]string, error) {
	out := []string{}
	for m, fr := range f.merchantOf {
		if fr != nil && *fr == id {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeStore) AttachMerchant(_ context.Context, id, merchantID string) (bool, error) {
	cur, ok := f.merchantOf[merchantID]
	if !ok || (cur != nil && *cur != id) {
		return false, nil
	}
	if cur == nil {
		f.visible[merchantID] = true
	}
	fid := id
	f.merchantOf[merchantID] = &fid
	return true, nil
}

func (f *fakeStore) DetachMerchant(_ context.Context, id, merchantID string) error {
	cur := f.merchantOf[merchantID]
	if cur == nil || *cur != id {
		return sql.ErrNoRows
	}
	f.merchantOf[merchantID] = nil
	return nil
}

func (f *fakeStore) SetMerchantHubVisible(_ context.Context, id, merchantID string, visible bool) error {
	cur := f.merchantOf[merchantID]
	if cur == nil || *cur != id {
		return sql.ErrNoRows
	}
	f.visible[merchantID] = visible
	return nil
}

func (f *fakeStore) MerchantFranchiseID(_ context.Context, merchantID string) (*string, bool, error) {
	cur, ok := f.merchantOf[merchantID]
	return cur, ok, nil
}

func (f *fakeStore) FranchiseTimezone(context.Context, string) (string, error) {
	return "Europe/Paris", nil
}

// FranchiseStats : 3 clics et 1 commande de 12 € sur m1, le 2026-06-02.
func (f *fakeStore) FranchiseStats(_ context.Context, _ string, merchantIDs []string, _, _ time.Time, _ string) (*StatsResponse, error) {
	return &StatsResponse{
		Totals:    StatsTotals{HubViews: 10, OrderClicks: 3, HubOrders: 1, HubRevenueTTC: 1200},
		Daily:     []StatsDay{{Date: "2026-06-02", HubViews: 10, OrderClicks: 3, HubOrders: 1}},
		Merchants: []StatsMerchant{{MerchantID: "m1", OrderClicks: 3, HubOrders: 1, HubRevenueTTC: 1200}},
	}, nil
}

type fakeCache struct {
	deleted   []string
	merchants []string
}

func (c *fakeCache) Delete(_ context.Context, key string) bool {
	c.deleted = append(c.deleted, key)
	return true
}

func (c *fakeCache) InvalidateMerchantStatusCache(_ context.Context, merchantID string) {
	c.merchants = append(c.merchants, merchantID)
}

var (
	alice = Actor{UserID: "u-alice"} // admin de m1 et m2
	bob   = Actor{UserID: "u-bob"}   // admin de m3
	carol = Actor{UserID: "u-carol"} // aucun établissement
	staff = Actor{UserID: "u-staff", PlatformStaff: true}
)

func setup(t *testing.T) (*Service, *fakeStore, *fakeCache) {
	t.Helper()
	store := newFakeStore()
	store.addMerchant("m1", alice.UserID)
	store.addMerchant("m2", alice.UserID)
	store.addMerchant("m3", bob.UserID)
	store.emails["bob@example.com"] = bob.UserID
	store.emails["carol@example.com"] = carol.UserID
	cache := &fakeCache{}
	return NewService(store, cache, "https://scannorder.example/"), store, cache
}

func mustCreate(t *testing.T, svc *Service, actor Actor, name string) *DetailResponse {
	t.Helper()
	d, err := svc.Create(context.Background(), actor, CreateRequest{Name: name})
	if err != nil {
		t.Fatalf("Create(%q) error = %v", name, err)
	}
	return d
}

func wantErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestCreate_RequiresAdminOfAnEstablishment(t *testing.T) {
	svc, _, _ := setup(t)
	_, err := svc.Create(context.Background(), carol, CreateRequest{Name: "Burger Co"})
	wantErr(t, err, models.ErrFranchiseCreateForbidden)

	d := mustCreate(t, svc, alice, "Burger Co")
	if d.Role != RoleOwner || d.Franchise.Slug != "burger-co" {
		t.Fatalf("created = role %q slug %q, want owner burger-co", d.Role, d.Franchise.Slug)
	}
	if d.HubURL != "https://scannorder.example/franchise/burger-co" {
		t.Fatalf("HubURL = %q", d.HubURL)
	}
}

func TestCreate_StaffCanCreateWithoutEstablishment(t *testing.T) {
	svc, _, _ := setup(t)
	mustCreate(t, svc, staff, "Support")
}

func TestCreate_DerivedSlugGetsSuffixWhenTaken(t *testing.T) {
	svc, _, _ := setup(t)
	mustCreate(t, svc, alice, "Burger Co")
	d := mustCreate(t, svc, bob, "Burger Co")
	if d.Franchise.Slug != "burger-co-2" {
		t.Fatalf("slug = %q, want burger-co-2", d.Franchise.Slug)
	}
}

func TestCreate_RequestedSlugValidatedAndUnique(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, alice, CreateRequest{Name: "X", Slug: "Not A Slug"})
	wantErr(t, err, models.ErrFranchiseSlugInvalid)
	_, err = svc.Create(ctx, alice, CreateRequest{Name: "X", Slug: "ab"})
	wantErr(t, err, models.ErrFranchiseSlugInvalid)

	mustCreate(t, svc, alice, "Pizza")
	_, err = svc.Create(ctx, bob, CreateRequest{Name: "Autre", Slug: "pizza"})
	wantErr(t, err, models.ErrFranchiseSlugTaken)
}

func TestNonMemberSeesNotFound(t *testing.T) {
	svc, _, _ := setup(t)
	d := mustCreate(t, svc, alice, "Burger Co")
	_, err := svc.Get(context.Background(), bob, d.Franchise.FranchiseID)
	wantErr(t, err, models.ErrFranchiseNotFound)

	if _, err := svc.Get(context.Background(), staff, d.Franchise.FranchiseID); err != nil {
		t.Fatalf("staff Get error = %v", err)
	}
}

func TestAddMerchants_RequiresAdminOfEachEstablishment(t *testing.T) {
	svc, store, cache := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID

	// m3 appartient à Bob : refus, et rien n'est rattaché (tout ou rien).
	_, err := svc.AddMerchants(ctx, alice, id, AddMerchantsRequest{MerchantIDs: []string{"m1", "m3"}})
	wantErr(t, err, models.ErrFranchiseMerchantNotAdmin)
	if store.merchantOf["m1"] != nil {
		t.Fatal("m1 rattaché malgré le refus")
	}

	got, err := svc.AddMerchants(ctx, alice, id, AddMerchantsRequest{MerchantIDs: []string{"m1", "m2", "m1"}})
	if err != nil {
		t.Fatalf("AddMerchants error = %v", err)
	}
	if len(got.Merchants) != 2 || !store.visible["m1"] {
		t.Fatalf("merchants = %+v, want m1+m2 visibles", got.Merchants)
	}
	if len(cache.deleted) == 0 || cache.deleted[len(cache.deleted)-1] != models.ScannorderFranchiseHub+"burger-co" {
		t.Fatalf("hub cache not purged: %v", cache.deleted)
	}
}

func TestAddMerchants_RefusesEstablishmentOfAnotherFranchise(t *testing.T) {
	svc, store, _ := setup(t)
	ctx := context.Background()
	first := mustCreate(t, svc, alice, "Burger Co")
	second := mustCreate(t, svc, alice, "Pizza Co")
	if _, err := svc.AddMerchants(ctx, alice, first.Franchise.FranchiseID, AddMerchantsRequest{MerchantIDs: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.AddMerchants(ctx, alice, second.Franchise.FranchiseID, AddMerchantsRequest{MerchantIDs: []string{"m2", "m1"}})
	wantErr(t, err, models.ErrFranchiseMerchantInOtherFranchise)
	if *store.merchantOf["m1"] != first.Franchise.FranchiseID {
		t.Fatal("m1 a changé de franchise")
	}
}

func TestManagerManagesEstablishmentsButNotMembers(t *testing.T) {
	svc, store, _ := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID
	if _, err := svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "bob@example.com"}); err != nil {
		t.Fatalf("AddMember error = %v", err)
	}
	if store.members[id][bob.UserID] != RoleManager {
		t.Fatal("Bob n'est pas manager")
	}

	// Bob (manager) ajoute son établissement m3, masque m3, mais ne gère pas
	// les gestionnaires et ne supprime pas la franchise.
	if _, err := svc.AddMerchants(ctx, bob, id, AddMerchantsRequest{MerchantIDs: []string{"m3"}}); err != nil {
		t.Fatalf("manager AddMerchants error = %v", err)
	}
	visible := false
	if _, err := svc.UpdateMerchant(ctx, bob, id, "m3", UpdateMerchantRequest{HubVisible: &visible}); err != nil {
		t.Fatalf("manager UpdateMerchant error = %v", err)
	}
	if store.visible["m3"] {
		t.Fatal("m3 encore visible")
	}
	_, err := svc.AddMember(ctx, bob, id, AddMemberRequest{Email: "carol@example.com"})
	wantErr(t, err, models.ErrFranchiseForbidden)
	wantErr(t, svc.Delete(ctx, bob, id), models.ErrFranchiseForbidden)
}

func TestMembers_KeepAtLeastOneOwner(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID

	_, err := svc.UpdateMember(ctx, alice, id, alice.UserID, UpdateMemberRequest{Role: RoleManager})
	wantErr(t, err, models.ErrFranchiseLastOwner)
	_, err = svc.RemoveMember(ctx, alice, id, alice.UserID)
	wantErr(t, err, models.ErrFranchiseLastOwner)

	if _, err := svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "bob@example.com", Role: RoleOwner}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RemoveMember(ctx, alice, id, alice.UserID); err != nil {
		t.Fatalf("second owner present, RemoveMember error = %v", err)
	}
}

func TestMembers_ManagerCanLeave(t *testing.T) {
	svc, store, _ := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID
	if _, err := svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RemoveMember(ctx, bob, id, bob.UserID); err != nil {
		t.Fatalf("manager leaving error = %v", err)
	}
	if store.members[id][bob.UserID] != "" {
		t.Fatal("Bob toujours membre")
	}
}

func TestAddMember_Errors(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID

	_, err := svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "nobody@example.com"})
	wantErr(t, err, models.ErrFranchiseUserNotFound)
	_, err = svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "bob@example.com", Role: "admin"})
	wantErr(t, err, models.ErrFranchiseRoleInvalid)
	if _, err := svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddMember(ctx, alice, id, AddMemberRequest{Email: "bob@example.com"})
	wantErr(t, err, models.ErrFranchiseMemberExists)
}

func TestUpdate_ValidatesAndPurgesOldAndNewSlug(t *testing.T) {
	svc, _, cache := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID

	bad := "red"
	_, err := svc.Update(ctx, alice, id, UpdateRequest{BrandColor: &bad})
	wantErr(t, err, models.ErrFranchiseBrandColorInvalid)

	color, slug, desc := "#ff5500", "burger-company", "  "
	got, err := svc.Update(ctx, alice, id, UpdateRequest{BrandColor: &color, Slug: &slug, Description: &desc})
	if err != nil {
		t.Fatalf("Update error = %v", err)
	}
	if got.Franchise.BrandColor == nil || *got.Franchise.BrandColor != "#FF5500" || got.Franchise.Description != nil {
		t.Fatalf("franchise = %+v", got.Franchise)
	}
	purged := map[string]bool{}
	for _, k := range cache.deleted {
		purged[k] = true
	}
	if !purged[models.ScannorderFranchiseHub+"burger-co"] || !purged[models.ScannorderFranchiseHub+"burger-company"] {
		t.Fatalf("purged = %v, want old and new slug", cache.deleted)
	}
}

func TestDelete_DetachesEstablishments(t *testing.T) {
	svc, store, cache := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID
	if _, err := svc.AddMerchants(ctx, alice, id, AddMerchantsRequest{MerchantIDs: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, alice, id); err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if store.merchantOf["m1"] != nil || store.franchises[id] != nil {
		t.Fatal("franchise or attachment left behind")
	}
	if len(cache.merchants) == 0 {
		t.Fatal("merchant cache not purged")
	}
}

func TestList_CanCreate(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	mustCreate(t, svc, alice, "Burger Co")

	got, err := svc.List(ctx, alice)
	if err != nil || len(got.Franchises) != 1 || !got.CanCreate {
		t.Fatalf("alice List = %+v, %v", got, err)
	}
	got, err = svc.List(ctx, carol)
	if err != nil || len(got.Franchises) != 0 || got.CanCreate {
		t.Fatalf("carol List = %+v, %v", got, err)
	}
}

func TestStats_MembersOnlyAndOneRowPerEstablishment(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	d := mustCreate(t, svc, alice, "Burger Co")
	id := d.Franchise.FranchiseID
	if _, err := svc.AddMerchants(ctx, alice, id, AddMerchantsRequest{MerchantIDs: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Stats(ctx, bob, id, "2026-06-01", "2026-06-03")
	wantErr(t, err, models.ErrFranchiseNotFound)
	_, err = svc.Stats(ctx, alice, id, "2026-06-03", "2026-06-01")
	wantErr(t, err, models.ErrInvalidInput)
	_, err = svc.Stats(ctx, alice, id, "2025-01-01", "2026-06-01")
	wantErr(t, err, models.ErrInvalidInput)

	got, err := svc.Stats(ctx, alice, id, "2026-06-01", "2026-06-03")
	if err != nil {
		t.Fatalf("Stats error = %v", err)
	}
	if len(got.Daily) != 3 || got.Daily[1].OrderClicks != 3 || got.Daily[0].HubViews != 0 {
		t.Fatalf("daily = %+v, want 3 days with activity on the 2nd", got.Daily)
	}
	if len(got.Merchants) != 2 || got.Merchants[0].MerchantID != "m1" || got.Merchants[1].HubOrders != 0 {
		t.Fatalf("merchants = %+v, want m1 (active) then m2 (empty)", got.Merchants)
	}
}

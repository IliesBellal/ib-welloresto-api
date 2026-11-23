package reviewrequests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"
)

type fakeStore struct {
	candidates []candidate
	listFrom   time.Time
	listTo     time.Time
	optedOut   map[string]bool // merchant|hash
	recent     map[string]bool
	rows       map[string]requestRow // par commande
	finished   map[string]string     // commande → statut final
	tokens     map[string]tokenInfo
	review     string
}

func newFakeStore(cands ...candidate) *fakeStore {
	return &fakeStore{
		candidates: cands, optedOut: map[string]bool{}, recent: map[string]bool{},
		rows: map[string]requestRow{}, finished: map[string]string{}, tokens: map[string]tokenInfo{},
	}
}

func (f *fakeStore) ListCandidates(_ context.Context, from, to time.Time, _ int) ([]candidate, error) {
	f.listFrom, f.listTo = from, to
	return f.candidates, nil
}
func (f *fakeStore) IsOptedOut(_ context.Context, m, h string) (bool, error) {
	return f.optedOut[m+"|"+h], nil
}
func (f *fakeStore) RequestedSince(_ context.Context, m, h string, _ time.Time) (bool, error) {
	return f.recent[m+"|"+h], nil
}
func (f *fakeStore) Insert(_ context.Context, row requestRow, _ time.Time) (bool, error) {
	if _, ok := f.rows[row.OrderID]; ok {
		return false, nil
	}
	f.rows[row.OrderID] = row
	f.tokens[row.Token] = tokenInfo{MerchantID: row.MerchantID, MerchantName: "Da Mario", EmailHash: row.EmailHash}
	return true, nil
}
func (f *fakeStore) Finish(_ context.Context, orderID, status, _ string, _ *time.Time) error {
	f.finished[orderID] = status
	return nil
}
func (f *fakeStore) GetByToken(_ context.Context, token string) (*tokenInfo, error) {
	info, ok := f.tokens[token]
	if !ok {
		return nil, models.ErrNotFound
	}
	info.OptedOut = f.optedOut[info.MerchantID+"|"+info.EmailHash]
	return &info, nil
}
func (f *fakeStore) SetOptOut(_ context.Context, m, h string, optedOut bool, _ time.Time) error {
	f.optedOut[m+"|"+h] = optedOut
	return nil
}
func (f *fakeStore) GetReviewSettings(context.Context, string) (string, string, error) {
	return "Da Mario", f.review, nil
}

type sentMail struct {
	to   string
	data mailer.ReviewRequestEmailData
}

type fakeMailer struct {
	sent []sentMail
	err  error
}

func (m *fakeMailer) SendReviewRequest(to string, data mailer.ReviewRequestEmailData) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.sent = append(m.sent, sentMail{to, data})
	return "msg-1", nil
}

// 2026-03-10 14:00 à Paris (UTC+1).
var afternoon = time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC)

func newTestService(store *fakeStore, m *fakeMailer, now time.Time) *Service {
	return &Service{repo: store, mail: m, baseURL: "https://sno.example", log: zap.NewNop(), now: func() time.Time { return now }}
}

func cand(order, email string) candidate {
	return candidate{
		OrderID: order, MerchantID: "42", MerchantName: "Da Mario", Timezone: "Europe/Paris",
		FirstName: "Camille", Email: email, ReviewURL: "https://g.page/r/Cabc/review",
	}
}

func TestRun_SendsAndRecords(t *testing.T) {
	store := newFakeStore(cand("1", "  Camille@Example.FR "))
	m := &fakeMailer{}
	res, err := newTestService(store, m, afternoon).Run(context.Background())
	if err != nil || res.Sent != 1 {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	if !store.listFrom.Equal(afternoon.Add(-ReviewWindow)) || !store.listTo.Equal(afternoon.Add(-ReviewDelay)) {
		t.Fatalf("fenêtre = %v → %v", store.listFrom, store.listTo)
	}
	if len(m.sent) != 1 || m.sent[0].to != "camille@example.fr" {
		t.Fatalf("envoi = %+v", m.sent)
	}
	row := store.rows["1"]
	if row.Status != StatusPending || store.finished["1"] != StatusSent || len(row.EmailHash) != 64 || strings.Contains(row.EmailHash, "@") {
		t.Fatalf("ligne = %+v, statut final %q", row, store.finished["1"])
	}
	d := m.sent[0].data
	if d.CustomerName != "Camille" || d.ReviewURL != "https://g.page/r/Cabc/review" ||
		d.UnsubscribeURL != "https://sno.example/desinscription/avis/"+row.Token || d.TestNotice != "" {
		t.Fatalf("données du mail = %+v", d)
	}
}

func TestRun_Skips(t *testing.T) {
	optedOut := cand("1", "a@example.fr")
	recent := cand("2", "b@example.fr")
	invalid := cand("3", "pas-une-adresse")
	store := newFakeStore(optedOut, recent, invalid)
	store.optedOut["42|"+emailHash("a@example.fr")] = true
	store.recent["42|"+emailHash("b@example.fr")] = true
	m := &fakeMailer{}

	res, err := newTestService(store, m, afternoon).Run(context.Background())
	if err != nil || res.Sent != 0 || res.Skipped != 3 || len(m.sent) != 0 {
		t.Fatalf("Run = %+v, %v, %d envois", res, err, len(m.sent))
	}
	want := map[string]string{"1": StatusSkippedOptOut, "2": StatusSkippedRecent, "3": StatusSkippedInvalid}
	for order, status := range want {
		if store.rows[order].Status != status {
			t.Errorf("commande %s : %q, want %q", order, store.rows[order].Status, status)
		}
	}
}

func TestRun_QuietHoursAndFailures(t *testing.T) {
	// 22 h 30 à Paris : rien n'est envoyé ni enregistré, la commande attend.
	night := time.Date(2026, 3, 10, 21, 30, 0, 0, time.UTC)
	store := newFakeStore(cand("1", "a@example.fr"))
	m := &fakeMailer{}
	if res, _ := newTestService(store, m, night).Run(context.Background()); res != (RunResult{}) || len(store.rows) != 0 {
		t.Fatalf("nuit : %+v, %d lignes", res, len(store.rows))
	}
	// 8 h à Paris : encore trop tôt ; 9 h : envoyé.
	if res, _ := newTestService(store, m, time.Date(2026, 3, 11, 7, 0, 0, 0, time.UTC)).Run(context.Background()); res.Sent != 0 {
		t.Fatal("envoyé avant 9 h")
	}
	if res, _ := newTestService(store, m, time.Date(2026, 3, 11, 8, 0, 0, 0, time.UTC)).Run(context.Background()); res.Sent != 1 {
		t.Fatal("non envoyé à 9 h")
	}

	// Échec d'envoi : ligne failed, jamais retentée.
	store = newFakeStore(cand("1", "a@example.fr"))
	m = &fakeMailer{err: errors.New("brevo 500")}
	res, err := newTestService(store, m, afternoon).Run(context.Background())
	if err != nil || res.Failed != 1 || store.finished["1"] != StatusFailed {
		t.Fatalf("échec : %+v, %v, %q", res, err, store.finished["1"])
	}
	m.err = nil
	if res, _ := newTestService(store, m, afternoon).Run(context.Background()); res.Sent != 0 || len(m.sent) != 0 {
		t.Fatal("commande en échec renvoyée")
	}

	// Sans mailer ni adresse publique : erreur de configuration.
	svc := newTestService(newFakeStore(), nil, afternoon)
	svc.mail = nil
	if _, err := svc.Run(context.Background()); !IsConfigError(err) {
		t.Fatalf("sans mailer : %v", err)
	}
}

func TestOptOutByToken(t *testing.T) {
	store := newFakeStore(cand("1", "a@example.fr"))
	svc := newTestService(store, &fakeMailer{}, afternoon)
	if _, err := svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	token := store.rows["1"].Token

	info, err := svc.Info(context.Background(), token)
	if err != nil || info.MerchantName != "Da Mario" || info.OptedOut || info.Color == "" {
		t.Fatalf("Info = %+v, %v", info, err)
	}
	if info, err := svc.SetOptOut(context.Background(), token, true); err != nil || !info.OptedOut {
		t.Fatalf("désinscription = %+v, %v", info, err)
	}
	if !store.optedOut["42|"+emailHash("a@example.fr")] {
		t.Fatal("désinscription non enregistrée pour cet établissement")
	}
	if info, err := svc.SetOptOut(context.Background(), token, false); err != nil || info.OptedOut {
		t.Fatalf("annulation = %+v, %v", info, err)
	}
	for _, bad := range []string{"", "court", "../../etc/passwd", strings.Repeat("a", 30)} {
		if _, err := svc.Info(context.Background(), bad); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("jeton %q : %v", bad, err)
		}
	}
}

func TestSendTest(t *testing.T) {
	store := newFakeStore()
	m := &fakeMailer{}
	svc := newTestService(store, m, afternoon)
	if err := svc.SendTest(context.Background(), "42", "patron@example.fr"); !errors.Is(err, ErrMissingReviewURL) {
		t.Fatalf("sans lien : %v", err)
	}
	store.review = "https://g.page/r/Cabc/review"
	if err := svc.SendTest(context.Background(), "42", ""); !errors.Is(err, ErrMissingRecipient) {
		t.Fatalf("sans destinataire : %v", err)
	}
	if err := svc.SendTest(context.Background(), "42", "patron@example.fr"); err != nil {
		t.Fatal(err)
	}
	if len(m.sent) != 1 || m.sent[0].data.TestNotice == "" || m.sent[0].data.ReviewURL != store.review || len(store.rows) != 0 {
		t.Fatalf("test : %+v, %d lignes", m.sent, len(store.rows))
	}
}

func TestNormalizeEmail(t *testing.T) {
	ok := map[string]string{" Camille@Example.FR ": "camille@example.fr", "a.b+c@sub.example.com": "a.b+c@sub.example.com"}
	for in, want := range ok {
		if got := normalizeEmail(in); got != want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "x", "a@b", "Camille <a@b.fr>", "a b@c.fr", "a@b.fr\nbcc:x@y.fr"} {
		if got := normalizeEmail(in); got != "" {
			t.Errorf("normalizeEmail(%q) = %q, want vide", in, got)
		}
	}
}

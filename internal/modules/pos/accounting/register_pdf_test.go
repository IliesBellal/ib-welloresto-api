package accounting

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/models"
	cashregisters "welloresto-api/internal/modules/cash_registers"
)

// registerPDFText décompresse les content streams d'un PDF gofpdf et
// concatène leur contenu : de quoi chercher un texte (ASCII) dans le rendu.
func registerPDFText(t *testing.T, pdfBytes []byte) string {
	t.Helper()
	var out bytes.Buffer
	pos := 0
	for {
		si := bytes.Index(pdfBytes[pos:], []byte("stream"))
		if si < 0 {
			break
		}
		si += pos + len("stream")
		for si < len(pdfBytes) && (pdfBytes[si] == '\r' || pdfBytes[si] == '\n') {
			si++
		}
		ei := bytes.Index(pdfBytes[si:], []byte("endstream"))
		if ei < 0 {
			break
		}
		ei += si
		block := pdfBytes[si:ei]
		if r, err := zlib.NewReader(bytes.NewReader(block)); err == nil {
			if decompressed, readErr := io.ReadAll(r); readErr == nil {
				out.Write(decompressed)
			}
		} else {
			out.Write(block)
		}
		pos = ei + len("endstream")
	}
	return out.String()
}

func TestOrderScopePredicate_RegisterID(t *testing.T) {
	sql, args := orderScope{manual: true, registerID: "42"}.predicate("o")
	if !strings.Contains(sql, "o.cash_register_id = ?") || !strings.Contains(sql, "o.brand = 'WELLO_RESTO'") {
		t.Fatalf("predicate = %q, want le périmètre manuel et le filtre registre", sql)
	}
	if len(args) != 1 || args[0] != "42" {
		t.Fatalf("args = %v, want [42]", args)
	}

	// Sans registre, rien n'est ajouté : les exports mensuels sont inchangés.
	sql, args = orderScope{manual: true}.predicate("o")
	if strings.Contains(sql, "cash_register_id") || len(args) != 0 {
		t.Fatalf("predicate sans registre = (%q, %v), want aucun filtre registre", sql, args)
	}

	// L'ordre des arguments suit celui du SQL : sources, types, puis registre.
	sql, args = orderScope{sources: []string{"KIOSK"}, orderTypes: []string{"IN"}, registerID: "7"}.predicate("o")
	if len(args) != 3 || args[2] != "7" || strings.Index(sql, "order_source") > strings.Index(sql, "cash_register_id") {
		t.Fatalf("predicate = (%q, %v), want sources, types puis registre", sql, args)
	}
}

func TestResolveRegisterExport_Validation(t *testing.T) {
	s := &AccountingService{}
	mc := &merchantPDFContext{loc: time.UTC}

	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = strconv.Itoa(i + 1)
		}
		return out
	}

	tests := []struct {
		name    string
		req     RegisterExportRequest
		wantErr error
	}{
		{"rien de demandé", RegisterExportRequest{}, models.ErrRegisterExportEmpty},
		{"identifiants et période", RegisterExportRequest{RegisterIDs: []string{"1"}, DateFrom: "2026-09-01", DateTo: "2026-09-30"}, models.ErrInvalidInput},
		{"identifiant non numérique", RegisterExportRequest{RegisterIDs: []string{"1", "abc"}}, models.ErrInvalidInput},
		{"identifiant vide", RegisterExportRequest{RegisterIDs: []string{" "}}, models.ErrInvalidInput},
		{"plafond dépassé", RegisterExportRequest{RegisterIDs: ids(RegisterExportMax + 1)}, models.ErrRegisterExportTooMany},
		{"date de début seule", RegisterExportRequest{DateFrom: "2026-09-01"}, models.ErrInvalidInput},
		{"date mal formée", RegisterExportRequest{DateFrom: "01/09/2026", DateTo: "2026-09-30"}, models.ErrInvalidInput},
		{"fin avant début", RegisterExportRequest{DateFrom: "2026-09-30", DateTo: "2026-09-01"}, models.ErrInvalidInput},
		{"jour inexistant", RegisterExportRequest{DateFrom: "2026-02-30", DateTo: "2026-03-01"}, models.ErrInvalidInput},
		{"heure mal formée", RegisterExportRequest{DateFrom: "2026-09-01 8h", DateTo: "2026-09-30"}, models.ErrInvalidInput},
		{"même jour, fin avant début", RegisterExportRequest{DateFrom: "2026-09-01 12:00:00", DateTo: "2026-09-01 08:00:00"}, models.ErrInvalidInput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := s.resolveRegisterExport(context.Background(), mc, "1", "u1", tc.req); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	t.Run("plafond atteint accepté", func(t *testing.T) {
		items, name, err := s.resolveRegisterExport(context.Background(), mc, "1", "u1", RegisterExportRequest{RegisterIDs: ids(RegisterExportMax)})
		if err != nil || len(items) != RegisterExportMax {
			t.Fatalf("resolve (%d ids) = (%d items, %v), want %d items", RegisterExportMax, len(items), err, RegisterExportMax)
		}
		if !strings.HasPrefix(name, fmt.Sprintf("WR_registres_%d_", RegisterExportMax)) || !strings.HasSuffix(name, ".zip") {
			t.Fatalf("nom du ZIP = %q", name)
		}
	})

	t.Run("doublons et espaces écartés, ordre conservé", func(t *testing.T) {
		items, _, err := s.resolveRegisterExport(context.Background(), mc, "1", "u1", RegisterExportRequest{RegisterIDs: []string{" 12", "7", "12", "7 "}})
		if err != nil || len(items) != 2 || items[0].id != "12" || items[1].id != "7" {
			t.Fatalf("resolve = (%+v, %v), want [12 7]", items, err)
		}
	})
}

func testRegisterPDFData(mode string) registerPDFData {
	paris, _ := time.LoadLocation("Europe/Paris")
	return registerPDFData{
		Header:          &MerchantHeader{MerchantName: "ITest Resto", SIRET: "12345678900011", Currency: "EUR", Timezone: "Europe/Paris"},
		RegisterNumber:  "4242",
		OpenedBy:        "Alice Martin",
		ClosedBy:        "Bob Durand",
		From:            time.Date(2026, 9, 1, 11, 0, 0, 0, paris),
		To:              time.Date(2026, 9, 1, 15, 0, 0, 0, paris),
		CashFundInitial: 10000,
		CashFundFinal:   12500,
		ClosingMode:     mode,
		TVARows: []TVARow{
			{TVATitle: "TVA 10%", Rate: 10, TTC: 2200, HT: 2000, TVA: 200},
			{TVATitle: "TVA 20%", Rate: 20, TTC: 1200, HT: 1000, TVA: 200},
		},
		Discounts:            DiscountSummary{TTCBeforeDiscounts: 3600, Discounts: 200, TTC: 3400},
		Payments:             []PaymentRow{{Label: "Carte", Amount: 2400}, {Label: "Especes", Amount: 1000}},
		EmptyPaymentsMessage: "Aucun encaissement sur ce registre.",
		TZName:               "Europe/Paris",
	}
}

func TestBuildRegisterPDFReport_Content(t *testing.T) {
	render := func(d registerPDFData) string {
		t.Helper()
		pdf, err := buildRegisterPDFReport(d)
		if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
			t.Fatalf("buildRegisterPDFReport = (%d octets, %v), want un PDF", len(pdf), err)
		}
		return registerPDFText(t, pdf)
	}
	mustContain := func(text, label string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s : %q absent du PDF", label, want)
			}
		}
	}

	// Mêmes sections que le rapport mensuel : TVA, remises, encaissements.
	manual := render(testRegisterPDFData(cashregisters.ClosingModeManual))
	mustContain(manual, "MANUAL", "Registre de caisse", "#4242", "TVA 10%", "20.00 EUR", "Encaissements", "Carte", "24.00 EUR", "Especes", "Manuel", "Remises accord")
	// Plus de tableau théorique / réel / écart.
	for _, gone := range []string{"orique", "cart", "Total"} {
		if strings.Contains(manual, gone) {
			t.Errorf("MANUAL : %q (tableau Théorique/Réel/Écart) ne doit plus apparaître", gone)
		}
	}
	if strings.Contains(manual, "Attention") {
		t.Errorf("MANUAL sain : la mention de dérive ne doit pas apparaître")
	}

	auto := render(testRegisterPDFData(cashregisters.ClosingModeAuto))
	mustContain(auto, "AUTO", "Encaissements", "Carte", "Automatique")
	if strings.Contains(auto, "Manuel") {
		t.Errorf("AUTO : le mode affiché doit être Automatique")
	}

	// Registre en dérive : produit quand même, avec la mention.
	drifted := testRegisterPDFData(cashregisters.ClosingModeManual)
	drifted.Drifted = true
	mustContain(render(drifted), "dérive", "Attention", "modifi", "rapport comptable mensuel")

	// Aucun encaissement : le message du mode remplace le tableau vide.
	empty := testRegisterPDFData(cashregisters.ClosingModeAuto)
	empty.Payments = nil
	mustContain(render(empty), "vide", "Aucun encaissement sur ce registre.")
}

func TestClosingModeLabel(t *testing.T) {
	if got := closingModeLabel(cashregisters.ClosingModeAuto); !strings.HasPrefix(got, "Automatique") {
		t.Errorf("AUTO = %q", got)
	}
	// Tout ce qui n'est pas AUTO se lit comme MANUAL (valeur historique).
	for _, mode := range []string{cashregisters.ClosingModeManual, "", "inconnu"} {
		if got := closingModeLabel(mode); !strings.HasPrefix(got, "Manuel") {
			t.Errorf("%q = %q, want Manuel", mode, got)
		}
	}
}

func TestNormalizeHistoryBound(t *testing.T) {
	for _, tc := range []struct {
		raw, defaultTime, want string
		ok                     bool
	}{
		{"2026-09-01", "00:00:00", "2026-09-01 00:00:00", true},
		{" 2026-09-30 ", "23:59:59", "2026-09-30 23:59:59", true},
		{"2026-09-01 10:30:00", "00:00:00", "2026-09-01 10:30:00", true},
		{"", "00:00:00", "", false},
		{"2026-9-1", "00:00:00", "", false},
		{"2026-09-01T10:30:00Z", "00:00:00", "", false},
		{"2026-13-01", "00:00:00", "", false},
	} {
		got, ok := normalizeHistoryBound(tc.raw, tc.defaultTime)
		if got != tc.want || ok != tc.ok {
			t.Errorf("normalizeHistoryBound(%q, %q) = (%q, %v), want (%q, %v)", tc.raw, tc.defaultTime, got, ok, tc.want, tc.ok)
		}
	}
}

package fiscalarchive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// maxVerifyProblems borne la liste des anomalies rapportées (leur nombre
// total reste compté).
const maxVerifyProblems = 50

// VerifyReport est le résultat de Verify.
type VerifyReport struct {
	Files     int      // fichiers contrôlés par le manifeste
	Tickets   int      // tickets recoupés
	Days      int      // clôtures journalières recoupées avec les tickets
	Months    int      // clôtures mensuelles recoupées avec leurs jours
	Anomalies int      // nombre total d'anomalies
	Problems  []string // les premières anomalies, en clair
}

// OK : aucune anomalie.
func (r *VerifyReport) OK() bool { return r.Anomalies == 0 }

func (r *VerifyReport) add(format string, args ...any) {
	r.Anomalies++
	if len(r.Problems) < maxVerifyProblems {
		r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
	}
}

// Verify relit une archive et la contrôle sans accès à la base (lot D phase 5,
// réutilisé par la vérification d'intégrité du lot E) :
//  1. manifeste : chaque fichier listé est présent, avec son empreinte, sa
//     taille et son nombre de lignes ; aucun fichier hors manifeste ;
//  2. tickets : pour un ticket au format complet, la somme de ses lignes
//     (TTC et HT) égale sa ventilation de TVA ;
//  3. clôtures journalières : ventes, avoirs (TTC et HT), nombre de tickets,
//     premier et dernier numéro recalculés depuis les tickets du jour ;
//  4. clôtures mensuelles dont tous les jours sont dans l'archive : somme de
//     leurs jours.
//
// Une erreur n'est renvoyée que si l'archive est illisible ; les écarts sont
// dans le rapport.
func Verify(zipBytes []byte) (*VerifyReport, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: verify: %w", err)
	}
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	mf, ok := entries["MANIFEST.json"]
	if !ok {
		return nil, fmt.Errorf("fiscalarchive: verify: MANIFEST.json missing")
	}
	raw, err := readEntry(mf)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("fiscalarchive: verify: manifest: %w", err)
	}

	r := &VerifyReport{}
	listed := map[string]bool{"MANIFEST.json": true}
	for _, m := range manifest.Fichiers {
		listed[m.Nom] = true
		f, ok := entries[m.Nom]
		if !ok {
			r.add("%s : listé au manifeste, absent de l'archive", m.Nom)
			continue
		}
		sum, size, lines, err := digestEntry(f)
		if err != nil {
			return nil, err
		}
		r.Files++
		if sum != m.SHA256 || size != m.Octets {
			r.add("%s : empreinte ou taille différente du manifeste (fichier modifié)", m.Nom)
		}
		// Lignes de données (sans l'en-tête) ; les fichiers sans lignes
		// déclarées (notice) ne sont pas comptés.
		if m.Lignes > 0 && lines-1 != m.Lignes {
			r.add("%s : %d lignes, %d au manifeste", m.Nom, lines-1, m.Lignes)
		}
	}
	for name := range entries {
		if !listed[name] {
			r.add("%s : présent dans l'archive, absent du manifeste", name)
		}
	}

	tickets, err := readCSVEntry(entries, "tickets.csv")
	if err != nil {
		return nil, err
	}
	vatRows, err := readCSVEntry(entries, "tickets_tva.csv")
	if err != nil {
		return nil, err
	}
	lineRows, err := readCSVEntry(entries, "tickets_lignes.csv")
	if err != nil {
		return nil, err
	}
	closures, err := readCSVEntry(entries, "clotures.csv")
	if err != nil {
		return nil, err
	}

	type amounts struct{ ttc, ht int64 }
	vat := map[string]*amounts{}
	for _, row := range vatRows {
		a := vat[row.get("numero")]
		if a == nil {
			a = &amounts{}
			vat[row.get("numero")] = a
		}
		a.ttc += row.int("ttc_centimes", r)
		a.ht += row.int("ht_centimes", r)
	}
	lines := map[string]*amounts{}
	for _, row := range lineRows {
		if row.get("total_ttc_centimes") == "" || row.get("total_ht_centimes") == "" {
			continue // ticket historique : pas de totaux par ligne
		}
		a := lines[row.get("numero")]
		if a == nil {
			a = &amounts{}
			lines[row.get("numero")] = a
		}
		a.ttc += row.int("total_ttc_centimes", r)
		a.ht += row.int("total_ht_centimes", r)
	}

	// Totaux par jour, calculés comme la clôture journalière : la
	// ventilation de TVA du ticket quand elle existe, son total sinon.
	type dayTotals struct {
		salesTTC, salesHT, refundsTTC, refundsHT int64
		count                                    int
		first, last                              string
	}
	days := map[string]*dayTotals{}
	for _, t := range tickets {
		number := t.get("numero")
		ttc, ht := t.int("ttc_centimes", r), t.int("ht_centimes", r)
		if v := vat[number]; v != nil {
			ttc, ht = v.ttc, v.ht
		}
		if t.get("format_lignes") == "complet" {
			l, v := lines[number], vat[number]
			if l != nil && v != nil && (l.ttc != v.ttc || l.ht != v.ht) {
				r.add("ticket %s : lignes %d / %d centimes (TTC / HT), TVA ventilée %d / %d", number, l.ttc, l.ht, v.ttc, v.ht)
			}
		}
		r.Tickets++
		date := t.get("date_locale")
		if len(date) < 10 {
			r.add("ticket %s : date locale illisible %q", number, date)
			continue
		}
		d := days[date[:10]]
		if d == nil {
			d = &dayTotals{}
			days[date[:10]] = d
		}
		if ttc >= 0 {
			d.salesTTC += ttc
			d.salesHT += ht
		} else {
			d.refundsTTC += ttc
			d.refundsHT += ht
		}
		d.count++
		if d.first == "" {
			d.first = number
		}
		d.last = number
	}

	type monthSum struct {
		salesTTC, salesHT, refundsTTC, refundsHT, tickets int64
		days                                              int
	}
	months := map[string]*monthSum{}
	closedDays := map[string]bool{}
	for _, c := range closures {
		if c.get("type") != "DAY" {
			continue
		}
		day := c.get("debut")
		closedDays[day] = true
		got := days[day]
		if got == nil {
			got = &dayTotals{}
		}
		want := dayTotals{
			salesTTC: c.int("ventes_ttc_centimes", r), salesHT: c.int("ventes_ht_centimes", r),
			refundsTTC: c.int("avoirs_ttc_centimes", r), refundsHT: c.int("avoirs_ht_centimes", r),
			count: int(c.int("tickets", r)), first: c.get("premier_ticket"), last: c.get("dernier_ticket"),
		}
		if *got != want {
			r.add("clôture du %s : %+v d'après la clôture, %+v d'après les tickets", day, want, *got)
		}
		r.Days++
		if len(day) >= 7 {
			m := months[day[:7]]
			if m == nil {
				m = &monthSum{}
				months[day[:7]] = m
			}
			m.salesTTC += want.salesTTC
			m.salesHT += want.salesHT
			m.refundsTTC += want.refundsTTC
			m.refundsHT += want.refundsHT
			m.tickets += int64(want.count)
			m.days++
		}
	}
	for day := range days {
		if !closedDays[day] {
			r.add("tickets du %s sans clôture journalière dans l'archive", day)
		}
	}
	for _, c := range closures {
		if c.get("type") != "MONTH" {
			continue
		}
		start, err := time.Parse("2006-01-02", c.get("debut"))
		if err != nil {
			r.add("clôture mensuelle : début illisible %q", c.get("debut"))
			continue
		}
		m := months[start.Format("2006-01")]
		if m == nil || m.days != start.AddDate(0, 1, -1).Day() {
			continue // mois partiel dans une archive à la demande
		}
		got := monthSum{salesTTC: c.int("ventes_ttc_centimes", r), salesHT: c.int("ventes_ht_centimes", r),
			refundsTTC: c.int("avoirs_ttc_centimes", r), refundsHT: c.int("avoirs_ht_centimes", r),
			tickets: c.int("tickets", r), days: m.days}
		if got != *m {
			r.add("clôture mensuelle %s : %+v, somme des jours %+v", start.Format("2006-01"), got, *m)
		}
		r.Months++
	}
	return r, nil
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: verify %s: %w", f.Name, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: verify %s: %w", f.Name, err)
	}
	return b, nil
}

// digestEntry calcule en flux l'empreinte, la taille et le nombre de lignes
// physiques d'un fichier (les gros fichiers, comme le journal, ne sont pas
// chargés).
func digestEntry(f *zip.File) (sum string, size, lines int, err error) {
	rc, err := f.Open()
	if err != nil {
		return "", 0, 0, fmt.Errorf("fiscalarchive: verify %s: %w", f.Name, err)
	}
	defer rc.Close()
	h := sha256.New()
	cr := &csvRecordCounter{}
	n, err := io.Copy(io.MultiWriter(h, cr), rc)
	if err != nil {
		return "", 0, 0, fmt.Errorf("fiscalarchive: verify %s: %w", f.Name, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), int(n), cr.records, nil
}

// csvRecordCounter compte les enregistrements CSV (fins de ligne hors
// guillemets : un libellé ou un JSON peut contenir un retour à la ligne).
type csvRecordCounter struct {
	quoted  bool
	records int
}

func (c *csvRecordCounter) Write(p []byte) (int, error) {
	for _, b := range p {
		switch b {
		case '"':
			c.quoted = !c.quoted
		case '\n':
			if !c.quoted {
				c.records++
			}
		}
	}
	return len(p), nil
}

// csvRow donne accès aux colonnes d'une ligne par leur nom d'en-tête.
type csvRow struct {
	header map[string]int
	values []string
}

func (r csvRow) get(col string) string {
	if i, ok := r.header[col]; ok && i < len(r.values) {
		return r.values[i]
	}
	return ""
}

func (r csvRow) int(col string, rep *VerifyReport) int64 {
	v := r.get(col)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		rep.add("valeur illisible %q (colonne %s)", v, col)
	}
	return n
}

func readCSVEntry(entries map[string]*zip.File, name string) ([]csvRow, error) {
	f, ok := entries[name]
	if !ok {
		return nil, nil
	}
	raw, err := readEntry(f)
	if err != nil {
		return nil, err
	}
	cr := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: verify %s: %w", name, err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	header := map[string]int{}
	for i, h := range records[0] {
		header[h] = i
	}
	rows := make([]csvRow, 0, len(records)-1)
	for _, rec := range records[1:] {
		rows = append(rows, csvRow{header: header, values: rec})
	}
	return rows, nil
}

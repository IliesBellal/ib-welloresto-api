package fiscalarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
	"welloresto-api/internal/version"
)

// Built est une archive construite en mémoire, avant stockage.
type Built struct {
	Zip            []byte
	Filename       string
	Timezone       string
	ManifestSHA256 string
	Manifest       Manifest
}

// Manifest est le contenu de MANIFEST.json.
type Manifest struct {
	Logiciel      string         `json:"logiciel"`
	Version       string         `json:"version"`
	Etablissement map[string]any `json:"etablissement"`
	Periode       map[string]any `json:"periode"`
	Type          string         `json:"type"`
	GenereLe      string         `json:"genere_le"`
	Fichiers      []ManifestFile `json:"fichiers"`
}

// ManifestFile décrit un fichier de l'archive.
type ManifestFile struct {
	Nom    string `json:"nom"`
	SHA256 string `json:"sha256"`
	Lignes int    `json:"lignes"`
	Octets int    `json:"octets"`
}

// Build lit les données de la période dans une transaction en lecture seule
// (un seul instantané pour tous les fichiers) et construit le ZIP.
func Build(ctx context.Context, database *sql.DB, merchantID string, start, end time.Time, kind string, generatedAt time.Time) (*Built, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: begin read: %w", err)
	}
	defer tx.Rollback()
	db := dbx.Wrap(tx)

	var name, siret, tz sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT fullname, siret, timezone FROM merchant WHERE id::text = ?`, merchantID).
		Scan(&name, &siret, &tz); err != nil {
		return nil, fmt.Errorf("fiscalarchive: merchant %s: %w", merchantID, err)
	}
	loc, err := time.LoadLocation(tz.String)
	if err != nil || tz.String == "" {
		return nil, fmt.Errorf("fiscalarchive: merchant %s timezone %q: %v", merchantID, tz.String, err)
	}
	b := &builder{ctx: ctx, db: db, loc: loc, merchantID: merchantID,
		from:     time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc),
		to:       time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1),
		startDay: start.Format("2006-01-02"), endDay: end.Format("2006-01-02")}

	var buf bytes.Buffer
	b.zw = zip.NewWriter(&buf)
	b.generatedAt = generatedAt

	steps := []func() error{b.tickets, b.orders, b.orderLines, b.payments, b.journal, b.closures, b.registers}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	if err := b.addFile("NOTICE.txt", []byte(notice()), 0); err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("%s_archive_%s_%s.zip", version.Product, safeFilePart(siret.String), start.Format("2006-01"))
	if kind == KindPeriod {
		filename = fmt.Sprintf("%s_archive_%s_%s_%s_%s.zip", version.Product, safeFilePart(siret.String),
			b.startDay, b.endDay, generatedAt.UTC().Format("20060102T150405Z"))
	}
	manifest := Manifest{
		Logiciel: version.Product,
		Version:  version.Version,
		Etablissement: map[string]any{
			"id": merchantID, "nom": name.String, "siret": siret.String,
		},
		Periode:  map[string]any{"debut": b.startDay, "fin": b.endDay, "fuseau": tz.String},
		Type:     kind,
		GenereLe: fiscal.FormatTime(generatedAt),
		Fichiers: b.manifest,
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	w, err := b.zw.CreateHeader(&zip.FileHeader{Name: "MANIFEST.json", Method: zip.Deflate, Modified: generatedAt})
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(manifestJSON); err != nil {
		return nil, err
	}
	if err := b.zw.Close(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(manifestJSON)
	return &Built{Zip: buf.Bytes(), Filename: filename, Timezone: tz.String, ManifestSHA256: fmt.Sprintf("%x", sum), Manifest: manifest}, nil
}

// --- Fichiers CSV ---
//
// Mémoire : un fichier « en flux » s'écrit directement dans son entrée du ZIP
// (compressée), son empreinte calculée au passage ; seuls les petits fichiers
// produits en même temps que lui (lignes et TVA des tickets) passent par un
// tampon le temps de l'étape. Le journal, de loin le plus gros fichier (état
// complet des commandes avant et après chaque modification), n'est jamais
// tenu en mémoire en clair.

type csvFile struct {
	name string
	w    *csv.Writer
	sum  hash.Hash
	size *countWriter
	rows int
	buf  *bytes.Buffer // nil pour un fichier en flux
}

type countWriter struct{ n int }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}

var bom = []byte{0xEF, 0xBB, 0xBF} // lisible directement dans un tableur

func (b *builder) newCSV(name string, target io.Writer, buf *bytes.Buffer, header []string) *csvFile {
	f := &csvFile{name: name, sum: sha256.New(), size: &countWriter{}, buf: buf}
	out := io.MultiWriter(target, f.sum, f.size)
	_, _ = out.Write(bom)
	f.w = csv.NewWriter(out)
	f.w.Comma = ';'
	f.w.UseCRLF = true
	_ = f.w.Write(header)
	b.open = append(b.open, f)
	return f
}

// stream ouvre un fichier écrit directement dans le ZIP : un seul à la fois,
// avant les éventuels fichiers en tampon de la même étape.
func (b *builder) stream(name string, header ...string) *csvFile {
	entry, err := b.zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: b.generatedAt})
	if err != nil {
		b.err = fmt.Errorf("fiscalarchive: zip %s: %w", name, err)
		entry = io.Discard
	}
	return b.newCSV(name, entry, nil, header)
}

// buffered ouvre un petit fichier tenu en mémoire jusqu'à la fin de l'étape.
func (b *builder) buffered(name string, header ...string) *csvFile {
	buf := &bytes.Buffer{}
	return b.newCSV(name, buf, buf, header)
}

func (f *csvFile) row(values ...string) {
	_ = f.w.Write(values)
	f.rows++
}

// finish termine les fichiers de l'étape, dans leur ordre d'ouverture, et les
// inscrit au manifeste.
func (b *builder) finish(err error) error {
	if err != nil {
		return err
	}
	if b.err != nil {
		return b.err
	}
	for _, f := range b.open {
		f.w.Flush()
		if err := f.w.Error(); err != nil {
			return fmt.Errorf("fiscalarchive: write %s: %w", f.name, err)
		}
		if f.buf != nil {
			entry, err := b.zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: b.generatedAt})
			if err != nil {
				return fmt.Errorf("fiscalarchive: zip %s: %w", f.name, err)
			}
			if _, err := io.Copy(entry, f.buf); err != nil {
				return fmt.Errorf("fiscalarchive: zip %s: %w", f.name, err)
			}
		}
		b.manifest = append(b.manifest, ManifestFile{Nom: f.name, SHA256: fmt.Sprintf("%x", f.sum.Sum(nil)), Lignes: f.rows, Octets: f.size.n})
	}
	b.open = nil
	return nil
}

// addFile ajoute un fichier déjà en mémoire (la notice).
func (b *builder) addFile(name string, content []byte, rows int) error {
	entry, err := b.zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: b.generatedAt})
	if err != nil {
		return err
	}
	if _, err := entry.Write(content); err != nil {
		return err
	}
	sum := sha256.Sum256(content)
	b.manifest = append(b.manifest, ManifestFile{Nom: name, SHA256: fmt.Sprintf("%x", sum), Lignes: rows, Octets: len(content)})
	return nil
}

// text protège un texte libre (libellé, commentaire) contre son
// interprétation comme formule par un tableur : une apostrophe est ajoutée
// devant =, +, -, @, tabulation ou retour chariot initiaux (règle rappelée
// dans la notice).
func text(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

type builder struct {
	ctx              context.Context
	db               *dbx.DB
	loc              *time.Location
	merchantID       string
	from, to         time.Time
	startDay, endDay string
	generatedAt      time.Time
	zw               *zip.Writer
	open             []*csvFile
	manifest         []ManifestFile
	err              error
}

func (b *builder) local(t time.Time) string { return t.In(b.loc).Format("2006-01-02 15:04:05") }
func utc(t time.Time) string                { return t.UTC().Format(time.RFC3339) }
func cents(v int64) string                  { return strconv.FormatInt(v, 10) }

// euros écrit un montant en euros, virgule décimale (tableur français).
func euros(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d,%02d", sign, v/100, v%100)
}

func rate(r float64) string { return strings.Replace(strconv.FormatFloat(r, 'f', -1, 64), ".", ",", 1) }

func nullTime(b *builder, t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return b.local(t.Time)
}

func (b *builder) tickets() error {
	tickets := b.stream("tickets.csv", "numero", "type", "date_locale", "date_utc", "commande", "canal", "registre_commande",
		"ttc_centimes", "ht_centimes", "tva_centimes", "ttc_euros", "ht_euros", "tva_euros", "format_lignes",
		"empreinte_precedente", "empreinte", "signature", "version_empreinte")
	lines := b.buffered("tickets_lignes.csv", "numero", "rang", "nature", "libelle", "quantite", "prix_unitaire_ttc_centimes",
		"taux_tva", "total_ttc_centimes", "total_ht_centimes", "total_tva_centimes", "rang_article")
	vat := b.buffered("tickets_tva.csv", "numero", "taux_tva", "ttc_centimes", "ht_centimes", "tva_centimes", "remise_ticket_centimes")

	rows, err := b.db.QueryContext(b.ctx, `
		SELECT r.receipt_number, r.created_at, r.order_id::text, COALESCE(o.order_source, o.brand, ''),
		       COALESCE(o.cash_register_id, ''), r.total_ttc, r.total_ht, COALESCE(r.items_snapshot::text, '[]'),
		       COALESCE(r.tax_details::text, '{}'), COALESCE(r.prev_hash, ''), COALESCE(r.hash, ''), COALESCE(r.signature, ''),
		       COALESCE(r.hash_version, 1)
		FROM receipts r
		LEFT JOIN orders o ON o.order_id = r.order_id
		WHERE r.merchant_id = ? AND r.created_at >= ? AND r.created_at < ?
		ORDER BY r.created_at, r.receipt_number`, b.merchantID, b.from, b.to)
	if err != nil {
		return fmt.Errorf("fiscalarchive: tickets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var number, order, channel, register, items, tax, prev, hash, sig string
		var at time.Time
		var ttc, ht int64
		var hv int
		if err := rows.Scan(&number, &at, &order, &channel, &register, &ttc, &ht, &items, &tax, &prev, &hash, &sig, &hv); err != nil {
			return fmt.Errorf("fiscalarchive: tickets: %w", err)
		}
		kind := "VENTE"
		if ttc < 0 {
			kind = "AVOIR"
		}
		var snap []models.SnapshotItem
		_ = json.Unmarshal([]byte(items), &snap)
		format := "historique"
		for _, it := range snap {
			if it.Kind != "" {
				format = "complet"
				break
			}
		}
		tickets.row(number, kind, b.local(at), utc(at), order, channel, register, cents(ttc), cents(ht), cents(ttc-ht),
			euros(ttc), euros(ht), euros(ttc-ht), format, prev, hash, sig, strconv.Itoa(hv))

		for i, it := range snap {
			nature, totalTTC, totalHT, totalTVA, parent := it.Kind, cents(it.PriceTTC*int64(it.Quantity)), "", "", ""
			if format == "complet" {
				totalTTC, totalHT, totalTVA = cents(it.TotalTTC), cents(it.TotalHT), cents(it.TotalTVA)
				if it.Parent != nil {
					parent = strconv.Itoa(*it.Parent + 1)
				}
			} else {
				nature = models.SnapshotKindArticle
			}
			lines.row(number, strconv.Itoa(i+1), nature, text(it.Name), strconv.Itoa(it.Quantity), cents(it.PriceTTC),
				rate(float64(it.TaxRate)/100), totalTTC, totalHT, totalTVA, parent)
		}
		if d, ok := fiscal.ParseTaxDetails([]byte(tax)); ok {
			for _, l := range d.Lines {
				vat.row(number, rate(l.Rate), cents(l.TTC), cents(l.HT), cents(l.TVA), cents(d.Discount))
			}
		}
	}
	return b.finish(rows.Err())
}

// closedOrdersSQL : commandes closes dans la période (date de clôture), base
// des fichiers commandes et lignes de commande.
const closedOrdersSQL = `SELECT order_id FROM orders
	WHERE merchant_id = ? AND state = 'CLOSED' AND delivered_on >= ? AND delivered_on < ?`

func (b *builder) orders() error {
	f := b.stream("commandes.csv", "commande", "numero", "type", "canal", "statut", "creation_locale", "cloture_locale",
		"cloture_utc", "ttc_centimes", "ht_centimes", "tva_centimes", "frais_livraison_centimes", "registre")
	rows, err := b.db.QueryContext(b.ctx, `
		SELECT o.order_id::text, COALESCE(o.order_num::text, ''), COALESCE(o.order_type, ''),
		       COALESCE(o.order_source, o.brand, ''), COALESCE(o.brand_status, ''), o.creation_date, o.delivered_on,
		       COALESCE(o.price, 0), COALESCE(o.ht, 0), COALESCE(o.tva, 0), COALESCE(o.delivery_fees, 0),
		       COALESCE(o.cash_register_id, '')
		FROM orders o
		WHERE o.order_id IN (`+closedOrdersSQL+`)
		ORDER BY o.delivered_on, o.order_id`, b.merchantID, b.from, b.to)
	if err != nil {
		return fmt.Errorf("fiscalarchive: orders: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, num, typ, channel, status, register string
		var created, closed sql.NullTime
		var price, ht, tva, fees int64
		if err := rows.Scan(&id, &num, &typ, &channel, &status, &created, &closed, &price, &ht, &tva, &fees, &register); err != nil {
			return fmt.Errorf("fiscalarchive: orders: %w", err)
		}
		closedUTC := ""
		if closed.Valid {
			closedUTC = utc(closed.Time)
		}
		f.row(id, num, typ, channel, status, nullTime(b, created), nullTime(b, closed), closedUTC,
			cents(price), cents(ht), cents(tva), cents(fees), register)
	}
	return b.finish(rows.Err())
}

func (b *builder) orderLines() error {
	f := b.stream("commandes_lignes.csv", "commande", "ligne", "nature", "libelle", "quantite", "prix_unitaire_centimes",
		"prix_catalogue_centimes", "taux_tva_fige", "remise_id")
	rows, err := b.db.QueryContext(b.ctx, `
		SELECT oi.order_id, oi.order_item_id, 1 AS k, 0::bigint AS sub, 'article' AS nature, COALESCE(p.name, ''),
		       oi.quantity::bigint, oi.price::bigint, COALESCE(oi.base_price::text, ''), COALESCE(oi.tva_rate::text, ''),
		       COALESCE(oi.discount_id::text, '')
		FROM orderitems oi LEFT JOIN products p ON p.product_id = oi.product_id
		WHERE oi.order_id IN (`+closedOrdersSQL+`)
		UNION ALL
		SELECT oi.order_id, oi.order_item_id, 2, oic.id::bigint, 'option', COALESCE(cao.title, ''),
		       oic.quantity::bigint, COALESCE(oic.extra_price, -1)::bigint, COALESCE(cao.extra_price::text, ''), '', ''
		FROM order_item_configuration oic
		JOIN orderitems oi ON oi.order_item_id = oic.order_item_id
		LEFT JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
		WHERE oi.order_id IN (`+closedOrdersSQL+`)
		UNION ALL
		SELECT oi.order_id, oi.order_item_id, 3, ex.id::bigint, 'supplement', COALESCE(ce.name, ''),
		       COALESCE(ex.quantity, 1)::bigint, ex.price::bigint, '', '', ''
		FROM extra ex
		JOIN orderitems oi ON oi.order_item_id = ex.order_item_id
		LEFT JOIN components ce ON ce.component_id = ex.component_id
		WHERE oi.order_id IN (`+closedOrdersSQL+`)
		ORDER BY 1, 2, 3, 4`,
		b.merchantID, b.from, b.to, b.merchantID, b.from, b.to, b.merchantID, b.from, b.to)
	if err != nil {
		return fmt.Errorf("fiscalarchive: order lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var order, item, k, sub, qty, price int64
		var nature, label, catalog, tvaRate, discount string
		if err := rows.Scan(&order, &item, &k, &sub, &nature, &label, &qty, &price, &catalog, &tvaRate, &discount); err != nil {
			return fmt.Errorf("fiscalarchive: order lines: %w", err)
		}
		unit := cents(price)
		if nature == "option" && price == -1 {
			unit = "" // surcoût non figé (avant la migration 173)
		}
		f.row(strconv.FormatInt(order, 10), strconv.FormatInt(item, 10), nature, text(label), strconv.FormatInt(qty, 10),
			unit, catalog, strings.Replace(tvaRate, ".", ",", 1), discount)
	}
	return b.finish(rows.Err())
}

func (b *builder) payments() error {
	f := b.stream("paiements.csv", "paiement", "commande", "date_locale", "date_utc", "moyen", "montant_centimes",
		"montant_euros", "type", "utilisateur", "registre", "actif", "empreinte_precedente", "empreinte", "signature",
		"version_empreinte")
	rows, err := b.db.QueryContext(b.ctx, `
		SELECT payment_id::text, COALESCE(order_id::text, ''), payment_date, COALESCE(mop, ''), COALESCE(amount, 0),
		       COALESCE(operation_type, ''), COALESCE(user_id, ''), COALESCE(cash_register_id, ''), COALESCE(enabled, TRUE),
		       COALESCE(previous_hash, ''), COALESCE(hash, ''), COALESCE(signature, ''), COALESCE(hash_version, 1)
		FROM payments
		WHERE merchant_id = ? AND payment_date >= ? AND payment_date < ?
		ORDER BY payment_date, payment_id`, b.merchantID, b.from, b.to)
	if err != nil {
		return fmt.Errorf("fiscalarchive: payments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, order, mop, op, user, register, prev, hash, sig string
		var at sql.NullTime
		var amount int64
		var enabled bool
		var hv int
		if err := rows.Scan(&id, &order, &at, &mop, &amount, &op, &user, &register, &enabled, &prev, &hash, &sig, &hv); err != nil {
			return fmt.Errorf("fiscalarchive: payments: %w", err)
		}
		active := "oui"
		if !enabled {
			active = "non"
		}
		atUTC := ""
		if at.Valid {
			atUTC = utc(at.Time)
		}
		f.row(id, order, nullTime(b, at), atUTC, mop, cents(amount), euros(amount), op, user, register, active, prev, hash, sig, strconv.Itoa(hv))
	}
	return b.finish(rows.Err())
}

func (b *builder) journal() error {
	f := b.stream("journal.csv", "entree", "date_locale", "date_utc", "action", "ressource", "ressource_id", "utilisateur",
		"etat_avant_json", "etat_apres_json", "empreinte_precedente", "empreinte", "signature", "version_empreinte")
	rows, err := b.db.QueryContext(b.ctx, `
		SELECT id, created_at, COALESCE(action, ''), COALESCE(resource_type, ''), COALESCE(resource_id, ''),
		       COALESCE(user_id, ''), COALESCE(old_values::text, ''), COALESCE(new_values::text, ''),
		       COALESCE(previous_hash, ''), COALESCE(hash, ''), COALESCE(signature, ''), COALESCE(hash_version, 1)
		FROM audit_logs
		WHERE merchant_id = ? AND resource_type IN ('`+models.ResourceOrder+`', '`+models.ResourcePayment+`')
		  AND created_at >= ? AND created_at < ?
		ORDER BY created_at, id`, b.merchantID, b.from, b.to)
	if err != nil {
		return fmt.Errorf("fiscalarchive: journal: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, action, resource, resourceID, user, before, after, prev, hash, sig string
		var at time.Time
		var hv int
		if err := rows.Scan(&id, &at, &action, &resource, &resourceID, &user, &before, &after, &prev, &hash, &sig, &hv); err != nil {
			return fmt.Errorf("fiscalarchive: journal: %w", err)
		}
		f.row(id, b.local(at), utc(at), action, resource, resourceID, user, before, after, prev, hash, sig, strconv.Itoa(hv))
	}
	return b.finish(rows.Err())
}

func (b *builder) closures() error {
	f := b.stream("clotures.csv", "type", "debut", "fin", "fuseau", "cloture_utc", "ventes_ttc_centimes", "ventes_ht_centimes",
		"avoirs_ttc_centimes", "avoirs_ht_centimes", "net_ttc_centimes", "net_ht_centimes", "tickets", "premier_ticket",
		"dernier_ticket", "commandes", "tva_par_taux_json", "reglements_json", "canaux_json", "commandes_json",
		"ouverture_json", "grand_total_periode_centimes", "total_perpetuel_centimes", "empreinte_precedente", "empreinte",
		"signature", "version_empreinte")
	rows, err := b.db.QueryContext(b.ctx, `
		SELECT period_type, to_char(period_start, 'YYYY-MM-DD'), to_char(period_end, 'YYYY-MM-DD'), timezone, closed_at,
		       sales_ttc, sales_ht, refunds_ttc, refunds_ht, net_ttc, net_ht, receipts_count,
		       COALESCE(first_receipt_number, ''), COALESCE(last_receipt_number, ''), orders_count,
		       vat_by_rate::text, payments_by_mop::text, by_channel::text, COALESCE(orders::text, ''), COALESCE(opening::text, ''),
		       grand_total_period, perpetual_total, previous_hash, hash, signature, hash_version
		FROM fiscal_closures
		WHERE merchant_id = ?
		  AND ((period_type = 'DAY' AND period_start BETWEEN ?::date AND ?::date)
		    OR (period_type IN ('MONTH', 'YEAR') AND period_end BETWEEN ?::date AND ?::date))
		ORDER BY CASE period_type WHEN 'DAY' THEN 1 WHEN 'MONTH' THEN 2 ELSE 3 END, period_start`,
		b.merchantID, b.startDay, b.endDay, b.startDay, b.endDay)
	if err != nil {
		return fmt.Errorf("fiscalarchive: closures: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ, start, end, tz, first, last, vat, pays, channels, orders, opening, prev, hash, sig string
		var closedAt time.Time
		var salesTTC, salesHT, refundsTTC, refundsHT, netTTC, netHT, grand, perpetual int64
		var receipts, ordersCount, hv int
		if err := rows.Scan(&typ, &start, &end, &tz, &closedAt, &salesTTC, &salesHT, &refundsTTC, &refundsHT, &netTTC, &netHT,
			&receipts, &first, &last, &ordersCount, &vat, &pays, &channels, &orders, &opening, &grand, &perpetual,
			&prev, &hash, &sig, &hv); err != nil {
			return fmt.Errorf("fiscalarchive: closures: %w", err)
		}
		f.row(typ, start, end, tz, utc(closedAt), cents(salesTTC), cents(salesHT), cents(refundsTTC), cents(refundsHT),
			cents(netTTC), cents(netHT), strconv.Itoa(receipts), first, last, strconv.Itoa(ordersCount), vat, pays, channels,
			orders, opening, cents(grand), cents(perpetual), prev, hash, sig, strconv.Itoa(hv))
	}
	return b.finish(rows.Err())
}

func (b *builder) registers() error {
	f := b.stream("registres.csv", "registre", "poste", "appareil", "utilisateur", "ouverture_locale", "fermeture_locale",
		"fermeture_utc", "fond_initial_centimes", "fond_final_centimes", "ferme_par", "commentaire", "lignes_z_json",
		"empreinte_precedente", "empreinte", "signature", "version_empreinte")
	rows, err := b.db.QueryContext(b.ctx, `
		SELECT cr.cash_register_id::text, COALESCE(cr.cash_desk_id::text, ''), COALESCE(cr.device_id, ''), COALESCE(cr.user_id, ''),
		       cr.start_date, cr.end_date, COALESCE(cr.cash_fund, 0), COALESCE(cr.final_cash_fund, 0),
		       COALESCE(cr.closed_by, ''), COALESCE(cr.closure_comment, ''),
		       COALESCE((SELECT json_agg(json_build_object('mop', ci.mop, 'amount', ci.amount) ORDER BY ci.id)::text
		                 FROM cash_registers_items ci WHERE ci.cash_register_id = cr.cash_register_id), '[]'),
		       COALESCE(cr.previous_hash, ''), COALESCE(cr.hash, ''), COALESCE(cr.signature, ''), COALESCE(cr.hash_version, 1)
		FROM cash_registers cr
		WHERE cr.merchant_id = ? AND cr.closed AND cr.end_date >= ? AND cr.end_date < ?
		ORDER BY cr.end_date, cr.cash_register_id`, b.merchantID, b.from, b.to)
	if err != nil {
		return fmt.Errorf("fiscalarchive: registers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, desk, device, user, closedBy, comment, z, prev, hash, sig string
		var opened, closed sql.NullTime
		var fund, final int64
		var hv int
		if err := rows.Scan(&id, &desk, &device, &user, &opened, &closed, &fund, &final, &closedBy, &comment, &z, &prev, &hash, &sig, &hv); err != nil {
			return fmt.Errorf("fiscalarchive: registers: %w", err)
		}
		closedUTC := ""
		if closed.Valid {
			closedUTC = utc(closed.Time)
		}
		f.row(id, desk, device, user, nullTime(b, opened), nullTime(b, closed), closedUTC, cents(fund), cents(final),
			closedBy, text(comment), z, prev, hash, sig, strconv.Itoa(hv))
	}
	return b.finish(rows.Err())
}

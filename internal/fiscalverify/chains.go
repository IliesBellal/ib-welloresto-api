package fiscalverify

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/fiscalarchive"
	"welloresto-api/internal/utils/security"
)

// link est un maillon de chaîne relu en base.
type link struct {
	id      string
	at      time.Time
	prev    string
	hash    string
	sig     string
	version int
}

// chainSpec décrit une chaîne : sa table, sa colonne de date (ordre des
// maillons) et sa gravité. legacy : chaîne retirée (commandes, lot B),
// contrôlée pour son seul chaînage, en avertissements.
type chainSpec struct {
	check, label string
	table        string
	dateExpr     string
	legacy       bool
}

func isGenesis(prev string) bool { return prev == "" || prev == fiscal.GenesisHash }

// sev : gravité d'une anomalie sur un maillon.
func (s chainSpec) sev(version int) Severity {
	return severity(!s.legacy && version >= fiscal.HashVersion)
}

// linkage contrôle le chaînage des maillons d'une période :
//   - empreinte unique ;
//   - pas de fourche (deux maillons sur le même parent) ;
//   - pas de trou (parent introuvable dans la table) ;
//   - pas de redémarrage (parent GENESIS ailleurs qu'au premier maillon) ;
//   - signature à clé des maillons v2.
//
// Le parent d'un maillon peut précéder la période : il est alors cherché dans
// toute la table.
func (v *verifier) linkage(ctx context.Context, s chainSpec, links []link) error {
	if len(links) == 0 {
		return nil
	}
	var earliest sql.NullTime
	if err := v.db.QueryRowContext(ctx, `SELECT min(`+s.dateExpr+`) FROM `+s.table+`
		WHERE merchant_id = ? AND hash IS NOT NULL AND hash <> ''`, v.merchant).Scan(&earliest); err != nil {
		return fmt.Errorf("fiscalverify: %s earliest: %w", s.table, err)
	}
	byHash := make(map[string]int, len(links))
	children := map[string]int{}
	for i, l := range links {
		if j, dup := byHash[l.hash]; dup {
			v.r.add(s.sev(l.version), s.check, "%s et %s : même empreinte %s", links[j].id, l.id, short(l.hash))
		} else {
			byHash[l.hash] = i
		}
		children[l.prev]++
	}
	forked := map[string]bool{}
	for _, l := range links {
		if !s.legacy && l.version >= fiscal.HashVersion && security.SignHash(l.hash) != l.sig {
			v.r.add(SeverityError, s.check, "%s : signature invalide", l.id)
		}
		if isGenesis(l.prev) {
			if earliest.Valid && l.at.After(earliest.Time) {
				v.r.add(s.sev(l.version), s.check, "%s (%s) : la chaîne redémarre (parent GENESIS)", l.id, l.at.UTC().Format(time.RFC3339))
			}
			continue
		}
		if children[l.prev] > 1 && !forked[l.prev] {
			forked[l.prev] = true
			v.r.add(s.sev(l.version), s.check, "fourche : %d maillons sur le parent %s (dont %s)", children[l.prev], short(l.prev), l.id)
		}
		if _, ok := byHash[l.prev]; ok {
			continue
		}
		var exists bool
		if err := v.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+s.table+` WHERE merchant_id = ? AND hash = ?)`,
			v.merchant, l.prev).Scan(&exists); err != nil {
			return fmt.Errorf("fiscalverify: %s parent: %w", s.table, err)
		}
		if !exists {
			v.r.add(s.sev(l.version), s.check, "%s : parent %s introuvable (maillon supprimé ou modifié)", l.id, short(l.prev))
		}
	}
	return nil
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}

// recomputed compare l'empreinte recalculée d'un maillon v2 à la sienne.
func (v *verifier) recomputed(s chainSpec, l link, payload any, err error) {
	if err == nil {
		var h string
		h, _, err = fiscal.Seal(fiscal.Chain(s.table), l.prev, payload)
		if err == nil && h == l.hash {
			return
		}
	}
	if err != nil {
		v.r.add(SeverityError, s.check, "%s : charge illisible (%v)", l.id, err)
		return
	}
	v.r.add(SeverityError, s.check, "%s : empreinte recalculée différente (donnée modifiée après scellement)", l.id)
}

func bytesOf(s sql.NullString) []byte {
	if !s.Valid {
		return nil
	}
	return []byte(s.String)
}

func (v *verifier) payments(ctx context.Context) error {
	s := chainSpec{check: "paiements", label: "Chaîne des paiements", table: "payments", dateExpr: "payment_date"}
	c := v.r.check(s.check, s.label)
	rows, err := v.db.QueryContext(ctx, `
		SELECT payment_id::text, order_id::text, amount, COALESCE(mop, ''), COALESCE(operation_type, ''), payment_date,
		       COALESCE(user_id, ''), comment, COALESCE(previous_hash, ''), hash, COALESCE(signature, ''), hash_version
		FROM payments
		WHERE merchant_id = ? AND hash IS NOT NULL AND hash <> '' AND payment_date >= ? AND payment_date < ?
		ORDER BY payment_date, payment_id`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: payments: %w", err)
	}
	var links []link
	for rows.Next() {
		var l link
		var orderID, mop, op, user string
		var amount int
		var comment sql.NullString
		if err := rows.Scan(&l.id, &orderID, &amount, &mop, &op, &l.at, &user, &comment, &l.prev, &l.hash, &l.sig, &l.version); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: payments: %w", err)
		}
		l.id = "paiement " + l.id
		if l.version >= fiscal.HashVersion {
			var cm *string
			if comment.Valid {
				cm = &comment.String
			}
			p, err := fiscal.NewPaymentPayload(v.merchant, orderID, amount, mop, op, l.at, user, cm)
			v.recomputed(s, l, p, err)
		}
		links = append(links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: payments: %w", err)
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

func (v *verifier) receipts(ctx context.Context) error {
	s := chainSpec{check: "tickets", label: "Chaîne des tickets et avoirs", table: "receipts", dateExpr: "created_at"}
	c := v.r.check(s.check, s.label)
	gaps := v.r.check("tickets_lignes", "Ticket : TTC égal à sa TVA ventilée")
	rows, err := v.db.QueryContext(ctx, `
		SELECT receipt_number, order_id::text, created_at, total_ttc, COALESCE(total_ht, 0), tax_details::text,
		       items_snapshot::text, payments_snapshot::text, COALESCE(prev_hash, ''), hash, COALESCE(signature, ''), hash_version
		FROM receipts
		WHERE merchant_id = ? AND hash IS NOT NULL AND hash <> '' AND created_at >= ? AND created_at < ?
		ORDER BY created_at, receipt_number`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: receipts: %w", err)
	}
	var links []link
	for rows.Next() {
		var l link
		var number, orderID string
		var ttc, ht int
		var tax, items, pays sql.NullString
		if err := rows.Scan(&number, &orderID, &l.at, &ttc, &ht, &tax, &items, &pays, &l.prev, &l.hash, &l.sig, &l.version); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: receipts: %w", err)
		}
		l.id = "ticket " + number
		if l.version >= fiscal.HashVersion {
			p, err := fiscal.NewReceiptPayload(v.merchant, number, orderID, l.at, ttc, ht, bytesOf(tax), bytesOf(items), bytesOf(pays))
			v.recomputed(s, l, p, err)
			if td, ok := fiscal.ParseTaxDetails(bytesOf(tax)); ok {
				gaps.Checked++
				if td.TotalTTC() != int64(ttc) {
					v.r.add(SeverityWarning, gaps.Check, "%s : TTC %d, TVA ventilée %d (prix calculé par la caisse différent de ses lignes, journalisé à l'émission)",
						l.id, ttc, td.TotalTTC())
				}
			}
		}
		links = append(links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: receipts: %w", err)
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

func (v *verifier) cashRegisters(ctx context.Context) error {
	s := chainSpec{check: "registres", label: "Chaîne des fermetures de registre (Z)", table: "cash_registers", dateExpr: "end_date"}
	c := v.r.check(s.check, s.label)
	rows, err := v.db.QueryContext(ctx, `
		SELECT cash_register_id::text, end_date, COALESCE(final_cash_fund, 0), COALESCE(previous_hash, ''), hash,
		       COALESCE(signature, ''), hash_version
		FROM cash_registers
		WHERE merchant_id = ? AND hash IS NOT NULL AND hash <> '' AND end_date >= ? AND end_date < ?
		ORDER BY end_date, cash_register_id`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: cash registers: %w", err)
	}
	type reg struct {
		link
		raw   string
		final int
	}
	var regs []reg
	for rows.Next() {
		var r reg
		if err := rows.Scan(&r.raw, &r.at, &r.final, &r.prev, &r.hash, &r.sig, &r.version); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: cash registers: %w", err)
		}
		r.id = "registre " + r.raw
		regs = append(regs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: cash registers: %w", err)
	}
	links := make([]link, 0, len(regs))
	for _, r := range regs {
		if r.version >= fiscal.HashVersion {
			p, err := fiscal.LoadCashRegisterClosure(ctx, v.db, r.raw, r.at, r.final)
			v.recomputed(s, r.link, p, err)
		}
		links = append(links, r.link)
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

func (v *verifier) auditLogs(ctx context.Context) error {
	s := chainSpec{check: "journal", label: "Chaîne du journal d'audit", table: "audit_logs", dateExpr: "created_at"}
	c := v.r.check(s.check, s.label)
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, COALESCE(user_id, ''), COALESCE(action, ''), COALESCE(resource_type, ''), COALESCE(resource_id, ''),
		       created_at, old_values::text, new_values::text, COALESCE(previous_hash, ''), hash, COALESCE(signature, ''), hash_version
		FROM audit_logs
		WHERE merchant_id = ? AND hash IS NOT NULL AND hash <> '' AND created_at >= ? AND created_at < ?
		ORDER BY created_at, id`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: audit logs: %w", err)
	}
	var links []link
	for rows.Next() {
		var l link
		var user, action, resType, resID string
		var oldV, newV sql.NullString
		if err := rows.Scan(&l.id, &user, &action, &resType, &resID, &l.at, &oldV, &newV, &l.prev, &l.hash, &l.sig, &l.version); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: audit logs: %w", err)
		}
		rawID := l.id
		l.id = "entrée " + rawID
		if l.version >= fiscal.HashVersion {
			p, err := fiscal.NewAuditLogPayload(rawID, v.merchant, user, action, resType, resID, l.at, bytesOf(oldV), bytesOf(newV))
			v.recomputed(s, l, p, err)
		}
		// Les entrées longues (états complets de commande) ne sont pas
		// gardées : seul le maillon l'est.
		links = append(links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: audit logs: %w", err)
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

func (v *verifier) closureChain(ctx context.Context) error {
	s := chainSpec{check: "clotures_chaine", label: "Chaîne des clôtures fiscales", table: "fiscal_closures", dateExpr: "closed_at"}
	c := v.r.check(s.check, s.label)
	rows, err := v.db.QueryContext(ctx, `
		SELECT id FROM fiscal_closures WHERE merchant_id = ? AND closed_at >= ? AND closed_at < ?
		ORDER BY closed_at, id`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: closures: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: closures: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: closures: %w", err)
	}
	links := make([]link, 0, len(ids))
	for _, id := range ids {
		sc, err := fiscal.LoadClosure(ctx, v.db, id)
		if err != nil {
			return err
		}
		at, _ := time.Parse(time.RFC3339Nano, sc.Payload.ClosedAt)
		l := link{id: fmt.Sprintf("clôture %s %s", sc.Payload.PeriodType, sc.Payload.PeriodStart), at: at,
			prev: sc.Prev, hash: sc.Hash, sig: sc.Signature, version: sc.Version}
		v.recomputed(s, l, sc.Payload, nil)
		links = append(links, l)
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

func (v *verifier) archives(ctx context.Context) error {
	s := chainSpec{check: "archives", label: "Chaîne des archives fiscales (et fichiers, si relus)", table: "fiscal_archives", dateExpr: "generated_at"}
	c := v.r.check(s.check, s.label)
	all, err := fiscalarchive.List(ctx, v.db, v.merchant)
	if err != nil {
		return err
	}
	var links []link
	for i := len(all) - 1; i >= 0; i-- { // du plus ancien au plus récent
		a := all[i]
		if a.GeneratedAt.Before(v.from) || !a.GeneratedAt.Before(v.to) {
			continue
		}
		l := link{id: "archive " + a.Filename, at: a.GeneratedAt, prev: a.PreviousHash, hash: a.Hash, sig: a.Signature, version: fiscal.HashVersion}
		v.recomputed(s, l, fiscalarchive.PayloadOf(a), nil)
		links = append(links, l)
		if v.opts.Archives == nil {
			continue
		}
		file, err := v.opts.Archives.GetFile(ctx, a.R2Key)
		if err != nil {
			v.r.add(SeverityError, s.check, "%s : fichier illisible dans le stockage (%v)", l.id, err)
			continue
		}
		if fmt.Sprintf("%x", sha256.Sum256(file)) != a.SHA256 || int64(len(file)) != a.SizeBytes {
			v.r.add(SeverityError, s.check, "%s : le fichier stocké ne correspond pas à l'empreinte scellée", l.id)
			continue
		}
		rep, err := fiscalarchive.Verify(file)
		if err != nil {
			v.r.add(SeverityError, s.check, "%s : archive illisible (%v)", l.id, err)
			continue
		}
		if !rep.OK() {
			v.r.add(SeverityError, s.check, "%s : contrôle croisé en écart (%d anomalie(s), dont : %s)", l.id, rep.Anomalies, rep.Problems[0])
		}
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

func (v *verifier) ordersChain(ctx context.Context) error {
	const at = "COALESCE(delivered_on, last_update, creation_date)"
	s := chainSpec{check: "commandes_chaine", label: "Ancienne chaîne des commandes (retirée au lot B) : chaînage seul",
		table: "orders", dateExpr: at, legacy: true}
	c := v.r.check(s.check, s.label)
	rows, err := v.db.QueryContext(ctx, `
		SELECT order_id::text, `+at+`, COALESCE(previous_hash, ''), hash, COALESCE(signature, ''), hash_version
		FROM orders
		WHERE merchant_id = ? AND hash IS NOT NULL AND hash <> '' AND `+at+` >= ? AND `+at+` < ?
		ORDER BY `+at+`, order_id`, v.merchant, v.from, v.to)
	if err != nil {
		return fmt.Errorf("fiscalverify: orders chain: %w", err)
	}
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.id, &l.at, &l.prev, &l.hash, &l.sig, &l.version); err != nil {
			rows.Close()
			return fmt.Errorf("fiscalverify: orders chain: %w", err)
		}
		l.id = "commande " + l.id
		links = append(links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fiscalverify: orders chain: %w", err)
	}
	c.Checked = len(links)
	return v.linkage(ctx, s, links)
}

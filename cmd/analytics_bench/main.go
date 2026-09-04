// analytics_bench is PROMPT 04 Phase 3's measurement kit: it fills
// docs/analytics/MESURES.md (wello-back-office repo) in one command, run by
// the product owner against staging AFTER promotion — never from this
// development environment (staging's CPU is measured 11-33x slower than a
// normal core with 3x run-to-run variance, PERIMETRE.md §2.1; a number
// gathered anywhere else is not just useless but actively misleading).
//
// Three modes, one binary:
//
//	grid        endpoint x establishment(s) x window x include_ht x cache
//	            timing grid over HTTP, PERF-INDEX.md §1.2's protocol (5 runs,
//	            first discarded, median of the rest, flag >1.5x spread).
//	pos-impact  orchestrates the POS-probe / analytical-load / recovery-time
//	            measurement — the most important number in this kit, see
//	            runPosImpact's doc comment. Needs POSTGRES_URL for the probe
//	            (direct DB, read-only) and --base-url for the load (HTTP).
//	fusible     spot-checks: does a 12-month include_ht=true CA query approach
//	            the 4s statement_timeout? Does the heaviest HT query spill to
//	            disk under 16MB work_mem (EXPLAIN ANALYZE, direct DB, read
//	            only)? What happens when 3 uncached requests hit the 2-connection
//	            analytics pool at once?
//
// Run this at 3h-5h — pos-impact and fusible both load the database and must
// never run during service.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	mode := flag.String("mode", "grid", `"grid", "pos-impact", or "fusible"`)
	baseURL := flag.String("base-url", "", "staging API base URL, e.g. https://welloresto-api-staging.onrender.com (required for grid and pos-impact)")
	merchantsFlag := flag.String("merchants", "212,228,225,226,231,235,234,236", "comma-separated PROD merchant IDs — first one is used as \"the biggest\" (1-établissement runs)")
	tokensFile := flag.String("tokens-file", "", "path to a JSON file {\"merchant_id\": \"bearer token\"} — one token per establishment (each token is scoped to exactly one merchant, see ResolveAccessibleMerchants). NEVER commit this file.")
	postgresURL := flag.String("postgres-url", os.Getenv("POSTGRES_URL"), "direct DB connection, read-only use only — required for pos-impact and fusible")
	out := flag.String("out", "", "write the markdown table(s) to this file instead of stdout")
	windowMonths := flag.String("windows", "1,12,24", "comma-separated window widths in months, for grid mode")
	posImpactDuration := flag.Duration("pos-impact-followup", 3*time.Minute, "how long to keep probing after the analytical load finishes")
	flag.Parse()

	var tokens map[string]string
	if *tokensFile != "" {
		data, err := os.ReadFile(*tokensFile)
		if err != nil {
			log.Fatalf("read tokens file: %v", err)
		}
		if err := json.Unmarshal(data, &tokens); err != nil {
			log.Fatalf("parse tokens file: %v", err)
		}
	}
	merchants := strings.Split(*merchantsFlag, ",")

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatalf("create output file: %v", err)
		}
		defer f.Close()
		w = f
	}

	switch *mode {
	case "grid":
		if *baseURL == "" || tokens == nil {
			log.Fatal("grid mode requires --base-url and --tokens-file")
		}
		windows := parseIntList(*windowMonths)
		runGrid(w, *baseURL, merchants, tokens, windows)
	case "pos-impact":
		if *baseURL == "" || tokens == nil {
			log.Fatal("pos-impact mode requires --base-url and --tokens-file")
		}
		if *postgresURL == "" {
			log.Fatal("pos-impact mode requires --postgres-url (or POSTGRES_URL env)")
		}
		runPosImpact(w, *baseURL, merchants, tokens, *postgresURL, *posImpactDuration)
	case "fusible":
		if *postgresURL == "" {
			log.Fatal("fusible mode requires --postgres-url (or POSTGRES_URL env)")
		}
		if *baseURL == "" || tokens == nil {
			log.Fatal("fusible mode's 3-concurrent-connections check requires --base-url and --tokens-file")
		}
		runFusible(w, *baseURL, merchants, tokens, *postgresURL)
	default:
		log.Fatalf("unknown --mode %q", *mode)
	}
}

func parseIntList(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// ---- HTTP client for the analytics endpoints ----

type analyticsClient struct {
	baseURL string
	http    *http.Client
}

func newAnalyticsClient(baseURL string) *analyticsClient {
	return &analyticsClient{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
}

type callResult struct {
	durationMS   float64
	rowsRendered int
	statusCode   int
	err          error
}

// call POSTs to one analytics endpoint and reports wall-clock duration —
// "durée bout-en-bout", the only timing this script can see from outside the
// process. "Durée serveur" is emitted server-side as a structured log line
// (analytics_query, internal/modules/analytics/service.go's
// logInstrumentation) — correlate by timestamp in Render's log viewer for
// the slow points this script flags; this script does not invent a
// server-timing header that doesn't exist.
func (c *analyticsClient) call(ctx context.Context, path, token string, body map[string]interface{}) callResult {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return callResult{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	started := time.Now()
	resp, err := c.http.Do(req)
	duration := time.Since(started)
	if err != nil {
		return callResult{durationMS: float64(duration.Milliseconds()), err: err}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	rows := countRenderedRows(respBody)
	return callResult{durationMS: float64(duration.Milliseconds()), rowsRendered: rows, statusCode: resp.StatusCode}
}

// countRenderedRows sums the length of every top-level array in the
// response's "data" object (timeline, by_channel, by_method, by_rate, ...) —
// a rough but endpoint-agnostic proxy for "lignes rendues" without needing a
// per-endpoint response type here.
func countRenderedRows(body []byte) int {
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0
	}
	total := 0
	for _, v := range envelope.Data {
		if arr, ok := v.([]interface{}); ok {
			total += len(arr)
		}
	}
	return total
}

// ---- Timing protocol: PERF-INDEX.md §1.2 — 5 runs, first discarded, median
// of the rest, flag >1.5x spread as significant (this instance's variance is
// ~3x run to run). ----

type timingSample struct {
	medianMS   float64
	spreadFlag bool
	samples    []float64
	lastResult callResult
}

func timeIt(ctx context.Context, n int, fn func() callResult) timingSample {
	var kept []float64
	var last callResult
	for i := 0; i < n; i++ {
		r := fn()
		last = r
		if i == 0 {
			continue // first run discarded — warms whatever there is to warm
		}
		if r.err == nil {
			kept = append(kept, r.durationMS)
		}
	}
	sort.Float64s(kept)
	median := 0.0
	if len(kept) > 0 {
		mid := len(kept) / 2
		if len(kept)%2 == 0 && mid > 0 {
			median = (kept[mid-1] + kept[mid]) / 2
		} else {
			median = kept[mid]
		}
	}
	spread := false
	if len(kept) > 0 && kept[0] > 0 {
		spread = kept[len(kept)-1]/kept[0] > 1.5
	}
	return timingSample{medianMS: median, spreadFlag: spread, samples: kept, lastResult: last}
}

// ---- Mode: grid ----

type gridEndpoint struct {
	name string
	path string
}

var gridEndpoints = []gridEndpoint{
	{"CA", "/analytics/revenue"},
	{"Commandes", "/analytics/orders"},
	{"Règlements", "/analytics/payments"},
	{"TVA", "/analytics/vat"},
}

func runGrid(w io.Writer, baseURL string, merchants []string, tokens map[string]string, windowMonths []int) {
	client := newAnalyticsClient(baseURL)
	ctx := context.Background()

	fmt.Fprintln(w, "# Grille de mesure des endpoints")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Protocole PERF-INDEX.md §1.2 : 5 exécutions, la première jetée, médiane des 4 restantes.")
	fmt.Fprintln(w, "\"⚠ écart >×1.5\" signale une variance significative sur cette instance (variance mesurée ×3 d'une exécution à l'autre).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Endpoint | Établissements | Fenêtre | include_ht | Cache | Durée bout-en-bout médiane (ms) | Lignes rendues | Statut | Note |")
	fmt.Fprintln(w, "|---|---|---|---|---|---:|---:|---|---|")

	dateTo := time.Now()

	for _, ep := range gridEndpoints {
		includeHTValues := []bool{true} // irrelevant for non-CA endpoints — kept single-valued
		if ep.name == "CA" {
			includeHTValues = []bool{true, false}
		}

		for _, months := range windowMonths {
			dateFrom := dateTo.AddDate(0, -months, 0)
			for _, includeHT := range includeHTValues {
				// 1 établissement (le plus gros)
				runGridPoint(ctx, w, client, ep, "1 ("+merchants[0]+")", months, includeHT, tokens[merchants[0]], dateFrom, dateTo, merchants[:1])

				// 8 établissements — appels séparés (ResolveAccessibleMerchants
				// scopes one token to exactly one merchant; see MESURES.md's
				// note on this constraint), reported as one aggregate row.
				runGridPointMultiMerchant(ctx, w, client, ep, months, includeHT, tokens, dateFrom, dateTo, merchants)
			}
		}
	}
}

func runGridPoint(ctx context.Context, w io.Writer, client *analyticsClient, ep gridEndpoint, scopeLabel string, months int, includeHT bool, token string, dateFrom, dateTo time.Time, _ []string) {
	if token == "" {
		fmt.Fprintf(w, "| %s | %s | %d mois | %v | froid+chaud | — | — | pas de token | aucun token pour cet établissement dans --tokens-file |\n", ep.name, scopeLabel, months, includeHT)
		return
	}
	body := map[string]interface{}{
		"date_from": dateFrom.Format("2006-01-02"),
		"date_to":   dateTo.Format("2006-01-02"),
	}
	if ep.name == "CA" {
		body["include_ht"] = includeHT
	}

	// Froid : fenêtre inédite (décalée d'une seconde pour casser le cache).
	coldFrom := dateFrom.Add(-time.Second)
	coldBody := map[string]interface{}{"date_from": coldFrom.Format("2006-01-02"), "date_to": dateTo.Format("2006-01-02")}
	if ep.name == "CA" {
		coldBody["include_ht"] = includeHT
	}
	cold := timeIt(ctx, 5, func() callResult { return client.call(ctx, ep.path, token, coldBody) })
	emitGridRow(w, ep.name, scopeLabel, months, includeHT, "froid", cold)

	// Chaud : même requête répétée (doit taper le cache Redis, TTL 5 min).
	warm := timeIt(ctx, 5, func() callResult { return client.call(ctx, ep.path, token, body) })
	emitGridRow(w, ep.name, scopeLabel, months, includeHT, "chaud", warm)
}

func runGridPointMultiMerchant(ctx context.Context, w io.Writer, client *analyticsClient, ep gridEndpoint, months int, includeHT bool, tokens map[string]string, dateFrom, dateTo time.Time, merchants []string) {
	body := map[string]interface{}{"date_from": dateFrom.Format("2006-01-02"), "date_to": dateTo.Format("2006-01-02")}
	if ep.name == "CA" {
		body["include_ht"] = includeHT
	}

	var total float64
	var flagged bool
	var missing []string
	for _, m := range merchants {
		token, ok := tokens[m]
		if !ok || token == "" {
			missing = append(missing, m)
			continue
		}
		sample := timeIt(ctx, 5, func() callResult { return client.call(ctx, ep.path, token, body) })
		total += sample.medianMS
		flagged = flagged || sample.spreadFlag
	}
	note := "somme des 8 appels séparés (contrainte ResolveAccessibleMerchants — voir MESURES.md)"
	if len(missing) > 0 {
		note += fmt.Sprintf("; tokens manquants pour %s", strings.Join(missing, ","))
	}
	status := "OK"
	if flagged {
		status = "⚠ écart >×1.5 sur au moins un établissement"
	}
	fmt.Fprintf(w, "| %s | 8 (appels séparés) | %d mois | %v | froid | %.0f | — | %s | %s |\n", ep.name, months, includeHT, total, status, note)
}

func emitGridRow(w io.Writer, endpoint, scope string, months int, includeHT bool, cache string, s timingSample) {
	status := "OK"
	if s.lastResult.err != nil {
		status = "ERREUR: " + s.lastResult.err.Error()
	} else if s.lastResult.statusCode == 504 || s.lastResult.statusCode == 500 {
		status = fmt.Sprintf("HTTP %d — possible statement_timeout (4000ms)", s.lastResult.statusCode)
	} else if s.spreadFlag {
		status = "⚠ écart >×1.5 entre exécutions"
	}
	fmt.Fprintf(w, "| %s | %s | %d mois | %v | %s | %.0f | %d | %s | |\n",
		endpoint, scope, months, includeHT, cache, s.medianMS, s.lastResult.rowsRendered, status)
}

// ---- Mode: pos-impact ----

// runPosImpact is the kit's most important measurement: does a 12-month
// analytical query evict the POS's hot working set from Postgres's 64MB
// shared_buffers, and if so, how long does the POS stay slow afterward?
// Never tested before this kit — the central, untested fear behind the
// whole chantier (PROMPT 04).
//
// The probe query mirrors internal/modules/order_life_cycle's
// GetPaymentsForOrder (SELECT ... FROM payments WHERE order_id = ?) plus an
// orderitems-by-order_id read — the exact shape of a POS ticket screen's
// read, and the query migration 087's indexes take from ~35ms to ~0.2ms.
type probePoint struct {
	t          time.Time
	durationMS float64
}

func runPosImpact(w io.Writer, baseURL string, merchants []string, tokens map[string]string, postgresURL string, followup time.Duration) {
	db, err := sql.Open("pgx", postgresURL)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // the probe is a single dedicated connection, deliberately outside the analytics/POS pools

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	orderID, err := pickRecentOrderID(ctx, db, merchants[0])
	if err != nil {
		log.Fatalf("pick a recent order to probe: %v", err)
	}
	fmt.Fprintf(w, "# Impact POS — sonde sur la commande %s (établissement %s)\n\n", orderID, merchants[0])
	fmt.Fprintln(w, "Sonde chaque seconde : `SELECT * FROM orderitems WHERE order_id = ?` + `SELECT ... FROM payments WHERE order_id = ?`")
	fmt.Fprintln(w, "(même requête que order_life_cycle.GetPaymentsForOrder — le chemin le plus chaud du POS).")
	fmt.Fprintln(w)

	var points []probePoint
	var mu sync.Mutex
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				d := probeOnce(ctx, db, orderID)
				mu.Lock()
				points = append(points, probePoint{t: time.Now(), durationMS: d})
				mu.Unlock()
			}
		}
	}()

	fmt.Fprintln(w, "## Référence (30s avant charge)")
	time.Sleep(30 * time.Second)
	baseline := medianOfRecent(&mu, &points, 30)
	fmt.Fprintf(w, "- Référence (médiane, 30 derniers points) : %.3f ms\n\n", baseline)

	fmt.Fprintln(w, "## Charge analytique (12 mois × 8 établissements)")
	loadStart := time.Now()
	runAnalyticalLoad(baseURL, tokens, merchants)
	loadDuration := time.Since(loadStart)
	fmt.Fprintf(w, "- Durée de la charge : %s\n\n", loadDuration)

	fmt.Fprintln(w, "## Retour à la normale")
	deadline := time.Now().Add(followup)
	var peak float64
	var recoveredAt *time.Time
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		mu.Lock()
		recent := lastNDurations(points, 5)
		mu.Unlock()
		if len(recent) == 0 {
			continue
		}
		m := median(recent)
		if m > peak {
			peak = m
		}
		if recoveredAt == nil && m <= baseline*1.5 {
			t := time.Now()
			recoveredAt = &t
		}
	}
	close(stop)
	wg.Wait()

	fmt.Fprintf(w, "- Pic pendant la charge : %.3f ms\n", peak)
	if recoveredAt != nil {
		fmt.Fprintf(w, "- Retour à la normale (≤×1.5 de la référence) : %s après la fin de la charge\n", recoveredAt.Sub(loadStart.Add(loadDuration)))
	} else {
		fmt.Fprintf(w, "- Retour à la normale : PAS ATTEINT dans la fenêtre de suivi (%s) — la sonde est toujours dégradée\n", followup)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Rejouer cette mesure avant et après l'application de la migration 087 (index) — l'écart entre les deux est l'argument de déploiement en production.")
}

func pickRecentOrderID(ctx context.Context, db *sql.DB, merchantID string) (string, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var orderID string
	err = tx.QueryRowContext(ctx, `
		SELECT order_id FROM orders
		WHERE merchant_id = $1
		ORDER BY creation_date DESC
		LIMIT 1`, merchantID).Scan(&orderID)
	return orderID, err
}

func probeOnce(ctx context.Context, db *sql.DB, orderID string) float64 {
	started := time.Now()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return -1
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT order_item_id FROM orderitems WHERE order_id = $1`, orderID)
	if err == nil {
		for rows.Next() {
		}
		rows.Close()
	}
	rows2, err := tx.QueryContext(ctx, `SELECT payment_id, mop, amount FROM payments WHERE order_id = $1`, orderID)
	if err == nil {
		for rows2.Next() {
		}
		rows2.Close()
	}
	return float64(time.Since(started).Microseconds()) / 1000.0
}

func medianOfRecent(mu *sync.Mutex, points *[]probePoint, n int) float64 {
	mu.Lock()
	defer mu.Unlock()
	vals := lastNDurations(*points, n)
	return median(vals)
}

func lastNDurations(points []probePoint, n int) []float64 {
	if len(points) == 0 {
		return nil
	}
	start := 0
	if len(points) > n {
		start = len(points) - n
	}
	var out []float64
	for _, p := range points[start:] {
		if p.durationMS >= 0 {
			out = append(out, p.durationMS)
		}
	}
	return out
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// runAnalyticalLoad fires the 12-month CA query (include_ht=true, the
// heaviest shape) against all 8 establishments, sequentially — mirroring 8
// separate real users opening the page, not one artificially parallel burst.
func runAnalyticalLoad(baseURL string, tokens map[string]string, merchants []string) {
	client := newAnalyticsClient(baseURL)
	ctx := context.Background()
	dateTo := time.Now()
	dateFrom := dateTo.AddDate(0, -12, 0)
	body := map[string]interface{}{
		"date_from":  dateFrom.Format("2006-01-02"),
		"date_to":    dateTo.Format("2006-01-02"),
		"include_ht": true,
	}
	for _, m := range merchants {
		token, ok := tokens[m]
		if !ok {
			continue
		}
		client.call(ctx, "/analytics/revenue", token, body)
	}
}

// ---- Mode: fusible ----

func runFusible(w io.Writer, baseURL string, merchants []string, tokens map[string]string, postgresURL string) {
	db, err := sql.Open("pgx", postgresURL)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	fmt.Fprintln(w, "# Vérification du fusible")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## 1. statement_timeout (4000ms) sur la requête CA 12 mois + include_ht=true")
	client := newAnalyticsClient(baseURL)
	dateTo := time.Now()
	dateFrom := dateTo.AddDate(0, -12, 0)
	body := map[string]interface{}{
		"date_from": dateFrom.Format("2006-01-02"), "date_to": dateTo.Format("2006-01-02"), "include_ht": true,
	}
	token := tokens[merchants[0]]
	if token == "" {
		fmt.Fprintln(w, "pas de token pour le premier établissement — non mesuré")
	} else {
		sample := timeIt(ctx, 5, func() callResult { return client.call(ctx, "/analytics/revenue", token, body) })
		fmt.Fprintf(w, "- Médiane : %.0f ms\n", sample.medianMS)
		if sample.medianMS >= 4000 {
			fmt.Fprintln(w, "- ⚠ APPROCHE OU DÉPASSE le statement_timeout de 4000ms — le fusible saute en usage normal, revoir le réglage.")
		} else {
			fmt.Fprintf(w, "- Marge avant le fusible : %.0f ms\n", 4000-sample.medianMS)
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## 2. Spill disque (work_mem 16MB) — EXPLAIN ANALYZE sur la requête HT la plus lourde")
	fmt.Fprintln(w, "Nécessite une connexion directe en lecture seule (READ ONLY) — pas de mesure de temps fiable depuis cet environnement,")
	fmt.Fprintln(w, "seule la présence d'un spill (`Sort Method: external merge` / `temp_written_blocks` dans le plan) est pertinente à relever ici.")
	explainSpill(ctx, w, db, merchants[0])
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## 3. Trois requêtes analytiques non-cachées simultanées (AnalyticsMaxOpenConns=2)")
	runConcurrencyCheck(ctx, w, client, tokens, merchants)
}

func explainSpill(ctx context.Context, w io.Writer, db *sql.DB, merchantID string) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		fmt.Fprintf(w, "- erreur ouverture transaction: %v\n", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL work_mem = '16MB'"); err != nil {
		fmt.Fprintf(w, "- erreur SET LOCAL work_mem: %v\n", err)
		return
	}
	dateTo := time.Now().UTC()
	dateFrom := dateTo.AddDate(0, -12, 0)
	rows, err := tx.QueryContext(ctx, `
		EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
		SELECT ROUND(CAST(COALESCE(SUM(
			CASE WHEN tva.tva_rate = 0 THEN ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)
			ELSE ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) * 100.0 / (100.0 + tva.tva_rate) END
		), 0) AS numeric), 0)
		FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		INNER JOIN products p ON p.product_id = oi.product_id
		INNER JOIN tva_categories tva ON tva.tva_id = (
			CASE WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id
			WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id ELSE p.tva_in_id END)
		LEFT JOIN (SELECT order_item_id, SUM(extra.price) AS extra_price FROM extra GROUP BY order_item_id) e
			ON e.order_item_id = oi.order_item_id
		WHERE o.merchant_id = $1 AND o.creation_date >= $2 AND o.creation_date < $3
		  AND o.state IN ('CLOSED','DONE') AND upper(o.brand_status) NOT IN ('DELETED','CANCELED')`,
		merchantID, dateFrom, dateTo)
	if err != nil {
		fmt.Fprintf(w, "- erreur EXPLAIN: %v\n", err)
		return
	}
	defer rows.Close()
	spillFound := false
	fmt.Fprintln(w, "```")
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err == nil {
			fmt.Fprintln(w, line)
			if strings.Contains(line, "external merge") || strings.Contains(line, "Disk:") {
				spillFound = true
			}
		}
	}
	fmt.Fprintln(w, "```")
	if spillFound {
		fmt.Fprintln(w, "- ⚠ SPILL DISQUE DÉTECTÉ à 16MB work_mem.")
	} else {
		fmt.Fprintln(w, "- Pas de spill détecté dans ce plan (à confirmer sur un établissement/fenêtre plus lourds si besoin).")
	}
}

func runConcurrencyCheck(ctx context.Context, w io.Writer, client *analyticsClient, tokens map[string]string, merchants []string) {
	if len(merchants) < 3 {
		fmt.Fprintln(w, "- besoin d'au moins 3 établissements avec token pour ce test — ignoré")
		return
	}
	dateTo := time.Now()
	var wg sync.WaitGroup
	results := make([]callResult, 3)
	starts := make([]time.Time, 3)
	for i := 0; i < 3; i++ {
		token := tokens[merchants[i]]
		// Fenêtres légèrement différentes par établissement pour éviter que
		// le cache Redis serve la 2e/3e requête au lieu de les faire
		// contendre sur le pool.
		dateFrom := dateTo.AddDate(0, -12-i, 0)
		body := map[string]interface{}{"date_from": dateFrom.Format("2006-01-02"), "date_to": dateTo.Format("2006-01-02"), "include_ht": true}
		wg.Add(1)
		go func(i int, token string, body map[string]interface{}) {
			defer wg.Done()
			starts[i] = time.Now()
			results[i] = client.call(ctx, "/analytics/revenue", token, body)
		}(i, token, body)
	}
	wg.Wait()
	for i, r := range results {
		status := "OK"
		if r.err != nil {
			status = "ERREUR: " + r.err.Error()
		} else if r.statusCode != 200 {
			status = fmt.Sprintf("HTTP %d", r.statusCode)
		}
		fmt.Fprintf(w, "- établissement %s : %.0f ms, %s\n", merchants[i], r.durationMS, status)
	}
	fmt.Fprintln(w, "- Observer ci-dessus si la 3e requête attend (durée nettement supérieure aux deux autres), échoue, ou passe sans contention visible.")
}

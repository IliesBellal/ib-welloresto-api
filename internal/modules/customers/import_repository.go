package customers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/customers/importer"
)

// lookupChunkSize borne la taille d'une clause IN pour les lookups de preview
// d'import. À l'échelle réelle (~18 500 lignes), une seule requête à des
// milliers de paramètres est à éviter ; le paquet garde une taille de plan de
// requête raisonnable. L'exécution reste de toute façon séquentielle (pool à
// une connexion, cf. internal/database/mysql.go) — cette constante borne le
// NOMBRE de paramètres par requête, pas le nombre de requêtes.
const lookupChunkSize = 900

// LoadExistingByEmails résout, pour un marchand donné, les emails déjà
// portés par un client actif. La clé de la map rendue est l'email en
// minuscule (comme lowerEmails en entrée) : la dédup se fait insensible à la
// casse — audit clients §1.5.
func (r *CustomersRepository) LoadExistingByEmails(ctx context.Context, merchantID string, lowerEmails []string) (map[string]int, error) {
	result := make(map[string]int, len(lowerEmails))
	if len(lowerEmails) == 0 {
		return result, nil
	}

	db := dbx.GetDB(ctx, r.database)

	for _, chunk := range chunkStrings(lowerEmails, lookupChunkSize) {
		query := fmt.Sprintf(`
			SELECT LOWER(customer_email), customer_id
			FROM customer
			WHERE merchant_id = ? AND enabled = TRUE
			  AND customer_email IS NOT NULL AND customer_email <> ''
			  AND LOWER(customer_email) IN (%s)
		`, placeholders(len(chunk)))

		rows, err := db.QueryContext(ctx, query, chunkArgs(merchantID, chunk)...)
		if err != nil {
			return nil, fmt.Errorf("lookup emails pour preview d'import clients: %w", err)
		}
		if err := scanIntoStringIntMap(rows, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// LoadExistingByPhones résout, pour un marchand donné, les téléphones déjà
// portés par un client actif. normalizedPhones est déjà normalisé au format
// FR par l'appelant (importer.CanonicalCustomer.Phone) : aucune
// normalisation n'est refaite ici.
func (r *CustomersRepository) LoadExistingByPhones(ctx context.Context, merchantID string, normalizedPhones []string) (map[string]int, error) {
	result := make(map[string]int, len(normalizedPhones))
	if len(normalizedPhones) == 0 {
		return result, nil
	}

	db := dbx.GetDB(ctx, r.database)

	for _, chunk := range chunkStrings(normalizedPhones, lookupChunkSize) {
		query := fmt.Sprintf(`
			SELECT customer_tel, customer_id
			FROM customer
			WHERE merchant_id = ? AND enabled = TRUE
			  AND customer_tel IS NOT NULL AND customer_tel <> ''
			  AND customer_tel IN (%s)
		`, placeholders(len(chunk)))

		rows, err := db.QueryContext(ctx, query, chunkArgs(merchantID, chunk)...)
		if err != nil {
			return nil, fmt.Errorf("lookup telephones pour preview d'import clients: %w", err)
		}
		if err := scanIntoStringIntMap(rows, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// LoadImportMappings résout, pour un marchand et un provider donnés, les
// external_id déjà mappés vers un client Wello par un import précédent.
// TargetExists distingue un mapping encore valide (le client existe et est
// actif) d'un mapping périmé (le client a été supprimé depuis) — audit
// produit §5.1.
func (r *CustomersRepository) LoadImportMappings(ctx context.Context, merchantID, provider string, externalIDs []string) (map[string]importer.MappingEntry, error) {
	result := make(map[string]importer.MappingEntry, len(externalIDs))
	if len(externalIDs) == 0 {
		return result, nil
	}

	db := dbx.GetDB(ctx, r.database)

	for _, chunk := range chunkStrings(externalIDs, lookupChunkSize) {
		query := fmt.Sprintf(`
			SELECT m.external_id, m.wello_id, (c.customer_id IS NOT NULL)
			FROM import_customers_mapping m
			LEFT JOIN customer c
			  ON c.customer_id = m.wello_id AND c.merchant_id = m.merchant_id AND c.enabled = TRUE
			WHERE m.merchant_id = ? AND m.provider = ? AND m.external_id IN (%s)
		`, placeholders(len(chunk)))

		args := make([]interface{}, 0, len(chunk)+2)
		args = append(args, merchantID, provider)
		for _, id := range chunk {
			args = append(args, id)
		}

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("lookup mappings pour preview d'import clients: %w", err)
		}
		if err := scanIntoMappingMap(rows, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// placeholders rend n points d'interrogation séparés par des virgules, pour
// une clause IN (...).
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// chunkArgs assemble les arguments d'une requête à un seul paramètre scalaire
// (merchantID) suivi de la clause IN.
func chunkArgs(merchantID string, chunk []string) []interface{} {
	args := make([]interface{}, 0, len(chunk)+1)
	args = append(args, merchantID)
	for _, v := range chunk {
		args = append(args, v)
	}
	return args
}

// chunkStrings découpe items en paquets d'au plus size éléments, dans
// l'ordre. Une liste vide rend nil (aucun paquet, donc aucune requête).
func chunkStrings(items []string, size int) [][]string {
	if len(items) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(items)+size-1)/size)
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}

// scanIntoStringIntMap lit les lignes (clé texte, id) d'un lookup email ou
// téléphone et les fusionne dans dest. Referme rows avant de rendre la main :
// le pool n'a qu'une connexion, la relâcher est nécessaire avant le chunk
// suivant.
func scanIntoStringIntMap(rows *sql.Rows, dest map[string]int) error {
	defer rows.Close()
	for rows.Next() {
		var key string
		var id int
		if err := rows.Scan(&key, &id); err != nil {
			return fmt.Errorf("lecture lookup preview d'import clients: %w", err)
		}
		dest[key] = id
	}
	return rows.Err()
}

// scanIntoMappingMap lit les lignes (external_id, wello_id, cible_existe) et
// les fusionne dans dest.
func scanIntoMappingMap(rows *sql.Rows, dest map[string]importer.MappingEntry) error {
	defer rows.Close()
	for rows.Next() {
		var externalID string
		var welloID int
		var targetExists bool
		if err := rows.Scan(&externalID, &welloID, &targetExists); err != nil {
			return fmt.Errorf("lecture mapping preview d'import clients: %w", err)
		}
		dest[externalID] = importer.MappingEntry{CustomerID: welloID, TargetExists: targetExists}
	}
	return rows.Err()
}

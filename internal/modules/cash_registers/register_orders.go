package cash_registers

import "welloresto-api/internal/models"

// Commandes d'un registre de caisse : la clé commune de la TVA du ticket Z et
// du PDF du registre (docs/ECART_REGISTRES_BORNE.md).
//
// Une commande de caisse porte son registre dans orders.cash_register_id. Une
// commande de borne porte la sentinelle 'KIOSK' et n'en change jamais : seul son
// paiement (MOP 'KIOSK', ou celui du caissier après un « payer en caisse ») est
// rattaché à un registre, à la clôture de celui-ci (CloseCashRegister, étape
// 3bis). Les encaissements du registre, calculés sur payments.cash_register_id,
// contiennent donc la vente borne, et la TVA, calculée sur orders.cash_register_id,
// l'ignorait : l'écart entre les deux égalait les ventes borne du registre.
//
// Une commande borne est rattachée au registre de ses paiements actifs ; si
// elle en a dans plusieurs (cas rare, un encaissement repris sur un autre
// registre), au plus petit — elle n'est ainsi comptée qu'une fois. La même règle
// est appliquée aux données par cmd/backfill_kiosk_order_registers.

// registerOrdersPlaceholders : nombre de fois où l'identifiant du registre est
// attendu par RegisterOrdersSQL.
const registerOrdersPlaceholders = 3

// RegisterOrdersSQL renvoie le prédicat « la commande alias appartient au
// registre », à insérer dans un WHERE. Il porte registerOrdersPlaceholders
// paramètres `?`, tous égaux à l'identifiant du registre (RegisterOrdersArgs).
// Les paiements sont lus par payments.cash_register_id, la colonne que le
// théorique du registre agrège déjà ; le test numérique est protégé par un CASE
// car la colonne porte aussi des sentinelles (SCANNORDER, UBER_EATS...).
func RegisterOrdersSQL(alias string) string {
	return `(` + alias + `.cash_register_id = ?
		OR (` + alias + `.cash_register_id = '` + models.KioskCashRegisterID + `'
			AND ` + alias + `.order_id IN (
				SELECT p.order_id FROM payments p
				WHERE p.cash_register_id = ? AND p.enabled IS TRUE)
			AND NOT EXISTS (
				SELECT 1 FROM payments p2
				WHERE p2.order_id = ` + alias + `.order_id AND p2.enabled IS TRUE
				  AND CASE WHEN p2.cash_register_id ~ '^[0-9]+$' THEN p2.cash_register_id::bigint END
				      < CAST(CAST(? AS text) AS bigint))))`
}

// RegisterOrdersArgs renvoie les arguments de RegisterOrdersSQL.
func RegisterOrdersArgs(registerID string) []interface{} {
	args := make([]interface{}, registerOrdersPlaceholders)
	for i := range args {
		args[i] = registerID
	}
	return args
}

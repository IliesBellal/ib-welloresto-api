package helpers

import "strings"

// TruncateRunes coupe s à n caractères (pas n octets : les colonnes varchar
// comptent des caractères, et les noms de produits marketplace contiennent des
// emojis), sans couper un caractère en deux. Les blancs de fin sont retirés.
//
// Sert aux textes que les plateformes envoient sans limite de taille (titre de
// groupe d'options, nom de client…) et qui dépassent la colonne qui les reçoit :
// l'insertion échouait et la commande entière n'était pas créée.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimRight(string(runes[:n]), " \t\r\n")
}

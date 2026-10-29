package helpers

import (
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func IntPtrToString(v *int) string {
	if v == nil {
		return "null"
	}
	return strconv.Itoa(*v)
}

func IntToString(v int) string {
	return strconv.Itoa(v)
}

func BoolPtr(b bool) *bool {
	return &b
}

func IntPtr(i int) *int {
	return &i
}

// PasswordMatches compare un mot de passe saisi à son hash bcrypt. Tout autre
// format stocké (vide pour un compte Google seul, ancien format) ne correspond
// jamais : les anciens mots de passe chiffrés de la version PHP ont été retirés
// de la base, et la comparaison en clair qui les accompagnait laissait passer
// un mot de passe vide sur un compte sans mot de passe.
func PasswordMatches(input string, stored string) bool {
	if input == "" || !strings.HasPrefix(stored, "$2") {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(stored), []byte(input)) == nil
}

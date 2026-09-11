package helpers

import (
	"fmt"
	"strconv"
)

// ComputeVATNumber derives a French intracommunity VAT number from a SIREN
// (the first 9 digits of a SIRET) — LOT A Semaine 3, Chantier 12:
// FR + a two-digit key + the SIREN, key = (12 + 3 × (SIREN mod 97)) mod 97.
// Returns ("", false) for a siren that isn't exactly 9 digits — callers
// treat that as "cannot compute a VAT number for this", never a hard error,
// since a malformed SIRET must never block merchant creation.
func ComputeVATNumber(siren string) (string, bool) {
	if len(siren) != 9 {
		return "", false
	}
	n, err := strconv.Atoi(siren)
	if err != nil {
		return "", false
	}
	key := (12 + 3*(n%97)) % 97
	return fmt.Sprintf("FR%02d%s", key, siren), true
}

// ValidateSIRETFormat checks that siret is 14 digits and passes the Luhn
// checksum every real SIRET satisfies. It does not check that the SIRET
// actually exists (not blocking, per LOT A Semaine 2 chantier 6b) — only
// that it is well-formed.
func ValidateSIRETFormat(siret string) bool {
	if len(siret) != 14 {
		return false
	}
	for _, c := range siret {
		if c < '0' || c > '9' {
			return false
		}
	}
	return luhnValid(siret)
}

// luhnValid implements the Luhn checksum (mod 10) used by French SIRET/SIREN
// numbers: doubling every second digit counted from the rightmost one, and
// subtracting 9 from any doubled value over 9.
func luhnValid(digits string) bool {
	sum := 0
	n := len(digits)
	for i := 0; i < n; i++ {
		d := int(digits[i] - '0')
		if (n-1-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

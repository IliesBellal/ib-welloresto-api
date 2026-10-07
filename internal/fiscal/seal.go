package fiscal

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"time"

	"welloresto-api/internal/utils/security"
)

// HashVersion est la valeur écrite dans la colonne hash_version des lignes
// scellées par Seal (migration 168). 1 = formules historiques.
const HashVersion = 2

// GenesisHash est le parent du premier maillon d'une chaîne.
const GenesisHash = "GENESIS_HASH"

// Chain nomme une chaîne ; il entre dans l'empreinte, pour qu'une charge
// utile d'une chaîne ne puisse pas valoir pour une autre.
type Chain string

const (
	ChainOrders        Chain = "orders"
	ChainPayments      Chain = "payments"
	ChainReceipts      Chain = "receipts"
	ChainCashRegisters Chain = "cash_registers"
	ChainAuditLogs     Chain = "audit_logs"
)

// PrevOrGenesis remplace un parent absent par GenesisHash.
func PrevOrGenesis(prev string) string {
	if prev == "" {
		return GenesisHash
	}
	return prev
}

type envelope struct {
	Chain Chain  `json:"chain"`
	V     int    `json:"v"`
	Prev  string `json:"prev"`
	Data  any    `json:"data"`
}

// Seal calcule l'empreinte v2 d'une ligne et sa signature (HMAC de
// FISCAL_SIGNING_KEY) : SHA-256 du JSON de {chain, v, prev, data}. data est
// une des structures *Payload de ce paquet : champs dans un ordre fixe,
// montants entiers, horodatages FormatTime, contenus jsonb passés par
// Canonical. Le même calcul, refait à partir de la ligne relue en base, doit
// redonner la même empreinte.
func Seal(chain Chain, prev string, data any) (hash, signature string, err error) {
	b, err := json.Marshal(envelope{Chain: chain, V: HashVersion, Prev: prev, Data: data})
	if err != nil {
		return "", "", fmt.Errorf("fiscal: marshal %s payload: %w", chain, err)
	}
	sum := sha256.Sum256(b)
	hash = fmt.Sprintf("%x", sum)
	return hash, security.SignHash(hash), nil
}

// Now renvoie l'instant à écrire dans une ligne scellée : UTC, tronqué à la
// microseconde (précision de timestamptz), pour que la valeur hachée soit
// exactement celle que la base restitue.
func Now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// FormatTime est la forme d'un horodatage dans une charge utile.
func FormatTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

// Canonical réécrit un contenu JSON sous une forme qui ne dépend pas de son
// stockage : jsonb réordonne les clés, retire les espaces et réécrit les
// nombres (1e-06 devient 0.000001). Clés triées, nombres en décimal exact
// minimal, aucun espace. Un contenu vide vaut null.
func Canonical(raw []byte) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null"), nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("fiscal: canonical json: %w", err)
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		s, err := canonicalNumber(string(x))
		if err != nil {
			return err
		}
		b.WriteString(s)
	case string:
		s, _ := json.Marshal(x)
		b.Write(s)
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			ks, _ := json.Marshal(k)
			b.Write(ks)
			b.WriteByte(':')
			if err := writeCanonical(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("fiscal: canonical json: unexpected %T", v)
	}
	return nil
}

// canonicalNumber écrit un nombre JSON en décimal exact, sans zéro ni
// exposant superflus. Un littéral décimal a toujours un développement fini.
func canonicalNumber(lit string) (string, error) {
	r, ok := new(big.Rat).SetString(lit)
	if !ok {
		return "", fmt.Errorf("fiscal: canonical json: invalid number %q", lit)
	}
	if r.IsInt() {
		return r.Num().String(), nil
	}
	for prec := 1; prec <= 1100; prec++ {
		s := r.FloatString(prec)
		if back, ok := new(big.Rat).SetString(s); ok && back.Cmp(r) == 0 {
			return s, nil
		}
	}
	return "", fmt.Errorf("fiscal: canonical json: number %q out of range", lit)
}

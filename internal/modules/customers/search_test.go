package customers

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSearchTerm(t *testing.T) {
	type want struct {
		kind          searchTokenKind
		raw           string
		digits        string
		international bool
		national      bool
	}
	tests := []struct {
		name string
		term string
		want []want
	}{
		{"vide", "   ", nil},
		{"une lettre ignorée", "a", nil},
		{"deux chiffres ignorés", "06", nil},
		{"un mot", "dupont", []want{{kind: searchTokenText, raw: "dupont"}}},
		{"deux mots, ordre libre", "jean dupont", []want{
			{kind: searchTokenText, raw: "jean"}, {kind: searchTokenText, raw: "dupont"},
		}},
		{"initiale ignorée, le reste cherche", "jean d", []want{{kind: searchTokenText, raw: "jean"}}},
		{"nom composé reste un seul mot", "jean-pierre", []want{{kind: searchTokenText, raw: "jean-pierre"}}},
		{"accents conservés (normalisés en SQL)", "Hélène", []want{{kind: searchTokenText, raw: "Hélène"}}},
		{"numéro national avec espaces refusionné", "06 12 34 56 78", []want{
			{kind: searchTokenPhone, raw: "0612345678", digits: "0612345678", national: true},
		}},
		{"numéro avec points et tirets", "06.12-34.56.78", []want{
			{kind: searchTokenPhone, raw: "06.12-34.56.78", digits: "0612345678", national: true},
		}},
		{"international avec +", "+33 6 12 34 56 78", []want{
			{kind: searchTokenPhone, raw: "+33612345678", digits: "33612345678", international: true},
		}},
		{"international avec 00", "0033612345678", []want{
			{kind: searchTokenPhone, raw: "0033612345678", digits: "33612345678", international: true},
		}},
		{"derniers chiffres", "5678", []want{{kind: searchTokenPhone, raw: "5678", digits: "5678"}}},
		{"nom puis numéro", "dupont 06 12 34", []want{
			{kind: searchTokenText, raw: "dupont"},
			{kind: searchTokenPhone, raw: "061234", digits: "061234", national: true},
		}},
		{"ponctuation seule ignorée", "- .", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSearchTerm(tc.term)
			var gotView []want
			for _, tok := range got {
				gotView = append(gotView, want{tok.kind, tok.raw, tok.digits, tok.international, tok.national})
			}
			if !reflect.DeepEqual(gotView, tc.want) {
				t.Fatalf("parseSearchTerm(%q) = %+v, want %+v", tc.term, gotView, tc.want)
			}
		})
	}
}

func TestParseSearchTerm_Limits(t *testing.T) {
	got := parseSearchTerm("aa bb cc dd ee ff gg hh")
	if len(got) != searchMaxTokens {
		t.Fatalf("attendu au plus %d mots, got %d", searchMaxTokens, len(got))
	}

	long := strings.Repeat("x", 500)
	got = parseSearchTerm(long)
	if len(got) != 1 || len([]rune(got[0].raw)) != searchMaxTokenRunes {
		t.Fatalf("un mot trop long doit être tronqué à %d caractères, got %+v", searchMaxTokenRunes, got)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("escapeLike = %q", got)
	}
}

// Chaque `?` doit avoir son argument, dans l'ordre : la requête est assemblée
// à la main et le moindre décalage ferait chercher le mauvais mot dans la
// mauvaise colonne sans qu'aucune erreur ne soit levée.
func TestBuildSearchSQL_PlaceholdersMatchArguments(t *testing.T) {
	for _, term := range []string{
		"dupont", "jean dupont", "5678", "06 12 34 56 78", "+33 6 12 34 56 78",
		"0033612345678", "jean 5948", "a1b2", "marie@x.fr", "jean-pierre 0612",
	} {
		tokens := parseSearchTerm(term)
		if len(tokens) == 0 {
			t.Fatalf("%q devrait produire au moins un mot", term)
		}
		s := buildSearchSQL(tokens)
		if got := strings.Count(s.score, "?"); got != len(s.scoreArgs) {
			t.Errorf("%q : score a %d ? pour %d arguments\n%s", term, got, len(s.scoreArgs), s.score)
		}
		if got := strings.Count(s.where, "?"); got != len(s.whereArgs) {
			t.Errorf("%q : where a %d ? pour %d arguments\n%s", term, got, len(s.whereArgs), s.where)
		}
	}
}

func TestBuildSearchSQL_TextToken(t *testing.T) {
	s := buildSearchSQL(parseSearchTerm("Dupont"))

	if !strings.Contains(s.where, "customer_search_name(c.customer_first_name, c.customer_last_name, c.customer_name) LIKE '%' || customer_search_norm(?) || '%'") {
		t.Errorf("le filtre doit passer par l'expression indexée du nom :\n%s", s.where)
	}
	if !strings.Contains(s.where, "lower(c.customer_email) LIKE ?") {
		t.Errorf("un mot de 3+ caractères cherche aussi dans l'email :\n%s", s.where)
	}
	// Mot de 6 caractères : ne peut pas être un code client (varchar(4)).
	if strings.Contains(s.where, "customer_code") {
		t.Errorf("un mot de plus de 4 caractères n'est pas un code :\n%s", s.where)
	}
	// nom (esc) puis email (motif en minuscules)
	if !reflect.DeepEqual(s.whereArgs, []interface{}{"Dupont", "%dupont%"}) {
		t.Errorf("whereArgs = %#v", s.whereArgs)
	}
}

func TestBuildSearchSQL_ShortTextTokenMayBeCode(t *testing.T) {
	s := buildSearchSQL(parseSearchTerm("A12"))
	if !strings.Contains(s.where, "c.customer_code = ?") || !strings.Contains(s.score, "customer_code = ?") {
		t.Errorf("un mot de 4 caractères ou moins peut être un code client :\n%s\n%s", s.where, s.score)
	}
	if strings.Contains(s.where, "lower(c.customer_email)") == false {
		t.Errorf("3 caractères suffisent pour chercher dans l'email :\n%s", s.where)
	}
}

func TestBuildSearchSQL_TwoCharTextTokenSkipsEmail(t *testing.T) {
	s := buildSearchSQL(parseSearchTerm("li"))
	if strings.Contains(s.where, "customer_email") || strings.Contains(s.score, "customer_email") {
		t.Errorf("sous 3 caractères l'email n'est pas cherché :\n%s\n%s", s.where, s.score)
	}
}

func TestBuildSearchSQL_EscapesWildcards(t *testing.T) {
	s := buildSearchSQL(parseSearchTerm("50%_x"))
	if !reflect.DeepEqual(s.whereArgs[:1], []interface{}{`50\%\_x`}) {
		t.Errorf("les jokers LIKE doivent être neutralisés, whereArgs = %#v", s.whereArgs)
	}
}

func TestBuildSearchSQL_PhoneVariants(t *testing.T) {
	// Numéro national complet : préfixe stocké (33…) OU forme locale ancienne (06…).
	s := buildSearchSQL(parseSearchTerm("06 12 34 56 78"))
	if !reflect.DeepEqual(s.whereArgs, []interface{}{"33612345678%", "%0612345678%"}) {
		t.Errorf("national : whereArgs = %#v", s.whereArgs)
	}
	if strings.Contains(s.where, "customer_search_name") {
		t.Errorf("un numéro ne cherche pas dans les noms :\n%s", s.where)
	}

	// Derniers chiffres : jamais préfixés par l'indicatif (c'était le défaut d'origine).
	s = buildSearchSQL(parseSearchTerm("5678"))
	if !reflect.DeepEqual(s.whereArgs, []interface{}{"5678", "%5678%"}) {
		// premier argument : code client (4 chiffres), second : contenu du téléphone
		t.Errorf("fin de numéro : whereArgs = %#v", s.whereArgs)
	}
	for _, a := range s.whereArgs {
		if str, _ := a.(string); strings.Contains(str, "33") {
			t.Errorf("les derniers chiffres ne doivent pas recevoir l'indicatif : %#v", s.whereArgs)
		}
	}

	// International : chiffres tels quels, pas de forme locale.
	s = buildSearchSQL(parseSearchTerm("+33 6 12"))
	if !reflect.DeepEqual(s.whereArgs, []interface{}{"%33612%"}) {
		t.Errorf("international : whereArgs = %#v", s.whereArgs)
	}
}

func TestBuildSearchSQL_AllWordsRequired(t *testing.T) {
	s := buildSearchSQL(parseSearchTerm("jean dupont"))
	if !strings.HasPrefix(s.where, " AND (") || strings.Count(s.where, ") AND (") != 1 {
		t.Errorf("chaque mot doit être exigé (AND) :\n%s", s.where)
	}
	if !strings.Contains(s.score, ") + (") && !strings.Contains(s.score, " + ") {
		t.Errorf("le score est la somme des mots :\n%s", s.score)
	}
}

// Package fiscalverify contrôle l'intégrité des données fiscales d'un
// établissement (conformité caisse, lot E, constat C4 : « détecter et
// démontrer », BOI-TVA-DECLA-30-10-30 §100). Il rejoue chaque chaîne
// (empreinte, signature, chaînage), la numérotation des tickets, les clôtures
// (recalculées depuis les données) et recoupe commandes et tickets.
//
// Les anomalies des lignes antérieures à la version attestée (empreinte v1,
// commandes closes avant le premier ticket v2) sont des avertissements ;
// celles de la version attestée sont des erreurs.
package fiscalverify

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Severity : gravité d'une anomalie.
type Severity string

const (
	// SeverityError : anomalie de la version attestée.
	SeverityError Severity = "ERREUR"
	// SeverityWarning : anomalie antérieure à la version attestée, ou écart
	// connu et tracé (paiement annulé après la clôture de son jour, écart
	// prix / lignes d'un ticket).
	SeverityWarning Severity = "AVERTISSEMENT"
)

// maxDetailsPerCheck borne le détail rapporté par contrôle (le nombre total
// d'anomalies reste compté).
const maxDetailsPerCheck = 20

// Finding est une anomalie.
type Finding struct {
	Severity Severity `json:"gravite"`
	Check    string   `json:"controle"`
	Message  string   `json:"message"`
}

// CheckSummary résume un contrôle.
type CheckSummary struct {
	Check    string `json:"controle"`
	Label    string `json:"libelle"`
	Checked  int    `json:"elements_controles"`
	Errors   int    `json:"erreurs"`
	Warnings int    `json:"avertissements"`
}

// Report est le rapport de vérification.
type Report struct {
	Software      string          `json:"logiciel"`
	Version       string          `json:"version"`
	MerchantID    string          `json:"etablissement"`
	MerchantName  string          `json:"raison_sociale"`
	Siret         string          `json:"siret"`
	Timezone      string          `json:"fuseau"`
	From          string          `json:"du"` // jour local inclus, vide = depuis le début
	To            string          `json:"au"` // jour local inclus
	AttestedSince *time.Time      `json:"version_attestee_depuis"`
	GeneratedAt   time.Time       `json:"genere_le"`
	Duration      string          `json:"duree"`
	Checks        []*CheckSummary `json:"controles"`
	Findings      []Finding       `json:"anomalies"`
	Errors        int             `json:"erreurs"`
	Warnings      int             `json:"avertissements"`

	byCheck map[string]*CheckSummary
	details map[string]int
}

// OK : aucune erreur (les avertissements n'empêchent pas la conformité).
func (r *Report) OK() bool { return r.Errors == 0 }

func (r *Report) check(name, label string) *CheckSummary {
	if r.byCheck == nil {
		r.byCheck, r.details = map[string]*CheckSummary{}, map[string]int{}
	}
	c := r.byCheck[name]
	if c == nil {
		c = &CheckSummary{Check: name, Label: label}
		r.byCheck[name] = c
		r.Checks = append(r.Checks, c)
	}
	return c
}

func (r *Report) add(sev Severity, check, format string, args ...any) {
	c := r.byCheck[check]
	if sev == SeverityError {
		r.Errors++
		c.Errors++
	} else {
		r.Warnings++
		c.Warnings++
	}
	r.details[check]++
	if r.details[check] <= maxDetailsPerCheck {
		r.Findings = append(r.Findings, Finding{Severity: sev, Check: check, Message: fmt.Sprintf(format, args...)})
	}
}

// severity : erreur pour la version attestée, avertissement avant.
func severity(attested bool) Severity {
	if attested {
		return SeverityError
	}
	return SeverityWarning
}

// WriteText écrit le rapport en clair, en français.
func (r *Report) WriteText(w io.Writer) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	p("RAPPORT DE VÉRIFICATION D'INTÉGRITÉ — %s %s", r.Software, r.Version)
	p("Établissement : %s (%s), SIRET %s, fuseau %s", r.MerchantName, r.MerchantID, r.Siret, r.Timezone)
	from := r.From
	if from == "" {
		from = "début"
	}
	p("Période : du %s au %s (jours locaux inclus)", from, r.To)
	if r.AttestedSince != nil {
		p("Version attestée depuis : %s (premier ticket à empreinte v2)", r.AttestedSince.UTC().Format(time.RFC3339))
	} else {
		p("Version attestée : aucun ticket à empreinte v2 pour cet établissement")
	}
	p("Généré le %s en %s", r.GeneratedAt.UTC().Format(time.RFC3339), r.Duration)
	p("")
	p("%-22s %10s %8s %15s  %s", "Contrôle", "Contrôlés", "Erreurs", "Avertissements", "Libellé")
	for _, c := range r.Checks {
		p("%-22s %10d %8d %15d  %s", c.Check, c.Checked, c.Errors, c.Warnings, c.Label)
	}
	p("")
	if r.OK() {
		p("RÉSULTAT : CONFORME — aucune erreur (%d avertissement(s)).", r.Warnings)
	} else {
		p("RÉSULTAT : NON CONFORME — %d erreur(s), %d avertissement(s).", r.Errors, r.Warnings)
	}
	if len(r.Findings) == 0 {
		return
	}
	p("")
	p("Anomalies (%d premières par contrôle) :", maxDetailsPerCheck)
	for _, f := range r.Findings {
		p("  [%s] %s : %s", f.Severity, f.Check, strings.TrimSpace(f.Message))
	}
}

package payouts

import (
	"strings"

	"welloresto-api/internal/infrastructure/mailer"
)

// Tout est volontairement en dur (pas de variables d'environnement) : voir
// docs/payouts-justificatifs.md, section « Passage en production ».
//
// Rien de ce qui n'est pas (encore) défini ici ne bloque l'envoi des
// justificatifs : une mention vide est simplement omise du document.

const (
	// documentsTestMode : tant qu'il vaut true, les justificatifs ne partent PAS
	// chez les restaurateurs mais à testRecipient, et les factures portent la
	// série d'essai (TEST-…), qui ne consomme pas la numérotation légale. Le
	// passage en réel (false) est décrit dans docs/payouts-justificatifs.md.
	documentsTestMode = true

	// testRecipient reçoit tous les justificatifs en phase de test, et ceux
	// d'un restaurateur sans adresse e-mail une fois en réel.
	testRecipient = "iliesbellal@gmail.com"

	// vatRatePercent : TVA appliquée à la commission de la plateforme.
	vatRatePercent = 20

	// seriesLive / seriesTest : préfixes de numérotation des factures. Une
	// facture d'essai ne doit jamais prendre un numéro de la série légale : un
	// numéro émis puis jamais livré serait un trou dans la suite.
	seriesLive = "WR"
	seriesTest = "TEST"

	// reconcileRetries : passages horaires pendant lesquels un payout que
	// Stripe n'a pas encore rapproché est retenté avant d'être envoyé tel
	// quel, avec son écart signalé sur le relevé.
	reconcileRetries = 3
)

// issuerIdentity est l'émetteur de la facture de commission : Wello Resto,
// exploité par BINYA SAS (même société que l'éditeur de l'attestation de
// conformité, internal/config/attestation.go).
type issuerIdentity struct {
	TradeName string
	Name      string // raison sociale
	Capital   string
	Address   string
	SIREN     string
	RCS       string // vide tant qu'il n'est pas connu : la ligne est omise
	VATNumber string // idem
	Email     string
}

var issuer = issuerIdentity{
	TradeName: "Wello Resto",
	Name:      "BINYA SAS",
	Capital:   "1 000 €",
	Address:   "13 rue Principale, 57450 Theding",
	SIREN:     "103020558",
	RCS:       "",
	VATNumber: "",
	Email:     mailer.InvoiceEmail,
}

// lines liste les mentions de l'émetteur, sans les lignes non renseignées.
func (i issuerIdentity) lines() []string {
	out := []string{i.TradeName + " (" + i.Name + ")"}
	for _, kv := range [][2]string{
		{"Capital social", i.Capital},
		{"Adresse", i.Address},
		{"SIREN", i.SIREN},
		{"RCS", i.RCS},
		{"N° TVA intracommunautaire", i.VATNumber},
		{"E-mail", i.Email},
	} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			out = append(out, kv[0]+" : "+v)
		}
	}
	return out
}

// invoiceSeries renvoie la série de numérotation en vigueur.
func invoiceSeries() string {
	if documentsTestMode {
		return seriesTest
	}
	return seriesLive
}

// recipientFor renvoie l'adresse qui reçoit les justificatifs d'un marchand, et
// une note à afficher dans le mail quand ce n'est pas lui qui les reçoit.
func recipientFor(m *Merchant) (to, notice string) {
	switch {
	case documentsTestMode:
		return testRecipient, "Phase de test : ce mail est destiné à " + m.Name + " (" + m.Email + ") et vous est adressé à sa place."
	case strings.TrimSpace(m.Email) == "":
		return testRecipient, "Aucune adresse e-mail n'est renseignée pour " + m.Name + " : ses justificatifs vous sont adressés."
	}
	return m.Email, ""
}

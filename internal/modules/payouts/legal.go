package payouts

import (
	"strings"

	"welloresto-api/internal/docsmode"
	"welloresto-api/internal/infrastructure/mailer"
)

// Tout est volontairement en dur (pas de variables d'environnement) : voir
// docs/payouts-justificatifs.md, section « Passage en production ».
//
// Rien de ce qui n'est pas (encore) défini ici ne bloque l'envoi des
// justificatifs : une mention vide est simplement omise du document.

const (
	// statementsTestMode : tant qu'il vaut true, les relevés de versement ne
	// partent PAS chez les restaurateurs mais à testRecipient, avec un bandeau
	// « SPÉCIMEN ». Ils sont en réel depuis le 2026-10-20.
	statementsTestMode = docsmode.StatementsTestMode

	// invoicesTestMode : même chose pour les factures de commission, qui
	// portent en plus la série d'essai (TEST-…), qui ne consomme pas la
	// numérotation légale. Le passage en réel (false) est décrit dans
	// docs/payouts-justificatifs.md. L'interrupteur est partagé avec les
	// factures d'abonnement : internal/docsmode.
	invoicesTestMode = docsmode.TestMode

	// testRecipient reçoit tous les documents en phase de test, et ceux d'un
	// restaurateur sans adresse e-mail une fois en réel.
	testRecipient = docsmode.TestRecipient

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
	if invoicesTestMode {
		return seriesTest
	}
	return seriesLive
}

// recipientFor renvoie l'adresse qui reçoit un document d'un marchand (son
// responsable, voir docsmode.Recipient), et une note à afficher dans le mail
// quand ce n'est pas lui qui le reçoit.
func recipientFor(m *Merchant, testMode bool) (to, notice string) {
	return docsmode.Recipient(testMode, m.Name, m.OwnerEmail, m.Email)
}

// establishment identifie le marchand dans les mails (nom, adresse,
// merchant_id) : une même adresse peut recevoir ceux de plusieurs établissements.
func (m *Merchant) establishment() mailer.Establishment {
	return mailer.Establishment{ID: m.ID, Name: m.Name, Address: m.Address}
}

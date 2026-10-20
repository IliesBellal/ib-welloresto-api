// Package docsmode porte les réglages communs des documents financiers envoyés
// aux restaurateurs (relevés de versement, factures de commission et
// d'abonnement) : les interrupteurs de la phase de test, la règle du
// destinataire et l'adresse du back-office pour les liens.
package docsmode

import "strings"

const (
	// TestMode : tant qu'il vaut true, les factures (commission et abonnement)
	// ne partent PAS chez les restaurateurs mais à TestRecipient. Passer à false
	// pour l'envoi réel (voir docs/payouts-justificatifs.md, « Passage en
	// production »).
	TestMode = true

	// StatementsTestMode : le même interrupteur pour les relevés de versement,
	// passés en réel le 2026-10-20 sans attendre les factures (l'émetteur de la
	// facture de commission n'est pas encore complet).
	StatementsTestMode = false

	// TestRecipient reçoit tous les documents en phase de test, et ceux d'un
	// restaurateur sans adresse e-mail une fois en réel.
	TestRecipient = "iliesbellal@gmail.com"

	// BackOfficeURL est l'adresse du back-office, pour les liens des mails.
	BackOfficeURL = "https://my.welloresto.fr"
	// TabVirementsPath / TabFacturesPath mènent aux onglets de la page
	// « Abonnement & facturation ».
	TabVirementsPath = "/settings/subscription?tab=virements"
	TabFacturesPath  = "/settings/subscription?tab=factures"
)

// Recipient renvoie l'adresse qui reçoit un document d'un établissement, et une
// note à afficher dans le mail quand ce n'est pas lui qui le reçoit.
//
// Le destinataire est le responsable de l'établissement, c'est-à-dire son
// premier admin actif (ownerEmail), à défaut l'adresse de l'établissement
// (merchantEmail), à défaut TestRecipient : la règle des rappels de fin d'essai
// et des relances d'impayés (subscriptions.GetMerchantOwnerContact). Un compte
// propriétaire d'établissement, à créer, primera sur cette règle : voir
// docs/payouts-justificatifs.md, « Destinataire ».
func Recipient(testMode bool, merchantName, ownerEmail, merchantEmail string) (to, notice string) {
	to = strings.TrimSpace(ownerEmail)
	if to == "" {
		to = strings.TrimSpace(merchantEmail)
	}
	switch {
	case testMode:
		return TestRecipient, "Phase de test : ce mail est destiné à " + merchantName + " (" + to + ") et vous est adressé à sa place."
	case to == "":
		return TestRecipient, "Aucune adresse e-mail n'est connue pour " + merchantName + " : ce mail vous est adressé à sa place."
	}
	return to, ""
}

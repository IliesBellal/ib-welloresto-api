// Package docsmode porte les réglages communs des documents financiers envoyés
// aux restaurateurs (justificatifs de versement, factures de commission et
// d'abonnement) : l'interrupteur de la phase de test, en un seul endroit à
// basculer pour passer en réel, et l'adresse du back-office pour les liens.
package docsmode

const (
	// TestMode : tant qu'il vaut true, ces documents ne partent PAS chez les
	// restaurateurs mais à TestRecipient. Passer à false pour l'envoi réel
	// (voir docs/payouts-justificatifs.md, « Passage en production »).
	TestMode = true

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

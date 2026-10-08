package attestations

import (
	"bytes"
	"fmt"

	"github.com/jung-kurt/gofpdf"
)

// Texte du modèle BOI-LETTRE-000242 (version du 25/03/2026), repris mot pour
// mot ; seules les zones à compléter (NOM Prénom, RAISON SOCIALE, DATE,
// références…) sont remplacées, et les mentions « À adapter selon le cas »
// tranchées au plus juste (décision d'Ilies, F4) : « les fonctionnalités de
// caisse de ce logiciel/système », « satisfont ». La mention facultative
// (3), racine majeure et subdivisions, est retenue : elle permet la tolérance
// du III-B § 380.
const (
	title       = "Attestation individuelle relative à l'utilisation d'un logiciel ou d'un système de caisse sécurisé"
	modelRef    = "Modèle BOI-LETTRE-000242 — article 286, I, 3° bis du code général des impôts"
	preamble    = "Les volets 1 et 2 de cette attestation doivent être présentés à l'administration fiscale en cas de contrôle. Elle n'a de valeur que si son volet 2 est dûment complété et signé par l'entreprise utilisatrice du logiciel / système."
	volet1Title = "I. Volet 1 : Partie à remplir par l'éditeur ou intégrateur du logiciel ou du système de caisse"
	volet2Title = "II. Volet 2 : Partie à remplir par l'entreprise qui utilise le logiciel ou le système de caisse"
	remark1     = "Remarque : Il est rappelé que l'établissement d'une fausse attestation est un délit pénal passible de trois ans d'emprisonnement et de 45 000 € d'amende (code pénal [C. pén.], art. 441-1). L'usage d'une fausse attestation est passible des mêmes peines."
	remark2     = "Remarque : Il est rappelé que l'établissement d'une fausse attestation est un délit pénal passible de trois ans d'emprisonnement et de 45 000 € d'amende (C. pén., art. 441-1). L'usage d'une fausse attestation est passible des mêmes peines."
)

func volet1Body(d Document) string {
	return fmt.Sprintf("Je soussigné, %s, représentant légal de la société %s, éditeur du logiciel / système de caisse %s (%s), "+
		"atteste que les fonctionnalités de caisse de ce logiciel/système, mis sur le marché à compter du %s, dans sa version n° %s, "+
		"sous le numéro de licence %s, satisfont aux conditions d'inaltérabilité, de sécurisation, de conservation et d'archivage des données en vue du contrôle "+
		"de l'administration fiscale, prévues au 3° bis du I de l'article 286 du code général des impôts.",
		d.EditorRepresentative, d.EditorCompany, d.Software, d.SoftwareDescription, d.ReleaseDate, d.Version, d.Licence)
}

func volet1Root(d Document) string {
	return fmt.Sprintf("J'atteste que la dernière version majeure de ce logiciel ou système est identifiée avec la racine suivante : %s "+
		"et que les versions mineures développées ultérieurement à cette version majeure sont ou seront identifiées par les subdivisions "+
		"suivantes de cette racine : %s. Je m'engage à ce que ces subdivisions ne soient utilisées par %s que pour l'identification des "+
		"versions mineures ultérieures, à l'exclusion de toute version majeure. Les versions majeures et mineures du logiciel ou système "+
		"s'entendent au sens du III-A § 340 du BOI-TVA-DECLA-30-10-30.",
		d.MajorRoot, d.MinorPattern, d.EditorCompany)
}

func volet2Body(d Document) []string {
	return []string{
		fmt.Sprintf("Je soussigné, %s, représentant légal de la société %s, certifie avoir acquis ou téléchargé le %s, auprès de %s, "+
			"le logiciel / système de caisse mentionné au volet 1 de cette attestation.",
			d.SignerName, d.CompanyName, d.AcquisitionDate, d.EditorCompany),
		fmt.Sprintf("J'atteste utiliser ce logiciel / système de caisse pour enregistrer les règlements de mes clients particuliers, "+
			"conformément à la réglementation fiscale en vigueur, depuis le %s.", d.UsageStartDate),
	}
}

// RenderPDF produit le PDF de l'attestation. signaturePNG : image de la
// signature du représentant légal de l'éditeur, apposée sur le volet 1.
func RenderPDF(d Document, signaturePNG []byte) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 18, 20)
	pdf.SetAutoPageBreak(true, 18)
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Arial", "", 7)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(0, 4, tr(fmt.Sprintf("Attestation %s — %s %s — page %d/{nb}", d.Reference, d.Software, d.Version, pdf.PageNo())),
			"", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})
	pdf.AliasNbPages("")
	pdf.AddPage()

	text := func(size float64, style, s string, h float64) {
		pdf.SetFont("Arial", style, size)
		pdf.MultiCell(0, h, tr(s), "", "J", false)
	}
	gap := func(h float64) { pdf.Ln(h) }

	pdf.SetFont("Arial", "B", 13)
	pdf.MultiCell(0, 6, tr(title), "", "C", false)
	pdf.SetFont("Arial", "", 8)
	pdf.MultiCell(0, 4, tr(modelRef), "", "C", false)
	gap(3)
	text(9, "I", preamble, 4.5)
	gap(2)
	text(9, "", fmt.Sprintf("Établissement : %s — SIRET %s — %s", d.CompanyName, d.Siret, d.Address), 4.5)

	// Volet 1.
	gap(4)
	text(11, "B", volet1Title, 5.5)
	gap(1.5)
	text(10, "", volet1Body(d), 5)
	gap(2)
	text(10, "", volet1Root(d), 5)
	gap(2)
	text(10, "", "Le périmètre couvert par cette attestation concerne les fonctionnalités suivantes :", 5)
	for _, f := range d.Covered {
		text(9.5, "", "– "+f, 4.6)
	}
	gap(1.5)
	text(10, "", "Les fonctionnalités suivantes ne sont pas couvertes par cette attestation :", 5)
	for _, f := range d.NotCovered {
		text(9.5, "", "– "+f, 4.6)
	}
	gap(2)
	text(10, "", fmt.Sprintf("Fait à %s, le %s,", d.EditorCity, d.EditorSignedOn), 5)
	text(10, "", "Signature du représentant légal de l'éditeur du logiciel ou système de caisse :", 5)
	if len(signaturePNG) > 0 {
		opt := gofpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}
		pdf.RegisterImageOptionsReader("editor_signature", opt, bytes.NewReader(signaturePNG))
		if err := pdf.Error(); err != nil {
			return nil, fmt.Errorf("attestations: signature image: %w", err)
		}
		y := pdf.GetY() + 1
		pdf.ImageOptions("editor_signature", 22, y, 0, 18, false, opt, 0, "")
		pdf.SetY(y + 19)
	} else {
		gap(18)
	}
	text(9, "", d.EditorRepresentative+", représentant légal de "+d.EditorCompany, 4.5)
	gap(1.5)
	text(8, "I", remark1, 4)

	// Volet 2, sur sa propre page : le restaurateur le lit et le signe à part.
	pdf.AddPage()
	text(11, "B", volet2Title, 5.5)
	gap(1.5)
	for _, p := range volet2Body(d) {
		text(10, "", p, 5)
		gap(1.5)
	}
	text(10, "", fmt.Sprintf("Fait à %s, le %s,", d.City, d.SignedOn), 5)
	text(10, "", "Signature du représentant légal :", 5)
	pdf.SetFillColor(244, 244, 244)
	pdf.SetFont("Arial", "", 9)
	pdf.MultiCell(0, 4.6, tr(fmt.Sprintf("Signé électroniquement par %s le %s, depuis le compte %s, après avoir certifié l'exactitude "+
		"des informations du volet 2. %s", d.SignerName, d.SignedAt, d.SignerAccount, d.IntegrityCheck)), "1", "L", true)
	gap(1.5)
	text(8, "I", remark2, 4)

	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("attestations: pdf: %w", err)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("attestations: pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

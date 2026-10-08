package attestations

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func sampleDocument() Document {
	return Document{
		Reference: "ATT-42-20261008120000", EditorRepresentative: "DUPONT Jean", EditorCompany: "WelloResto SAS",
		EditorCity: "Paris", EditorSignedOn: "08/10/2026", Software: "WelloResto", SoftwareDescription: SoftwareDescription,
		Version: "2.0.0", MajorRoot: "2", MinorPattern: "2.x.y", ReleaseDate: "01/11/2026", Licence: "WR-42",
		Covered: Covered, NotCovered: NotCovered, MerchantID: "42", CompanyName: "Pizzeria Test SARL", Siret: "12345678900011",
		Address: "1 rue de la Paix, 75001 Paris", City: "Paris", SignerName: "MARTIN Paul", AcquisitionDate: "01/03/2026",
		UsageStartDate: "15/03/2026", SignedOn: "08/10/2026", SignedAt: "08/10/2026 à 12:00 (Europe/Paris)",
		SignerAccount: "paul@example.fr", IntegrityCheck: "Contrôle d'intégrité préalable : conforme.",
	}
}

// Le texte reprend le modèle BOI-LETTRE-000242 mot pour mot, zones à
// compléter remplacées.
func TestDocumentTextFollowsOfficialModel(t *testing.T) {
	d := sampleDocument()
	v1 := volet1Body(d)
	for _, want := range []string{
		"Je soussigné, DUPONT Jean, représentant légal de la société WelloResto SAS, éditeur du logiciel / système de caisse WelloResto",
		"atteste que les fonctionnalités de caisse de ce logiciel/système, mis sur le marché à compter du 01/11/2026, dans sa version n° 2.0.0, sous le numéro de licence WR-42,",
		"satisfont aux conditions d'inaltérabilité, de sécurisation, de conservation et d'archivage des données en vue du contrôle de l'administration fiscale, prévues au 3° bis du I de l'article 286 du code général des impôts.",
	} {
		if !strings.Contains(v1, want) {
			t.Fatalf("volet 1 missing %q in:\n%s", want, v1)
		}
	}
	root := volet1Root(d)
	for _, want := range []string{
		"identifiée avec la racine suivante : 2 et que les versions mineures",
		"subdivisions suivantes de cette racine : 2.x.y. Je m'engage à ce que ces subdivisions ne soient utilisées par WelloResto SAS",
		"au sens du III-A § 340 du BOI-TVA-DECLA-30-10-30.",
	} {
		if !strings.Contains(root, want) {
			t.Fatalf("root mention missing %q", want)
		}
	}
	v2 := strings.Join(volet2Body(d), "\n")
	for _, want := range []string{
		"Je soussigné, MARTIN Paul, représentant légal de la société Pizzeria Test SARL, certifie avoir acquis ou téléchargé le 01/03/2026, auprès de WelloResto SAS, le logiciel / système de caisse mentionné au volet 1 de cette attestation.",
		"J'atteste utiliser ce logiciel / système de caisse pour enregistrer les règlements de mes clients particuliers, conformément à la réglementation fiscale en vigueur, depuis le 15/03/2026.",
	} {
		if !strings.Contains(v2, want) {
			t.Fatalf("volet 2 missing %q in:\n%s", want, v2)
		}
	}
}

func TestEmbeddedEditorSignature(t *testing.T) {
	if _, err := png.Decode(bytes.NewReader(editorSignature)); err != nil {
		t.Fatalf("embedded signature is not a PNG: %v", err)
	}
	if _, err := RenderPDF(sampleDocument(), editorSignature); err != nil {
		t.Fatal(err)
	}
}

func TestRenderPDF(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 40))
	for x := 10; x < 110; x++ {
		img.Set(x, 20+(x%7), color.Black)
	}
	var sig bytes.Buffer
	if err := png.Encode(&sig, img); err != nil {
		t.Fatal(err)
	}
	for name, signature := range map[string][]byte{"with signature": sig.Bytes(), "without signature": nil} {
		out, err := RenderPDF(sampleDocument(), signature)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF-")) || len(out) < 2000 {
			t.Fatalf("%s: not a PDF (%d bytes)", name, len(out))
		}
	}
	if _, err := RenderPDF(sampleDocument(), []byte("not a png")); err == nil {
		t.Fatal("an unreadable signature image must fail")
	}
}

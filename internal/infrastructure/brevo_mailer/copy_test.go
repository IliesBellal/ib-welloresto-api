package brevo_mailer

import "testing"

func TestWithCopy(t *testing.T) {
	t.Run("copie cachée ajoutée", func(t *testing.T) {
		p := withCopy(map[string]interface{}{}, "client@example.com")
		bcc, ok := p["bcc"].([]map[string]string)
		if !ok || len(bcc) != 1 || bcc[0]["email"] != copyBCC {
			t.Fatalf("bcc = %v, attendu %s", p["bcc"], copyBCC)
		}
	})

	t.Run("pas de doublon si le destinataire est la copie", func(t *testing.T) {
		p := withCopy(map[string]interface{}{}, " IliesBellal@gmail.com ")
		if _, ok := p["bcc"]; ok {
			t.Fatalf("bcc inattendu : %v", p["bcc"])
		}
	})
}

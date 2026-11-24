package r2

import (
	"context"
	"testing"
)

func TestGetKeyFromURL(t *testing.T) {
	c := &Client{publicBaseURL: "https://storage.welloresto.fr/"}
	const key = "wello_resto_images_storage/merchants/212/scannorder/banner.jpg"

	cases := map[string]string{
		"https://storage.welloresto.fr/" + key:                                          key,
		"https://storage.welloresto.fr/" + key + "?v=1795136490475911409":              key,
		"https://pub-528ad8f1b1df4b8f9744fedd19777689.r2.dev/" + key:                    key,
		"https://pub-528ad8f1b1df4b8f9744fedd19777689.r2.dev/" + key + "?v=1795136490": key,
		"https://autre-domaine.fr/" + key:                                               "",
		"https://evil.com/x.r2.dev/" + key:                                              "",
		"http://pub-abc.r2.dev/" + key:                                                  "",
		"":                                                                              "",
	}
	for in, want := range cases {
		if got := c.GetKeyFromURL(in); got != want {
			t.Errorf("GetKeyFromURL(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestGetKeyFromURL_NoPublicBaseURL(t *testing.T) {
	c := &Client{}
	if got := c.GetKeyFromURL("/wello_resto_images_storage/a.jpg"); got != "" {
		t.Errorf("sans base publique, une URL relative ne doit pas donner de clé, obtenu %q", got)
	}
}

func TestImageBankKey(t *testing.T) {
	key := GenerateImageBankKey("boissons-soft", "canette-cola-33cl-1a2b3c4d", "png")
	if key != "wello_resto_images_storage/image_bank/boissons-soft/canette-cola-33cl-1a2b3c4d.png" {
		t.Fatalf("clé inattendue : %s", key)
	}
	if !IsImageBankKey(key) {
		t.Fatal("clé de banque non reconnue")
	}
	if IsImageBankKey("wello_resto_images_storage/merchants/2/products/69.png") {
		t.Fatal("une clé d'établissement n'est pas une clé de banque")
	}
}

func TestDeleteFile_NeverDeletesImageBank(t *testing.T) {
	// Client sans S3 : un appel réel paniquerait. La garde doit répondre avant.
	c := &Client{}
	if err := c.DeleteFile(context.Background(), ImageBankPrefix+"burgers/x.jpg"); err != nil {
		t.Fatalf("erreur inattendue : %v", err)
	}
}

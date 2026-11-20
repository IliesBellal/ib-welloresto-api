package r2

import "testing"

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

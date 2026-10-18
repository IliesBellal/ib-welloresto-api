package auth

import "testing"

func TestUpdateAvailableResult(t *testing.T) {
	withChecksum := updateAvailableResult(140, "https://x/app.apk", "abc123")
	if withChecksum["status"] != "update_available" ||
		withChecksum["download_url"] != "https://x/app.apk" ||
		withChecksum["version_code"] != 140 ||
		withChecksum["checksum_sha256"] != "abc123" {
		t.Errorf("unexpected result: %+v", withChecksum)
	}

	// Empreinte non renseignée : la clé est omise (et non vide) pour que
	// l'appli refuse l'installation automatique.
	without := updateAvailableResult(140, "https://x/app.apk", "")
	if _, ok := without["checksum_sha256"]; ok {
		t.Errorf("checksum_sha256 must be omitted when empty: %+v", without)
	}
}

package helpers

import (
	"bytes"
	"crypto/aes"
	"strconv"
)

func IntPtrToString(v *int) string {
	if v == nil {
		return "null"
	}
	return strconv.Itoa(*v)
}

func IntToString(v int) string {
	return strconv.Itoa(v)
}

func EncryptPHP(password string) (string, error) {
	// CORRECTION 1 : Utiliser la clé directement si elle fait 16 chars (AES-128)
	// Si votre clé PHP est vraiment du base64, gardez le DecodeString,
	// mais assurez-vous que le résultat décodé fasse 16, 24 ou 32 octets.
	key := []byte("oBo9mPqMfJ2Ni4Ma")

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// CORRECTION 2 : Appliquer le Padding
	data := []byte(password)
	data = pkcs7Padding(data, block.BlockSize())

	// CORRECTION 3 : Implémentation manuelle du mode ECB
	encrypted := make([]byte, len(data))
	blockSize := block.BlockSize()

	// On boucle sur chaque bloc pour le chiffrer individuellement (C'est ça, l'ECB)
	for bs, be := 0, blockSize; bs < len(data); bs, be = bs+blockSize, be+blockSize {
		block.Encrypt(encrypted[bs:be], data[bs:be])
	}

	// Retourner en hex ou base64 selon ce que votre BDD attend.
	// Souvent PHP retourne du binaire brut que l'on encode ensuite.
	// Ici, le code original retournait string(out), ce qui risque de casser en JSON.
	// Il est plus sûr de retourner du base64 ici aussi :
	// return base64.StdEncoding.EncodeToString(encrypted), nil

	return string(encrypted), nil
}

// Fonction utilitaire pour ajouter le padding (PKCS#7)
func pkcs7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

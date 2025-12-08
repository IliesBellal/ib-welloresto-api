// notification/token_manager.go

package notification

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Interface utilisée dans le NotificationService
type FCMTokenManager interface {
	GenerateToken(ctx context.Context) (string, error)
}

type GoogleFCMTokenManager struct {
	ServiceAccountPath string
	HTTP               *http.Client
}

func NewGoogleFCMTokenManager(serviceAccountPath string) *GoogleFCMTokenManager {
	return &GoogleFCMTokenManager{
		ServiceAccountPath: serviceAccountPath,
		HTTP:               &http.Client{},
	}
}

/*
STRUCTURE DU FICHIER JSON :
{
  "type": "service_account",
  "project_id": "wello-resto-150721",
  "private_key_id": "...",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "xxxxx@xxxxx.iam.gserviceaccount.com",
  "client_id": "...",
  ...
}
*/

type ServiceAccountInfo struct {
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
}

//
// Fonctions principales
//

func (m *GoogleFCMTokenManager) GenerateToken(ctx context.Context) (string, error) {

	info, err := m.readServiceAccount()
	if err != nil {
		return "", err
	}

	jwt, err := m.buildJWT(info)
	if err != nil {
		return "", fmt.Errorf("jwt generation failed: %w", err)
	}

	token, err := m.requestAccessToken(jwt)
	if err != nil {
		return "", fmt.Errorf("oauth request failed: %w", err)
	}

	m.log("New FCM token generated successfully")

	return token, nil
}

//
// Lecture du fichier JSON
//

func (m *GoogleFCMTokenManager) readServiceAccount() (*ServiceAccountInfo, error) {

	data, err := os.ReadFile(m.ServiceAccountPath)
	if err != nil {
		m.log("Error: cannot read service account file")
		return nil, err
	}

	var info ServiceAccountInfo

	if err := json.Unmarshal(data, &info); err != nil {
		m.log("Error: invalid JSON service account")
		return nil, err
	}

	if info.PrivateKey == "" || info.ClientEmail == "" {
		return nil, errors.New("invalid service account: missing private_key or client_email")
	}

	return &info, nil
}

//
// Construction du JWT (équivalent generateJWT)
//

func (m *GoogleFCMTokenManager) buildJWT(info *ServiceAccountInfo) (string, error) {

	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}

	claims := map[string]interface{}{
		"iss":   info.ClientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   "https://oauth2.googleapis.com/token",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	}

	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	h := base64urlEncode(headerJSON)
	c := base64urlEncode(claimsJSON)

	unsigned := h + "." + c

	signed, err := m.sign(unsigned, info.PrivateKey)
	if err != nil {
		return "", err
	}

	return unsigned + "." + signed, nil
}

//
// Signature RSA SHA256
//

func (m *GoogleFCMTokenManager) sign(unsigned string, privateKey string) (string, error) {

	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return "", errors.New("invalid private key PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("private key is not RSA")
	}

	hasher := sha256.New()
	hasher.Write([]byte(unsigned))

	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, cryptoSHA256, hasher.Sum(nil))
	if err != nil {
		return "", err
	}

	return base64urlEncode(sig), nil
}

const cryptoSHA256 = crypto.SHA256

//
// Requête HTTP vers Google OAuth (équivalent fetchAccessToken)
//

func (m *GoogleFCMTokenManager) requestAccessToken(jwt string) (string, error) {

	form := "grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=" + jwt

	req, _ := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	var parsed struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}

	_ = json.Unmarshal(raw, &parsed)

	if parsed.AccessToken != "" {
		return parsed.AccessToken, nil
	}

	if parsed.Error != "" {
		return "", errors.New(parsed.Error)
	}

	return "", errors.New("unknown error while requesting access token")
}

//
// Utilitaires
//

func base64urlEncode(data []byte) string {
	s := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(
		base64Encode(data), "+", "-"), "/", "_"), "=")
	return s
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func (m *GoogleFCMTokenManager) log(msg string) {
	fmt.Printf("[FCMTokenManager] %s\n", msg)
}

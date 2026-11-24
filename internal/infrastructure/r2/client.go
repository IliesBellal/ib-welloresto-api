package r2

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Client struct {
	s3Client      *s3.Client
	presignClient *s3.PresignClient
	bucket        string
	publicBaseURL string
}

type UploadConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string
	Bucket          string
	PublicBaseURL   string
}

// NewClient crée un nouveau client R2
func NewClient(cfg UploadConfig) (*Client, error) {
	// Configurer le client S3 pour R2
	r2Resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               cfg.Endpoint,
			SigningRegion:     "auto",
			HostnameImmutable: true,
		}, nil
	})

	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithEndpointResolverWithOptions(r2Resolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg)

	return &Client{
		s3Client:      s3Client,
		presignClient: s3.NewPresignClient(s3Client),
		bucket:        cfg.Bucket,
		publicBaseURL: cfg.PublicBaseURL,
	}, nil
}

// UploadFile uploade un fichier vers R2
func (c *Client) UploadFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error) {
	// Upload vers R2
	_, err := c.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(contentType),
		ACL:         types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to R2: %w", err)
	}

	// Construire l'URL publique
	publicURL := strings.TrimRight(c.publicBaseURL, "/") + "/" + key

	return publicURL, nil
}

// UploadPrivateFile uploade un fichier vers un bucket privé R2.
func (c *Client) UploadPrivateFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error) {
	_, err := c.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload private file to R2: %w", err)
	}

	return c.GenerateSignedURL(ctx, key, time.Hour)
}

// GenerateSignedURL retourne une URL signée de lecture pour un objet privé.
func (c *Client) GenerateSignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if c.presignClient == nil {
		return "", fmt.Errorf("presign client not initialized")
	}
	resp, err := c.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("failed to presign R2 url: %w", err)
	}
	return resp.URL, nil
}

// GetFile lit le contenu d'un objet du bucket (public ou privé selon le client).
func (c *Client) GetFile(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read from R2: %w", err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DeleteFile supprime un fichier de R2.
//
// Les images de la banque (préfixe ImageBankPrefix) ne sont jamais supprimées :
// plusieurs établissements référencent la même URL, et tous les circuits qui
// effacent « l'ancienne image » d'un produit, d'une option, d'une bannière…
// passent par ici. Voir docs/banque-images.md.
func (c *Client) DeleteFile(ctx context.Context, key string) error {
	if IsImageBankKey(key) {
		return nil
	}
	_, err := c.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from R2: %w", err)
	}

	return nil
}

// CopyFile duplique un objet public du bucket sous une nouvelle clé, côté R2
// (le fichier ne transite pas par l'API), et retourne l'URL publique de la
// copie. Le Content-Type de la source est conservé.
func (c *Client) CopyFile(ctx context.Context, srcKey, dstKey string) (string, error) {
	// CopySource doit être encodé comme un chemin d'URL : on encode chaque
	// segment de la clé en gardant les "/" qui la structurent.
	segments := strings.Split(srcKey, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}

	_, err := c.s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(c.bucket + "/" + strings.Join(segments, "/")),
		ACL:        types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		return "", fmt.Errorf("failed to copy in R2: %w", err)
	}

	return c.PublicURL(dstKey), nil
}

// GetKeyFromURL extrait la clé R2 depuis une URL publique. La query string
// (ex: cache-buster ?v=...) est ignorée pour retrouver la clé d'objet réelle.
// Les URLs historiques en pub-….r2.dev sont reconnues en plus du domaine
// public courant : les lignes déjà en base peuvent encore les porter.
func (c *Client) GetKeyFromURL(rawURL string) string {
	if idx := strings.Index(rawURL, "?"); idx != -1 {
		rawURL = rawURL[:idx]
	}
	baseURL := strings.TrimRight(c.publicBaseURL, "/")
	if baseURL != "" && strings.HasPrefix(rawURL, baseURL+"/") {
		return strings.TrimPrefix(rawURL, baseURL+"/")
	}
	if u, err := url.Parse(rawURL); err == nil && u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".r2.dev") {
		return strings.TrimPrefix(u.Path, "/")
	}
	return ""
}

// ImageBankPrefix : dossier R2 des images de la banque Wello Resto, communes à
// tous les établissements (table image_bank).
const ImageBankPrefix = "wello_resto_images_storage/image_bank/"

// IsImageBankKey indique si key désigne une image de la banque.
func IsImageBankKey(key string) bool {
	return strings.HasPrefix(key, ImageBankPrefix)
}

// GenerateImageBankKey génère la clé R2 d'une image de la banque. name est un
// identifiant déjà normalisé (minuscules, chiffres, tirets).
func GenerateImageBankKey(category, name, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ImageBankPrefix + category + "/" + name + ext
}

// IsImageKey : clé d'un fichier image accepté (jpg, png, webp).
func IsImageKey(key string) bool {
	k := strings.ToLower(key)
	return strings.HasSuffix(k, ".jpg") || strings.HasSuffix(k, ".jpeg") ||
		strings.HasSuffix(k, ".png") || strings.HasSuffix(k, ".webp")
}

// MerchantKeyPrefix : dossier R2 des fichiers d'un établissement.
func MerchantKeyPrefix(merchantID string) string {
	return "wello_resto_images_storage/merchants/" + merchantID + "/"
}

// GenerateProductKey génère la clé R2 pour un produit
func GenerateProductKey(merchantID, productID, ext string) string {
	// Nettoyer l'extension
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/products/%s%s", merchantID, productID, ext)
}

// GenerateScanNOrderKey génère la clé R2 pour les images de branding ScanNOrder.
// imageType doit être "logo" ou "banner".
func GenerateScanNOrderKey(merchantID, imageType, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/scannorder/%s%s", merchantID, imageType, ext)
}

// GenerateFranchiseKey génère la clé R2 des images du hub d'une franchise.
// imageType doit être "logo" ou "banner".
func GenerateFranchiseKey(franchiseID, imageType, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/franchises/%s/%s%s", franchiseID, imageType, ext)
}

// GenerateMerchantLogoKey génère la clé R2 pour le logo général de
// l'établissement (identité merchant, distinct du logo Kiosk/ScanNOrder).
// Clé déterministe : un nouvel upload écrase l'ancien fichier.
func GenerateMerchantLogoKey(merchantID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/logo%s", merchantID, ext)
}

// GenerateKioskKey génère la clé R2 pour les images de configuration Kiosk
// (logo merchant, image de veille). imageType doit être "logo" ou "idle".
// Clé déterministe : un nouvel upload écrase l'ancien fichier.
func GenerateKioskKey(merchantID, imageType, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/kiosk/%s%s", merchantID, imageType, ext)
}

// GenerateKioskMarketingMediaKey génère la clé R2 d'un média du ruban marketing
// des bornes Kiosk (table kiosk_marketing_media).
//
// Contrairement à GenerateKioskKey (un seul visuel de veille par merchant, clé
// déterministe qu'on écrase), le ruban est une rotation de plusieurs médias qui
// coexistent : chacun a sa propre clé, dérivée de son identifiant, supprimée
// individuellement quand il est retiré. Une clé n'étant jamais réécrite, aucun
// cache-buster n'est nécessaire.
func GenerateKioskMarketingMediaKey(merchantID, mediaID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/kiosk/marketing/%s%s", merchantID, mediaID, ext)
}

// GenerateConfigOptionKey génère la clé R2 pour l'image d'une option de
// configuration produit (ex. variantes "Coca / Sprite / Fanta" dans un
// wizard de personnalisation).
func GenerateConfigOptionKey(merchantID, optionID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/config_options/%s%s", merchantID, optionID, ext)
}

// GenerateProductCategoryKey génère la clé R2 pour l'image d'une catégorie
// de caisse (table productcateg).
func GenerateProductCategoryKey(merchantID, categoryID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/product_categories/%s%s", merchantID, categoryID, ext)
}

// GenerateMarketingCategoryKey génère la clé R2 pour l'image d'une catégorie
// marketing (table marketing_categories).
func GenerateMarketingCategoryKey(merchantID, categoryID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/marketing_categories/%s%s", merchantID, categoryID, ext)
}

// GetExtensionFromContentType retourne l'extension depuis le content type
func GetExtensionFromContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// ValidateImageType valide que le type MIME est accepté
func ValidateImageType(contentType string) bool {
	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/webp": true,
	}
	return allowedTypes[contentType]
}

// ValidateVideoType valide que le type MIME vidéo est accepté.
func ValidateVideoType(contentType string) bool {
	allowedTypes := map[string]bool{
		"video/mp4":  true,
		"video/webm": true,
	}
	return allowedTypes[contentType]
}

// GetVideoExtensionFromContentType retourne l'extension depuis le content
// type vidéo.
func GetVideoExtensionFromContentType(contentType string) string {
	switch contentType {
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}

// GenerateUserAvatarKey génère la clé R2 pour la photo de profil d'un utilisateur.
// La clé est déterministe (écrase l'ancienne photo à chaque upload).
func GenerateUserAvatarKey(userID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/users/%s/avatar%s", userID, ext)
}

// GenerateHACCPTraceabilityKey génère la clé R2 pour une photo de traçabilité
// HACCP (étiquettes/emballages). index est la position de la photo (0-based)
// dans la soumission ; chaque photo d'un même enregistrement a sa propre clé.
func GenerateHACCPTraceabilityKey(merchantID, recordID string, index int, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/haccp/tracabilite/%s/%d%s", merchantID, recordID, index, ext)
}

// GenerateCDSMediaKey génère la clé R2 d'un média de la zone marketing d'un
// écran d'affichage client (table cds_media_items).
//
// La clé est scopée par écran ET par média : contrairement à
// GenerateKioskKey (un seul visuel de veille par merchant, clé déterministe
// qu'on écrase), la zone CDS est une rotation de plusieurs médias qui
// coexistent. Chacun a donc son propre objet, supprimé individuellement
// quand le média est retiré de la rotation — et aucun cache-buster n'est
// nécessaire, puisqu'une clé n'est jamais réécrite.
func GenerateCDSMediaKey(merchantID, displayID, mediaID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/cds/%s/media/%s%s", merchantID, displayID, mediaID, ext)
}

// PublicURL reconstruit l'URL publique d'un objet à partir de sa clé R2.
// Utile quand seule la clé (pas l'URL) est persistée en base, ex.
// haccp_traceability_photos.photo_key.
func (c *Client) PublicURL(key string) string {
	return strings.TrimRight(c.publicBaseURL, "/") + "/" + key
}

// GetContentTypeFromExtension retourne le content type depuis l'extension
func GetContentTypeFromExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

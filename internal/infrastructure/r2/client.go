package r2

import (
	"context"
	"fmt"
	"io"
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

// DeleteFile supprime un fichier de R2
func (c *Client) DeleteFile(ctx context.Context, key string) error {
	_, err := c.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from R2: %w", err)
	}

	return nil
}

// GetKeyFromURL extrait la clé R2 depuis une URL publique. La query string
// (ex: cache-buster ?v=...) est ignorée pour retrouver la clé d'objet réelle.
func (c *Client) GetKeyFromURL(url string) string {
	if idx := strings.Index(url, "?"); idx != -1 {
		url = url[:idx]
	}
	baseURL := strings.TrimRight(c.publicBaseURL, "/")
	if strings.HasPrefix(url, baseURL+"/") {
		return strings.TrimPrefix(url, baseURL+"/")
	}
	return ""
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

// GenerateKioskKey génère la clé R2 pour les images de configuration Kiosk
// (logo merchant, image de veille). imageType doit être "logo" ou "idle".
// Clé déterministe : un nouvel upload écrase l'ancien fichier.
func GenerateKioskKey(merchantID, imageType, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("wello_resto_images_storage/merchants/%s/kiosk/%s%s", merchantID, imageType, ext)
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

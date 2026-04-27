package r2

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Client struct {
	s3Client      *s3.Client
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

// GetKeyFromURL extrait la clé R2 depuis une URL publique
func (c *Client) GetKeyFromURL(url string) string {
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

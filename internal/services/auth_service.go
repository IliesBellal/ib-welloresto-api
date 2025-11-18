package services

import (
	"bytes"
	"context"
	"crypto/aes"
	"errors"
	"strings"
	"welloresto-api/internal/repositories"
)

type AuthService struct {
	repo *repositories.UserRepository
}

func NewAuthService(r *repositories.UserRepository) *AuthService {
	return &AuthService{repo: r}
}

// Fonction utilitaire pour ajouter le padding (PKCS#7)
func pkcs7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

func encryptPHP(password string) (string, error) {
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

func convertApp(app string) (int, error) {
	switch strings.ToUpper(app) {
	case "0", "WR_RECEPTION": return 0, nil
	case "1", "WR_DELIVERY": return 1, nil
	case "2", "WR_WAITER": return 2, nil
	default: return -1, errors.New("invalid app")
	}
}

func (s *AuthService) Login(ctx context.Context, app string, deviceID string, username string, password string, token string) (map[string]interface{}, error) {

	appID, _ := convertApp(app)

	var encrypted string
	var err error

	if username != "" && password != "" {
		encrypted, err = encryptPHP(password)
		if err != nil { return nil, err }
	}

	user, err := s.repo.Login(ctx, username, encrypted, password, token)
	if err != nil { return nil, err }
	if user == nil {
		return map[string]interface{}{
			"status": "0",
			"enabled": "no user found",
		}, nil
	}

	if !user.Enabled {
		return map[string]interface{}{
			"status": "3",
			"enabled": "false",
		}, nil
	}

	switch appID {
	case 0:
		if !user.AccessReception {
			return map[string]interface{}{
				"status": "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	case 1:
		if !user.AccessDelivery || !user.AllowDeliveryAccount {
			return map[string]interface{}{
				"status": "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	case 2:
		if !user.AccessWaiter || !user.AllowWaiterAccount {
			return map[string]interface{}{
				"status": "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	}

	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	// JSON EXACT
	return map[string]interface{}{
		"status": "1",
		"device_cash_desk": nil, // à implémenter plus tard
		"enabled": "true",

		"name": user.Name,
		"first_name": user.FirstName,
		"userId": user.UserID,
		"user_mail": user.Email,
		"user_tel": user.Tel,

		"merchantId": user.MerchantID,
		"merchantName": user.MerchantName,
		"merchantTel": user.MerchantTel,
		"merchantAd": user.MerchantAddress,
		"merchant_lat": user.MerchantLat,
		"merchant_lng": user.MerchantLng,
		"timezone": user.TimeZone,
		"merchantLogo": user.MerchantLogo.String,

		"SNOSettings": map[string]interface{}{
			"activated": user.SNOActivated,
		},

		"integration_uber_eats": map[string]interface{}{
			"store_id": user.UEStoreID.String,
			"estimated_preparation_time": user.UEPrepTime.String,
			"delay_until": user.UEDelayUntil.Time,
			"delay_duration": user.UEDelayDuration.Int64,
			"closed_until": user.UEClosedUntil.Time,
		},

		"integration_uber_direct": map[string]interface{}{
			"customer_id": user.UDCustomerID.String,
		},

		"integration_deliveroo": map[string]interface{}{
			"location_id": user.DrooLocationID.String,
		},

		"scannorder_ready": user.ScanNOrderReady,
		"manage_on_site": user.ManageOnSite,
		"manage_take_away": user.ManageTakeAway,
		"manage_delivery": user.ManageDelivery,
		"stock_management": user.StockManagement,
		"hr_management": user.HrManagement,
		"service_required_for_ordering": user.ServiceRequiredForOrdering,
		"safety_stock_active": user.DisableSafetyStock,
		"warning_new_order_not_paid": user.WarningNewOrderNotPaid,

		"currency": user.Currency,
		"is_open": user.IsOpen,
		"pin_code": user.PinCode.String,
		"web_site": user.WebSite.String,
		"token": user.RightsToken,
		"profile_picture": user.ProfilePicture.String,

		"merchants": merchants,
	}, nil
}

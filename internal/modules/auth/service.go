package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"errors"
	"strconv"
	"strings"
	"welloresto-api/internal/logger"
)

type AuthService struct {
	repo AuthRepository
}

func NewAuthService(r AuthRepository) AuthService {
	return AuthService{repo: r}
}

func (s *AuthService) GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error) {
	return s.repo.GetUserByToken(ctx, token)
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
	case "0", "WR_RECEPTION":
		return 0, nil
	case "1", "WR_DELIVERY":
		return 1, nil
	case "2", "WR_WAITER":
		return 2, nil
	default:
		return -1, errors.New("invalid app")
	}
}

func (s *AuthService) Login(ctx context.Context, payload LoginRequestPayload, token string) (map[string]interface{}, error) {

	appID, _ := convertApp(payload.App)

	var encrypted, username string
	var err error

	if (payload.Username != "" || payload.Email != "") && payload.Password != "" {
		encrypted, err = encryptPHP(payload.Password)
		if err != nil {
			log := logger.FromContext(ctx)
			log.Error("EncryptPHP Error: " + err.Error())
			return nil, err
		}
	}
	username = payload.Username + payload.Email

	user, err := s.repo.Login(ctx, username, encrypted, payload.Password, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return map[string]interface{}{
			"status":  "user_not_found", // 0
			"enabled": "no user found",
		}, nil
	}

	if !user.Enabled {
		return map[string]interface{}{
			"status":  "account_disabled", //3
			"enabled": "false",
		}, nil
	}

	switch appID {
	case 0:
		if !user.AccessReception {
			return map[string]interface{}{
				"status":  "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	case 1:
		if !user.AccessDelivery || !user.AllowDeliveryAccount {
			return map[string]interface{}{
				"status":  "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	case 2:
		if !user.AccessWaiter || !user.AllowWaiterAccount {
			return map[string]interface{}{
				"status":  "user_not_allowed",
				"enabled": "User can't access this app",
			}, nil
		}
	}

	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	// JSON EXACT
	return map[string]interface{}{
		"status":           "1",
		"device_cash_desk": nil, // à implémenter plus tard
		"enabled":          "true",

		"name":                  user.Name,
		"first_name":            user.FirstName,
		"userId":                user.UserID,
		"user_mail":             user.Email,
		"user_tel":              user.Tel,
		"open_cash_drawer":      user.OpenCashDrawer,
		"terms_of_use_accepted": user.TermsOfUseAccepted,
		"admin":                 user.Admin,

		"merchantId":                          user.MerchantID,
		"merchant_id":                         user.MerchantID,
		"merchantName":                        user.MerchantName,
		"business_name":                       user.MerchantName,
		"merchantTel":                         user.MerchantTel,
		"merchant_tel":                        user.MerchantTel,
		"delivery_fees":                       user.DeliveryFees,
		"delivery_fees_limit":                 user.DeliveryFeesLimit,
		"kitchen_show_only_paid":              user.KitchenShowOnlyPaid,
		"allow_waiter_account":                user.AllowWaiterAccount,
		"print_merchant_cash_report":          user.PrintMerchantCashReport,
		"merchantAd":                          user.MerchantAddress,
		"merchant_address":                    user.MerchantAddress,
		"merchant_lat":                        user.MerchantLat,
		"delivery_distance_limit":             user.DeliveryDistanceLimit,
		"kitchen_distribution_mode":           user.KitchenDistributionMode,
		"production_display_mode":             user.ProductionDisplayMode,
		"pager_number_required":               user.PagerNumberRequired,
		"cash_register_required_for_ordering": user.CashRegisterRequiredForOrdering,
		"merchant_lng":                        user.MerchantLng,
		"timezone":                            user.TimeZone,
		//"merchantLogo":                        user.MerchantLogo.String,

		"SNOSettings": map[string]interface{}{
			"activated": user.SNOActivated,
		},

		"integration_uber_eats": map[string]interface{}{
			"store_id":                   user.UEStoreID.String,
			"estimated_preparation_time": user.UEPrepTime.String,
			"delay_until":                user.UEDelayUntil,
			"delay_duration":             user.UEDelayDuration.Int64,
			"closed_until":               user.UEClosedUntil,
		},

		"integration_uber_direct": map[string]interface{}{
			"customer_id": user.UDCustomerID.String,
		},

		"integration_deliveroo": map[string]interface{}{
			"location_id": user.DrooLocationID.String,
		},

		"scannorder_ready":              user.ScanNOrderReady,
		"manage_on_site":                user.ManageOnSite,
		"manage_take_away":              user.ManageTakeAway,
		"manage_delivery":               user.ManageDelivery,
		"stock_management":              user.StockManagement,
		"hr_management":                 user.HrManagement,
		"service_required_for_ordering": user.ServiceRequiredForOrdering,
		"safety_stock_active":           user.DisableSafetyStock,
		"warning_new_order_not_paid":    user.WarningNewOrderNotPaid,

		"currency":          user.Currency,
		"is_open":           user.IsOpen,
		"pin_code":          user.PinCode.String,
		"merchant_web_site": user.WebSite.String,
		"token":             user.RightsToken,
		"profile_picture":   user.ProfilePicture.String,

		"merchants": merchants,
	}, nil
}

func (s *AuthService) CheckAppVersion(ctx context.Context, token, versionCodeString, app string) (map[string]interface{}, error) {

	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	versionCode, err := strconv.Atoi(versionCodeString)
	if err != nil {
		return nil, errors.New("invalid version number")
	}

	// Business logic
	result, err := s.repo.CheckAppVersion(ctx, versionCode, app, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *AuthService) SaveDeviceToken(ctx context.Context, token, deviceToken, deviceID, app string) (map[string]string, error) {

	user, err := s.repo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	err = s.repo.SaveDevice(ctx, user.UserID, user.MerchantID, app, deviceID, deviceToken)
	if err != nil {
		return map[string]string{
			"status": "-3",
			"error":  err.Error(),
		}, nil
	}

	return map[string]string{"status": "1"}, nil
}

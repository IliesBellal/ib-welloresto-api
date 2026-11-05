package mailer

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	BrandLogoURL  = "https://scannorder.welloresto.fr/src/tracking-preview.png"
	SupportEmail  = "support@welloresto.fr"
	InvoiceEmail  = "invoice@welloresto.fr"
	SecurityEmail = "security@welloresto.fr"
)

// Couleurs par défaut d'un établissement : celles de merchant_parameters
// (primary_color, text_color_on_primary_color).
const (
	DefaultBrandColor     = "#212529"
	DefaultBrandTextColor = "#ffffff"
)

// MerchantBrand est l'identité d'un établissement dans les mails envoyés en
// son nom (en-tête et pied « head_merchant » / « foot_merchant » de
// partial_ui.html, cf. docs/emails-styles.md) : nom, logo, initiales affichées
// à la place d'un logo absent, couleur du logo de repli et du bouton, couleur
// du texte posé dessus. Code est le slug ScanNOrder de l'établissement (QR
// principal), repris en utm_campaign du lien « Propulsé par Wello Resto » ;
// vide si inconnu. Construit par NewMerchantBrand (Code posé ensuite par
// merchantbrand.Repository).
type MerchantBrand struct {
	Name      string
	LogoURL   string
	Initials  string
	Color     string
	TextColor string
	Code      string
}

var hexColorPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// NewMerchantBrand normalise l'identité d'un établissement : une couleur absente
// ou invalide prend la valeur par défaut ; une couleur de texte absente ou
// invalide est choisie (blanc ou foncé) selon la clarté du fond ; un logo qui
// n'est pas une URL http(s) est ignoré.
func NewMerchantBrand(name, logoURL, color, textColor string) MerchantBrand {
	color = strings.TrimSpace(color)
	if !hexColorPattern.MatchString(color) {
		color = DefaultBrandColor
	}
	textColor = strings.TrimSpace(textColor)
	if !hexColorPattern.MatchString(textColor) {
		textColor = readableTextOn(color)
	}
	logoURL = strings.TrimSpace(logoURL)
	if !strings.HasPrefix(logoURL, "https://") && !strings.HasPrefix(logoURL, "http://") {
		logoURL = ""
	}
	name = strings.TrimSpace(name)
	return MerchantBrand{Name: name, LogoURL: logoURL, Initials: initials(name), Color: color, TextColor: textColor}
}

// initials renvoie les initiales d'un nom d'établissement, au plus deux
// lettres ou chiffres, en majuscules : "Croq'O Pizzas" -> "CP", "Timgad" -> "T",
// "" -> "". Seule la première lettre de chaque mot compte (l'apostrophe ne
// coupe pas un mot).
func initials(name string) string {
	var out []rune
	for _, word := range strings.Fields(name) {
		for _, r := range word {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				out = append(out, unicode.ToUpper(r))
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

// Liens et images Wello Resto des mails. Les PNG sont servis par le site
// vitrine (wello-resto-vitrine/public/email/) : les messageries n'affichent
// pas le SVG.
const (
	WebsiteURL = "https://welloresto.fr/"
	// WelloTileURL : W blanc sur carré bleu arrondi, 120 px, affiché en 40 px
	// dans l'en-tête des mails Wello Resto.
	WelloTileURL = "https://welloresto.fr/email/wello-resto-tile.png"
	// WelloMarkURL : W gris seul, 72 x 48 px, affiché en 21 x 14 px dans
	// « Propulsé par Wello Resto ».
	WelloMarkURL = "https://welloresto.fr/email/wello-resto-mark-grey.png"
)

// PoweredByURL renvoie le lien du site vitrine porté par « Propulsé par Wello
// Resto » au pied des mails clients, avec les mêmes paramètres que le lien
// équivalent de ScanNOrder (PoweredBy.tsx) : utm_source=email,
// utm_medium=powered_by, utm_campaign = slug de l'établissement (omis s'il est
// inconnu), utm_content = nom du mail (ex. "booking_confirmation"). Aucune
// donnée sur le client. La vitrine garde source, medium et campaign avec une
// demande de démo ; son URL canonique exclut ces paramètres.
func PoweredByURL(merchantCode, mail string) string {
	q := url.Values{}
	q.Set("utm_source", "email")
	q.Set("utm_medium", "powered_by")
	if c := strings.TrimSpace(merchantCode); c != "" {
		q.Set("utm_campaign", c)
	}
	if m := strings.TrimSpace(mail); m != "" {
		q.Set("utm_content", m)
	}
	return WebsiteURL + "?" + q.Encode()
}

// FormatEuros formate des centimes à la française : 128460 -> "1 284,60 €",
// -1550 -> "-15,50 €". Espace insécable fine avant les milliers et le symbole
// pour qu'un montant ne soit jamais coupé en fin de ligne.
func FormatEuros(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := strconv.FormatInt(cents/100, 10)
	var groups []string
	for len(whole) > 3 {
		groups = append([]string{whole[len(whole)-3:]}, groups...)
		whole = whole[:len(whole)-3]
	}
	groups = append([]string{whole}, groups...)
	return fmt.Sprintf("%s%s,%02d €", sign, strings.Join(groups, " "), cents%100)
}

// readableTextOn renvoie le blanc ou un gris très foncé, celui qui se lit le
// mieux sur la couleur hex donnée (luminance relative WCAG).
func readableTextOn(color string) string {
	hex := strings.TrimPrefix(color, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	var lum float64
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		v, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			return DefaultBrandTextColor
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			c /= 12.92
		} else {
			c = math.Pow((c+0.055)/1.055, 2.4)
		}
		lum += weight * c
	}
	// Contraste avec le blanc contre contraste avec #1a1a1a (luminance ≈ 0.0103).
	if 1.05/(lum+0.05) >= (lum+0.05)/0.0603 {
		return DefaultBrandTextColor
	}
	return "#1a1a1a"
}

// RefundData pour charge.refunded
type RefundData struct {
	Brand         MerchantBrand
	MerchantName  string // ex: "Burger King"
	MerchantLogo  string // URL du logo
	MerchantColor string // ex: "#E2F2F9" (Optionnel, pour le header)
	Amount        string // ex: "15.50 €"
	Date          string // ex: "12/02/2024"
	CustomerName  string // Nom du client
	RefundReason  string // ex: "requested_by_customer" (traduire en FR)
	PaymentMethod string // ex: "Visa" ou "Link"
	PaymentDetail string // ex: "•••• 4242" ou "FR"
	CardBrand     string // ex: "Visa"
	CardLast4     string // ex: "4242"
	ReceiptURL    string // URL du reçu Stripe
	SupportEmail  string // Email support
}

// PayoutData pour payout.paid
// Establishment identifie l'établissement concerné par un mail adressé au
// restaurateur : plusieurs établissements d'un même groupe peuvent recevoir
// leurs mails à la même adresse. Affiché par le bloc partagé "establishment"
// (templates/partial_establishment.html) ; ID est le merchant_id.
type Establishment struct {
	ID      string
	Name    string
	Address string
}

// SubjectSuffix renvoie " — <nom>" à ajouter à l'objet d'un mail, pour
// distinguer les établissements dans une même boîte ("" si le nom est inconnu).
func (e Establishment) SubjectSuffix() string {
	if strings.TrimSpace(e.Name) == "" {
		return ""
	}
	return " — " + e.Name
}

type PayoutData struct {
	Establishment Establishment
	MerchantName string
	MerchantLogo string
	Destination  string
	Status       string
	Amount       string // ex: "1450.00 €"
	PayoutDate   string // Date du virement
	ArrivalDate  string // Date estimée sur le compte (arrival_date)
	BankName     string // ex: "BNP Paribas" ou "Stripe Balance"
	AccountLast4 string // ex: "6789"
	PayoutID     string // ex: "po_1Mn..."
	DashboardURL string // Lien vers le dashboard Stripe/Wello
}

// Used when sending an order confirmation
type ScanNOrderConfirmationData struct {
	Brand            MerchantBrand
	MerchantName     string // $merchant->business_name
	MerchantLogo     string // $merchant->logo_url
	MerchantCurrency string // Inutilisé par le modèle depuis le 2026-11-04 : la devise est dans OrderTotal
	OrderTotal       string // Montant complet à la française, devise comprise : "33,00 €" (FormatEuros)
	OrderNumber      string // Numéro de commande donné au comptoir (orders.order_num), comme dans le SMS ; vide si inconnu
	OrderDate        string // Formaté: "12/02/2024"
	TrackingURL      string // L'URL complète de suivi
	PrivacyURL       string // Lien politique de confidentialité
	TermsURL         string // Lien conditions générales
	SupportEmail     string // "Wello Resto SAS..."
}

type MfaOTPData struct {
	UserName  string
	UserEmail string
	OTP       string
}

type EmailBaseData struct {
	BrandName    string
	BrandLogoURL string
	SupportEmail string
	Year         int
}

// Pour la confirmation de compte
type ConfirmationEmailData struct {
	EmailBaseData
	FirstName       string
	ConfirmationURL string
}

// Pour le code MFA
type MFAMailData struct {
	EmailBaseData
	MFACode   string
	ExpiresIn int // en minutes
}

// PasswordResetData est ce que l'appelant fournit pour un email
// « mot de passe oublié ». ResetURL contient le token en clair : ne jamais
// la journaliser (cf. docs/PASSWORD_RESET.md).
type PasswordResetData struct {
	UserEmail string
	FirstName string
	ResetURL  string
	ExpiresIn int // en minutes
}

// PasswordResetMailData est le modèle passé au template password_reset.html.
type PasswordResetMailData struct {
	EmailBaseData
	FirstName string
	ResetURL  string
	ExpiresIn int // en minutes
}

// Attachment est une pièce jointe de mail (fichier complet en mémoire).
type Attachment struct {
	Name    string
	Content []byte
}

// PayoutDocumentsData pour le mail « relevé de versement + facture de commission ».
type PayoutDocumentsData struct {
	Establishment Establishment
	MerchantName  string
	PayoutID      string
	Amount        string // ex : "1450,00 €"
	ArrivalDate   string // ex : "07/11/2026"
	BackOfficeURL string // lien vers les virements dans le back-office
	SupportEmail  string
	// TestNotice, non vide, est affiché en tête du mail (phase de test : le
	// destinataire n'est pas le restaurateur).
	TestNotice string
}

// CommissionInvoiceData pour le mail mensuel « facture de commission +
// récapitulatif » (deux PDF en pièces jointes).
type CommissionInvoiceData struct {
	Establishment Establishment
	MerchantName  string
	InvoiceNumber string
	Month         string // ex : "octobre 2026"
	Amount        string // commission TTC, ex : "42,00 €"
	PayoutCount   int
	BackOfficeURL string // lien vers les factures dans le back-office
	SupportEmail  string
	// TestNotice, non vide, est affiché en tête du mail (le destinataire n'est
	// pas le restaurateur).
	TestNotice string
}

// SubscriptionInvoiceData pour le mail « facture d'abonnement » (PDF Stripe en pièce jointe).
type SubscriptionInvoiceData struct {
	Establishment Establishment
	MerchantName  string
	InvoiceNumber string
	Amount        string // ex : "79,00 €"
	Period        string // ex : "du 01/10/2026 au 31/10/2026" ; vide si inconnue
	HostedURL     string // page de la facture chez Stripe ; vide si inconnue
	SupportEmail  string
	// TestNotice, non vide, est affiché en tête du mail (le destinataire n'est
	// pas le restaurateur).
	TestNotice string
}

// InvoiceEmailData pour l'envoi de facture PDF en pièce jointe
type InvoiceEmailData struct {
	Brand         MerchantBrand
	MerchantName  string
	CustomerName  string
	ReceiptNumber string
	OrderNumber   string // numéro de commande affiché au client ; vide pour l'envoi depuis la caisse
	SupportEmail  string
}

// WaitlistAvailableData pour la notification "une table s'est libérée"
// envoyée au premier de la liste d'attente lors d'un no-show / expiration.
type WaitlistAvailableData struct {
	EmailBaseData
	Brand         MerchantBrand
	MerchantName  string
	CustomerName  string
	PartySize     int
	ExpiryMinutes int
}

// BookingMessageData est la donnée partagée par les emails du cycle de vie
// d'une réservation (confirmation, rappel, modification, annulation,
// reconfirmation). ManagementLink est vide si non pertinent pour le type de
// message (ex: annulation) ou si aucune base URL publique n'est configurée.
type BookingMessageData struct {
	EmailBaseData
	Brand          MerchantBrand
	MerchantName   string
	CustomerName   string
	BookingNumber  string
	DateLabel      string // date formatée locale, ex "vendredi 12 juillet 2026"
	TimeLabel      string // heure formatée locale, ex "20:00"
	PartySize      int
	ManagementLink string
	// CalendarLink : lien « ajouter à Google Agenda », renseigné quand un
	// fichier .ics accompagne le mail (confirmation, modification).
	CalendarLink string
}

// BookingPostVisitData pour le message post-visite (remerciement + demande
// d'avis — Should du cadrage §6.4, préparé mais non branché dans le cron).
type BookingPostVisitData struct {
	EmailBaseData
	Brand        MerchantBrand
	MerchantName string
	CustomerName string
}

// DemoRequestData est le récapitulatif envoyé en interne (jamais au
// visiteur) à chaque soumission de DemoForm.astro sur le site vitrine —
// remplace Web3Forms (stockage hors UE, écarté sur avis juridique). Depuis le
// chantier créneaux engageants (2026-09-24), Slot est un horaire réellement
// réservé (voir demorequest.Slot), plus une simple préférence texte.
// GoogleCalendarLink ajouté le 2026-09-25 : l'équipe Wello Resto doit pouvoir
// ajouter le rendez-vous à son propre agenda aussi facilement que le client
// (même lien "quick add" + même .ics en pièce jointe que DemoConfirmationData
// ci-dessous, générés une seule fois par demorequest.Service.Create pour les
// deux e-mails).
type DemoRequestData struct {
	EmailBaseData
	Establishment        string
	EstablishmentAddress string // vide si l'autocomplétion Google Places n'a pas abouti
	RestaurantType       string
	Phone                string
	Situation            string
	Origin               string // paramètres utm_* de la visite, ex. "depliant · print · dpl_sno_brn_2610" ; vide = visite directe
	Slot                 string // ex: "vendredi 12 juillet 2026 à 9h00"
	GoogleCalendarLink   string
}

// DemoConfirmationData est envoyé au VISITEUR (contrairement à
// DemoRequestData, interne) une fois son créneau de démo confirmé — avec un
// fichier .ics en pièce jointe (voir demorequest.buildICS) et un lien Google
// Agenda en complément (aucune intégration Google Calendar API nécessaire,
// juste une URL "quick add" — voir docs/decisions-log.md du site vitrine
// pour pourquoi l'intégration complète a été écartée).
type DemoConfirmationData struct {
	EmailBaseData
	Establishment      string
	DateLabel          string // ex: "vendredi 12 juillet 2026"
	TimeLabel          string // ex: "9h00"
	Phone              string // numéro qui sera appelé, rappelé pour confirmation
	GoogleCalendarLink string
}

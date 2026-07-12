package brevo_sms_reply

import "strings"

// BrevoSMSReply modélise un webhook SMS entrant Brevo. Les noms de champs
// variant selon la configuration côté Brevo, on accepte plusieurs alias et on
// retient la première valeur non vide.
type BrevoSMSReply struct {
	From    string `json:"from"`
	Sender  string `json:"sender"`
	MSISDN  string `json:"msisdn"`
	Text    string `json:"text"`
	Message string `json:"message"`
	Content string `json:"content"`
}

// Phone retourne le numéro de l'expéditeur (premier alias non vide).
func (b BrevoSMSReply) Phone() string {
	return firstNonEmpty(b.From, b.Sender, b.MSISDN)
}

// Body retourne le texte du SMS (premier alias non vide).
func (b BrevoSMSReply) Body() string {
	return firstNonEmpty(b.Text, b.Message, b.Content)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Intent représente l'action déduite du texte du SMS.
type Intent string

const (
	IntentReconfirm Intent = "reconfirm"
	IntentCancel    Intent = "cancel"
	IntentIgnore    Intent = "ignore"
)

var reconfirmTokens = map[string]bool{"CONFIRMER": true, "OUI": true, "YES": true}
var cancelTokens = map[string]bool{"ANNULER": true, "NON": true, "NO": true, "CANCEL": true}

// ParseIntent déduit l'intention à partir du premier mot du SMS.
func ParseIntent(text string) Intent {
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(text)))
	if len(fields) == 0 {
		return IntentIgnore
	}
	switch first := fields[0]; {
	case reconfirmTokens[first]:
		return IntentReconfirm
	case cancelTokens[first]:
		return IntentCancel
	default:
		return IntentIgnore
	}
}

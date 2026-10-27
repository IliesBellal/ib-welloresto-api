# Copie des mails et SMS

**Statut : temporaire — à retirer le 15 décembre 2026** (voir [REMINDERS.md](REMINDERS.md)). Aucune variable d'environnement : tout est en constantes dans le code.

## Mails (copie cachée)

Tous les mails partis via Brevo (`internal/infrastructure/brevo_mailer`) sont aussi envoyés en **copie cachée (`bcc`)** à `iliesbellal@gmail.com`. Le destinataire ne voit pas cette adresse. Pas de copie si le destinataire est déjà cette adresse.

Couvre les trois constructeurs de payload de [service.go](../internal/infrastructure/brevo_mailer/service.go) (sans, une, ou plusieurs pièces jointes), donc tous les types de mail : OTP, mot de passe oublié, réservations, factures, relevés, relances. Code : [brevo_mailer/copy.go](../internal/infrastructure/brevo_mailer/copy.go).

## SMS (second envoi)

L'API SMS de Brevo n'a pas de copie cachée : chaque SMS accepté par Brevo déclenche un **second SMS** vers `+33609217928` (le numéro de test déjà utilisé par `TriggerTestSMS`), avec le texte préfixé `[copie → <numéro d'origine>]` pour savoir à qui il était destiné. Code : [brevo_sms/copy.go](../internal/infrastructure/brevo_sms/copy.go), appelé depuis `sendSMSViaBrevo`, point de passage de tous les SMS.

- La copie part après l'acceptation du SMS réel, dans une goroutine : un échec de la copie est seulement loggué et n'affecte jamais l'envoi réel ni sa valeur de retour (le compteur `merchantsms` ne compte que les SMS réels).
- Le préfixe rallonge le message : il peut passer à un segment de plus.
- Ça consomme du volume SMS Brevo (un SMS de plus par envoi) ; sans coût pour le moment.
- Pas de copie si le destinataire est déjà ce numéro.

## Décisions

- **BCC et non CC** pour les mails : une copie visible exposerait l'adresse à tous les clients.
- **Les messages sensibles sont inclus** (OTP par mail et SMS, lien de réinitialisation de mot de passe) : demande explicite « tous les mails / SMS ».
- **Pas de variable d'environnement** (demande explicite) : un changement ou un retrait passe par le code.

## Retrait (15 décembre 2026)

1. Supprimer `brevo_mailer/copy.go` + `copy_test.go` et les trois appels `withCopy(payload, to)` dans `brevo_mailer/service.go`.
2. Supprimer `brevo_sms/copy.go` + `copy_test.go`, et dans `brevo_sms/service.go` remettre `sendSMSViaBrevo` sous le nom `postSMS` (supprimer le wrapper qui lance `sendCopy`).
3. Retirer cette page et l'entrée de `REMINDERS.md`.

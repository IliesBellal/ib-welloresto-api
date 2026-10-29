# Styles des e-mails

Refonte des modèles d'e-mails de l'API (`internal/infrastructure/mailer/templates/`), décidée et codée le 2026-10-26. Ce document décrit les trois cadres, comment écrire un nouveau mail et ce qui reste à faire.

> Statut : **codé, non commité, non déployé.** Aucune migration. Complété le même jour par le fichier d'agenda des réservations et la facture automatique des commandes en ligne (sections dédiées). À valider visuellement dans de vraies messageries (Gmail web et mobile, Apple Mail, Outlook) avant la mise en production.

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| D1 | Trois familles selon **qui envoie et qui lit** : clients d'un établissement, restaurateurs, équipe et sécurité. | Le client connaît le restaurant, pas Wello Resto ; le restaurateur attend des chiffres et des documents ; un mail de sécurité doit être reconnaissable. |
| D2 | Choix d'Ilies parmi 8 propositions : **Maison (C2) avec le bouton pleine largeur de Statut (C3)** pour les clients, **Tableau de bord (R2)** pour les restaurateurs, **Coffre (S1)** pour l'équipe et la sécurité. | Maquettes comparées sur les mêmes contenus. |
| D3 | Le cadre client prend la **couleur de marque de l'établissement** (`merchant_parameters.primary_color` / `text_color_on_primary_color`, déjà utilisées par ScanNOrder et la réservation) et son logo (`merchant.logo_url`). | Pas de nouveau réglage à faire saisir au restaurateur. |
| D4 | Blocs partagés (`partial_*.html`) plutôt qu'un HTML complet par mail. | Un seul endroit à corriger pour 26 mails ; impossible d'oublier le pied de page ou le pré-en-tête. |
| D5 | Mise en page en tableaux, styles en ligne, polices système (Helvetica/Arial, Georgia, Menlo), largeur 600 px, pas d'image hormis le logo. | Seule structure rendue partout, Outlook et Gmail compris (Gmail ignore les polices web). |
| D6 | Les cinq relances (`dunning_*`, `trial_expiry_reminder`) sont créées. | Elles n'existaient pas : le rendu échouait à l'envoi, l'erreur était journalisée puis ignorée. Aucune relance de paiement ni rappel de fin d'essai n'est jamais parti. |

## Les trois cadres

| Cadre | Fichier | Fond | Utilisé par |
|---|---|---|---|
| **Maison** (C2 + bouton C3) | `partial_client.html` | `#f4f4f2` | réservations (6), liste d'attente, confirmation de commande (`scannorder_payout.html`), remboursement, facture client |
| **Tableau de bord** (R2) | `partial_pro.html` + `partial_establishment.html` | `#f3f5fa` | virement, relevé, facture de commission, facture d'abonnement, attestation, relances (4), fin d'essai, démo (2) |
| **Coffre** (S1) | `partial_secure.html` | `#f5f5f7` | code de vérification, mot de passe, confirmation de compte, planning |

Tous partent de `partial_layout.html` (`doc_start` / `doc_end`) : document, pré-en-tête masqué (la ligne grise sous l'objet dans la boîte de réception) et colonne de 600 px.

- **Maison** : bandeau à la couleur de l'établissement (logo + nom en Georgia), titre en serif, informations clés en liste à filets, bouton pleine largeur arrondi (12 px) à la couleur de l'établissement, signature au nom de l'établissement, pied « Envoyé avec Wello Resto ».
- **Tableau de bord** : bandeau bleu `#0036d9` avec le logo texte Wello Resto, le titre et le chiffre clé ; détail en cartes blanches ; bouton bleu pleine largeur. Couleur du bandeau selon l'urgence : **bleu `#0036d9`** (information), **orange `#b45309`** (paiement en attente sans date de suspension : premier échec, relances hebdomadaires avant l'ouverture de la fenêtre), **rouge `#a32121`** (suspension annoncée : relance 2, relances hebdomadaires avec date, dernier rappel). Orange demandé par Ilies le 2026-10-26 : un échec de paiement est un avertissement dès le premier mail.
- **Coffre** : carte blanche centrée, pastille (cadenas ou calendrier), code en très grand groupé par 3 (deux `<span>` : un copier-coller donne `482913`, sans espace), bouton sombre, encadré gris de mise en garde.
  - **Copier le code** : un bouton « Copier » est impossible dans un mail (aucune messagerie n'exécute de JavaScript). À la place, le code est dans l'**objet** (« 482913 est votre code de vérification Wello Resto ») et le pré-en-tête, ce qui déclenche la proposition de copie d'iOS (remplissage automatique des codes reçus par Mail) et de Gmail ; et la case du code porte `user-select: all` (un appui sélectionne tout le code dans Apple Mail). Contrepartie acceptée : le code est visible dans la notification, sur l'écran verrouillé. Pour le planning, l'en-tête et le bouton prennent le nom et la couleur de l'établissement.

## Fichier d'agenda des réservations (.ics)

Ajouté le 2026-10-26 (`internal/ics`, partagé avec la démo ; `bookingcomm.sendEmail`).

| Mail | Fichier joint | Lien Google Agenda |
|---|---|---|
| Confirmation | `METHOD:PUBLISH` | oui |
| Modification d'une réservation confirmée | `METHOD:PUBLISH`, même UID, SEQUENCE plus grande : l'agenda déplace l'événement | oui |
| Modification qui repasse en attente (pas d'acceptation automatique) | aucun | non |
| Annulation d'une réservation qui était confirmée (ou installée) | `METHOD:CANCEL` / `STATUS:CANCELLED` : l'événement est retiré | non |
| Annulation d'une demande en attente, expiration, refus | aucun (jamais ajoutée à l'agenda) | non |
| Rappel, reconfirmation | aucun | non |

- **UID** : `<booking_id>@reservation.welloresto.fr`, stable d'un envoi à l'autre. **SEQUENCE** : minutes Unix au moment de l'envoi, donc toujours croissante sans rien stocker.
- **Lieu** : nom et adresse de l'établissement. **Description** : couverts, référence, lien de gestion.
- Les appelants (`bookings`, `reservation`) renseignent `StartAt`, `EndAt`, `MerchantAddress` et `Confirmed` dans `BookingMessage` ; sans horaires, pas de fichier.
- `METHOD:PUBLISH` plutôt que `REQUEST` : pas de boutons « Accepter / Refuser » d'invitation. Contrepartie : certaines messageries peuvent créer un second événement à la modification au lieu de déplacer le premier (à vérifier en recette, R1).

## Facture automatique des commandes en ligne

Ajoutée le 2026-10-26. Décision d'Ilies : **seulement dès 25 € TTC** (seuil de la note obligatoire en restauration ; en dessous, le client la demande en caisse).

- **Tâche** `RunOnlineOrderInvoices`, toutes les 5 minutes (`cmd/api/tasks.go`), une instance à la fois (`fiscal.TryLock`). Elle appelle `OrdersLifeCycleService.SendOnlineOrderInvoices` (`order_life_cycle/online_invoices.go`).
- **Commandes concernées** : marque `WELLO_RESTO` (ni Uber Eats ni Deliveroo), close (`state = 'CLOSED'`) depuis moins de 2 h, au moins un paiement en ligne Stripe actif (inscrit par le webhook, `user_id = 'WEBHOOK_STRIPE'`), ticket de vente d'au moins 2 500 centimes TTC, client avec e-mail (enregistré au paiement Stripe), pas déjà facturée.
- **Anti-doublon** : chaque envoi est inscrit dans `outbound_messages` (`domain = 'order_invoice'`, `domain_ref_id` = commande) ; une commande inscrite n'est jamais refacturée. Pas de migration. **L'envoi depuis la caisse** (`SendInvoiceByEmail`) passe par le même envoi (`sendOrderInvoiceMail`) et la même inscription (`journalOrderInvoice`) : une facture envoyée à la main n'est pas renvoyée par la tâche, et elle part elle aussi aux couleurs de l'établissement avec le numéro de commande. Un renvoi manuel reste toujours possible. Si l'inscription échoue après un envoi manuel réussi, la réponse ne change pas (avertissement journalisé) ; au pire, la tâche renvoie une fois la facture.
- **Fenêtre de 2 h** : au déploiement, seules les commandes closes dans les 2 dernières heures reçoivent leur facture (pas d'envoi de l'historique) ; une commande en échec est retentée toutes les 5 minutes pendant ce temps, puis abandonnée (journalisé).
- **PDF** : exactement celui de l'envoi depuis la caisse (`orderInvoicePDF`, extrait de `SendInvoiceByEmail`) : construit depuis le ticket de vente figé (aucun recalcul), détail de la TVA par taux, en-tête de l'établissement.
- **Mail** : `invoice_email.html` (cadre Maison aux couleurs de l'établissement), objet « Votre facture <établissement> — commande n° <numéro> », envoyé au nom de l'établissement (`BrevoMailer.SendOrderInvoice`, synchrone, renvoie le messageId).
- **Vérifié sur staging** (lecture seule, 2026-10-26) : la requête s'exécute sur le vrai schéma ; sur 90 jours, 535 commandes closes, 37 payées en ligne, 12 avec e-mail, 6 au-dessus de 25 €.

À trancher (cf. R8) : chaque envoi est une copie du ticket. L'envoi manuel depuis la caisse ne trace pas non plus ces copies ; à confirmer dans le cadre de la conformité caisse si un envoi par e-mail doit être enregistré comme duplicata.

## Pièces jointes

Inchangées : elles ne passent pas par les templates mais par les fonctions d'envoi, que la refonte ne touche pas. Fichier `.ics` des rendez-vous de démo (`SendAsyncWithAttachment`, `demorequest`), PDF du relevé et de la facture de commission (`SendPayoutDocuments`), de la facture de commission mensuelle et de son récapitulatif (`SendCommissionInvoice`), de la facture d'abonnement (`SendSubscriptionInvoice`), de la facture client (`SendInvoiceEmailToCustomer`), de l'attestation (`attestations.Email`). Les cartes « PDF » des mails ne font que signaler la pièce jointe.

## Écrire un nouveau mail

1. Créer `templates/<nom>.html` en partant d'un mail de la même famille (l'en-tête de chaque `partial_*.html` donne la séquence d'appels).
2. Ajouter ses données d'exemple dans `templateFixtures()` (`templates_render_test.go`) : `TestEveryTemplateHasAFixture` échoue sinon.
3. `go test ./internal/infrastructure/mailer/...` vérifie le rendu de chaque mail (pas de `<no value>`, de `%!` ni de `<nil>`) et que **tout nom de template cité dans le code Go existe** (`TestTemplatesReferencedInCodeExist`, le garde-fou qui a manqué aux relances).
4. Aperçu : `MAILER_PREVIEW_DIR=<dossier> go test ./internal/infrastructure/mailer/ -run TestEveryTemplateRenders` écrit chaque rendu en `.html` dans ce dossier.

Fonctions disponibles dans les templates (`templateFuncs`, `mailer/service.go`) : `dict` (argument des blocs : `dict "Label" "Voir" "URL" .Link`), `plural` (`plural 4 "personne" "personnes"` → « 4 personnes »), `supportEmail`.

Règles d'écriture : un seul bouton principal ; jamais d'affichage direct d'une clé de map qui peut manquer (passer par `if` ou `or`) ; pas de texte technique dans le mail ; ne promettre que ce que le code fait (ex. la rubrique « Abonnement & facturation » du back-office existe, `wello-back-office/src/pages/settings/SubscriptionManagement.tsx`).

## Identité de l'établissement

`mailer.MerchantBrand` (`Name`, `LogoURL`, `Color`, `TextColor`), construit par `mailer.NewMerchantBrand` qui normalise : couleur absente ou invalide → `#212529` (défaut de `merchant_parameters`) ; couleur de texte absente ou invalide → blanc ou `#1a1a1a` selon la clarté du fond ; logo non http(s) ignoré.

`merchantbrand.Repository.Resolve(ctx, merchantID, name)` lit `merchant` + `merchant_parameters`. Il ne renvoie jamais d'erreur (journalise et renvoie le cadre par défaut) pour ne jamais bloquer un envoi. Branché dans `cmd/api/routes.go` sur `bookingcomm`, `planningcomm` et le webhook Stripe (confirmation de commande) via `SetBrandResolver`.

## Autres corrections faites au passage

- **Planning** : le modèle lisait `{{.Title}}` sur `planningcomm.ShiftSummary`, qui n'a pas ce champ. Le rendu échouait dès qu'il y avait un créneau : seuls les plannings vides partaient.
- **Code de vérification** : les `**` autour de la durée s'affichaient tels quels.
- **Confirmation de commande** : la date était `CreationDate.String()` (« 2026-10-26 19:21:00 +0000 UTC »), maintenant `jj/mm/aaaa`.
- **Remboursement** : montant affiché « 15.50 EUR » au lieu de « 15.50 eur ».
- **Virement en cours** : objet « WR ScanNOrder - Virement en cours » remplacé par « Votre virement est en route ».
- Commentaires de `dunning` et `subscriptions` qui disaient ces modèles hébergés chez Brevo : ils sont rendus localement.

## Reste à faire

| # | Point | Détail |
|---|---|---|
| R1 | **Recette dans de vraies messageries** | Envoi test de chaque famille vers Gmail, Apple Mail, Outlook (ordinateur et mobile). Outlook ordinateur affiche les coins carrés. |
| R2 | **Remboursement au nom du restaurant** | `HandleChargeRefunded` ne connaît pas l'établissement : le mail part au nom de « Wello Resto », bandeau sans nom. Retrouver le `merchant_id` (paiements de l'intent ou métadonnées de la charge), puis `Brand` + nom d'expéditeur. |
| R3 | ~~Facture client aux couleurs~~ | **Fait (2026-10-26)** : l'envoi depuis la caisse passe par `sendOrderInvoiceMail` (identité de l'établissement). `SendInvoiceEmailToCustomer` ne sert plus que de repli si le mailer ne renvoie pas de messageId. |
| R4 | **Bouton dans les relances** | Les relances citent la rubrique « Abonnement & facturation » sans lien : les données n'ont pas d'URL de back-office. Ajouter `BackOfficeURL` (comme `PayoutDocumentsData`). |
| R5 | **Version texte** | Non générée ; Brevo n'envoie que le HTML. |
| R6 | **Campagnes marketing** | Le cadre Maison est la base prévue du « cadre unique » de `docs/campagnes-marketing.md` (P2) ; pied légal et désinscription à ajouter le moment venu. |
| R7 | `account_confirmation.html` | Restylé mais appelé nulle part. |
| R8 | ~~Duplicata~~ | **Tranché (Ilies, 2026-10-26) : pas de journal des remises ni de mention « duplicata ».** On s'en tient aux textes du gouvernement : le BOFiP (BOI-TVA-DECLA-30-10-30, version du 25/03/2026) impose inaltérabilité, sécurisation, conservation et archivage des données, sans exigence sur les copies remises au client ; la mention « duplicata » et le compteur de réimpressions relèvent de la certification NF525, qui n'est pas notre voie (attestation éditeur). Le mail de facture reste tracé dans `outbound_messages`. |
| R9 | ~~Avoir après remboursement~~ | **Écarté (Ilies, 2026-10-26)** : pas d'avoir par e-mail. |
| R10 | **Recette .ics** | Vérifier dans Apple Calendar, Google Agenda et Outlook que la modification déplace l'événement et que l'annulation le retire. |

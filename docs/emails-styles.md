# Styles des e-mails

Modèles d'e-mails de l'API (`internal/infrastructure/mailer/templates/`). Première refonte le 2026-10-26 (trois cadres Maison, Tableau de bord, Coffre) ; **seconde refonte le 2026-11-04 : direction « Blanc » (A)**, un seul système dans la charte de la vitrine, choisi par Ilies sur la page de propositions (artifact « E-mails épurés Wello Resto ») et vérifié sur les rendus réels (artifact « Rendus e-mails Wello Resto »). Ce document décrit le cadre, comment écrire un nouveau mail et ce qui reste à faire.

> Statut : **codé, non commité, non déployé.** Aucune migration. **Ordre de mise en ligne : déployer d'abord la vitrine** (`wello-resto-vitrine/public/email/` : logo W en PNG et police), sinon les mails partent avec une image cassée dans l'en-tête et le pied. À valider ensuite dans de vraies messageries (Gmail web et mobile, Apple Mail, Outlook).

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| D1 | Trois familles selon **qui envoie et qui lit** : clients d'un établissement, restaurateurs, équipe et sécurité. | Le client connaît le restaurant, pas Wello Resto ; le restaurateur attend des chiffres et des documents ; un mail de sécurité doit être reconnaissable. |
| D2 | Choix d'Ilies parmi 8 propositions : **Maison (C2) avec le bouton pleine largeur de Statut (C3)** pour les clients, **Tableau de bord (R2)** pour les restaurateurs, **Coffre (S1)** pour l'équipe et la sécurité. **Remplacé par D7 le 2026-11-04.** | Maquettes comparées sur les mêmes contenus. |
| D3 | Le cadre client prend la **couleur de marque de l'établissement** (`merchant_parameters.primary_color` / `text_color_on_primary_color`, déjà utilisées par ScanNOrder et la réservation) et son logo (`merchant.logo_url`). | Pas de nouveau réglage à faire saisir au restaurateur. |
| D4 | Blocs partagés (`partial_*.html`) plutôt qu'un HTML complet par mail. | Un seul endroit à corriger pour 26 mails ; impossible d'oublier le pied de page ou le pré-en-tête. |
| D5 | Mise en page en tableaux, styles en ligne, polices système (Helvetica/Arial, Georgia, Menlo), largeur 600 px, pas d'image hormis le logo. | Seule structure rendue partout, Outlook et Gmail compris (Gmail ignore les polices web). |
| D6 | Les cinq relances (`dunning_*`, `trial_expiry_reminder`) sont créées. | Elles n'existaient pas : le rendu échouait à l'envoi, l'erreur était journalisée puis ignorée. Aucune relance de paiement ni rappel de fin d'essai n'est jamais parti. |
| D7 | **Direction A « Blanc » (2026-11-04)** remplace les trois cadres : page blanche, colonne de 560 px, en-tête et titre centrés, détail dans un bloc gris bleuté, bouton en pilule, police Plus Jakarta Sans. | Ilies trouvait les cadres du 26 octobre « old school » et éloignés de l'esprit sobre de la vitrine (bandeaux pleins, Georgia italique, emoji, trois cadres sans air de famille). |
| D8 | La couleur ne sert qu'au logo et au bouton ; l'état passe par une **pastille** au-dessus du titre (vert : fait ; orange : action attendue ; rouge : suspension ; bleu : information Wello Resto ; gris : information). Jamais de rouge ni de bleu Wello Resto dans un mail client. | Remplace les bandeaux de couleur ; même règle d'urgence qu'avant (orange dès le premier échec de paiement, rouge quand une date de suspension est annoncée). |
| D9 | **Pas de carte « pièce jointe »** : le PDF est signalé dans une phrase. | Demande d'Ilies : la carte donnait envie de cliquer alors que ce n'est pas un lien. |
| D10 | Pied des mails clients : **« Propulsé par [W] Wello Resto »**, comme ScanNOrder (`PoweredBy.tsx`), lien vers `https://welloresto.fr/` avec `utm_source=email`, `utm_medium=powered_by`, `utm_campaign` = slug ScanNOrder du restaurant, `utm_content` = nom du mail. Remplace « Envoyé avec Wello Resto ». | Savoir quels restaurants et quels mails amènent des visites et des demandes de démo (la vitrine garde source, medium et campaign avec la demande). Sans risque : l'URL canonique de la vitrine exclut les paramètres (pas de doublon pour le référencement, et un lien d'e-mail n'est pas indexé) ; un lien vers le domaine expéditeur, sans raccourcisseur, ne pèse pas sur le filtrage anti-spam. Aucune donnée sur le client dans l'URL. |
| D11 | Logo : celui de l'établissement (56 px, coins arrondis) ; **sans logo, ses initiales** dans un carré à sa couleur (`MerchantBrand.Initials`). Mails Wello Resto : le vrai W de la vitrine en PNG (les messageries n'affichent pas le SVG). | Le bandeau « nom seul » des établissements sans logo disparaît avec les bandeaux. |

## Le cadre (direction A)

Fichiers : `partial_layout.html` (`doc_start` / `doc_end` : document, police, correctif Outlook, pré-en-tête masqué, colonne de 560 px), `partial_ui.html` (tous les blocs, mode d'emploi en tête du fichier), `partial_establishment.html` (ligne « Établissement » des mails restaurateurs). Les anciens `partial_client.html`, `partial_pro.html` et `partial_secure.html` sont supprimés.

| Famille | En-tête | Pied | Bouton | Mails |
|---|---|---|---|---|
| Clients d'un établissement | `head_merchant` : logo ou initiales, nom | `foot_merchant` : nom, liens légaux s'ils sont connus, « Propulsé par Wello Resto » | couleur de l'établissement | réservations (6), liste d'attente, commande (`scannorder_payout.html`), remboursement, facture client |
| Restaurateurs | `head_wello` : W bleu, « Wello Resto » | `foot_wello` : support | bleu `#0036d9` | virement, relevé, commission, abonnement, attestation, relances (4), fin d'essai, démo (2) |
| Sécurité et équipe | `head_wello` (planning : `head_merchant`) | `foot_secure` (planning : `foot_merchant`) | bleu (planning : couleur de l'établissement) | code de vérification, mot de passe, confirmation de compte, planning |

- **Blocs** : `hero` (pastille, titre, sous-titre), `figure` (chiffre clé centré : montant, date de suspension), `facts_start` / `fact` / `facts_end` (résumé ; la première ligne porte `"First" true`), `p_open` / `small_open` / `p_close` (paragraphes), `button`, `calendar`, `code`, `note`, `notice` (envoi de test), `sign`.
- **Police** : `@font-face` vers `https://welloresto.fr/email/fonts/` (Plus Jakarta Sans 400, 700, 800, licence OFL, copiées de `@fontsource`), lue par Apple Mail et iOS ; Gmail l'ignore et prend la police système. Pas de Google Fonts (choix de la vitrine). **Outlook pour ordinateur** affiche en Times une police absente placée en tête : `msoFontFix` (fonction Go, car `html/template` supprime les commentaires écrits dans le modèle) lui impose Arial.
- **Images** : `https://welloresto.fr/email/wello-resto-tile.png` (W blanc sur carré bleu, 120 px affiché en 40) et `wello-resto-mark-grey.png` (W gris, 72 × 48 affiché en 21 × 14), générées depuis `public/logos/logo-wello-resto-white.svg` de la vitrine. Constantes `mailer.WelloTileURL` / `WelloMarkURL`.
- **Code de vérification** : inchangé sur le fond (code dans l'objet et le pré-en-tête, groupé par 3 sans espace copiée, `user-select: all`), présenté dans un bloc gris centré.
- **Relances** : la date de suspension est le chiffre clé (`figure`) ; l'ancien encadré rouge, redondant, est supprimé.

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
- **Mail** : `invoice_email.html` (cadre client aux couleurs de l'établissement), objet « Votre facture <établissement> — commande n° <numéro> », envoyé au nom de l'établissement (`BrevoMailer.SendOrderInvoice`, synchrone, renvoie le messageId).
- **Vérifié sur staging** (lecture seule, 2026-10-26) : la requête s'exécute sur le vrai schéma ; sur 90 jours, 535 commandes closes, 37 payées en ligne, 12 avec e-mail, 6 au-dessus de 25 €.

À trancher (cf. R8) : chaque envoi est une copie du ticket. L'envoi manuel depuis la caisse ne trace pas non plus ces copies ; à confirmer dans le cadre de la conformité caisse si un envoi par e-mail doit être enregistré comme duplicata.

## Pièces jointes

Inchangées : elles ne passent pas par les templates mais par les fonctions d'envoi, que la refonte ne touche pas. Fichier `.ics` des rendez-vous de démo (`SendAsyncWithAttachment`, `demorequest`), PDF du relevé et de la facture de commission (`SendPayoutDocuments`), de la facture de commission mensuelle et de son récapitulatif (`SendCommissionInvoice`), de la facture d'abonnement (`SendSubscriptionInvoice`), de la facture client (`SendInvoiceEmailToCustomer`), de l'attestation (`attestations.Email`). Les cartes « PDF » des mails ne font que signaler la pièce jointe.

## Écrire un nouveau mail

1. Créer `templates/<nom>.html` en partant d'un mail de la même famille (l'en-tête de `partial_ui.html` donne la séquence d'appels). Un mail client passe son nom à `foot_merchant` (`"Mail" "<nom>"`, sans `.html`) : c'est l'`utm_content` du lien vers la vitrine.
2. Ajouter ses données d'exemple dans `templateFixtures()` (`templates_render_test.go`) : `TestEveryTemplateHasAFixture` échoue sinon.
3. `go test ./internal/infrastructure/mailer/...` vérifie le rendu de chaque mail (pas de `<no value>`, de `%!` ni de `<nil>`) et que **tout nom de template cité dans le code Go existe** (`TestTemplatesReferencedInCodeExist`, le garde-fou qui a manqué aux relances).
4. Aperçu : `MAILER_PREVIEW_DIR=<dossier> go test ./internal/infrastructure/mailer/ -run TestEveryTemplateRenders` écrit chaque rendu en `.html` dans ce dossier.

Fonctions disponibles dans les templates (`templateFuncs`, `mailer/service.go`) : `dict` (argument des blocs : `dict "Label" "Voir" "URL" .Link`), `plural` (`plural 4 "personne" "personnes"` → « 4 personnes »), `supportEmail`, `poweredByURL`, `welloTileURL`, `welloMarkURL`, `msoFontFix`. Montants : `mailer.FormatEuros(centimes)` → « 1 284,60 € » (espaces insécables).

Règles d'écriture : un seul bouton principal ; jamais d'affichage direct d'une clé de map qui peut manquer (passer par `if` ou `or`) ; pas de texte technique dans le mail ; ne promettre que ce que le code fait (ex. la rubrique « Abonnement & facturation » du back-office existe, `wello-back-office/src/pages/settings/SubscriptionManagement.tsx`).

## Identité de l'établissement

`mailer.MerchantBrand` (`Name`, `LogoURL`, `Initials`, `Color`, `TextColor`, `Code`), construit par `mailer.NewMerchantBrand` qui normalise : couleur absente ou invalide → `#212529` (défaut de `merchant_parameters`) ; couleur de texte absente ou invalide → blanc ou `#1a1a1a` selon la clarté du fond ; logo non http(s) ignoré ; initiales = première lettre ou chiffre des deux premiers mots (« Croq'O Pizzas » → « CP »). Le logo est supposé à peu près carré, affiché en 56 px de large. `Code` (slug du QR principal ScanNOrder, même règle que `scannorder.snoMainQRCondition`) est posé par `merchantbrand.Repository.Resolve` ; vide ailleurs, le lien « Propulsé par » part alors sans `utm_campaign`.

`merchantbrand.Repository.Resolve(ctx, merchantID, name)` lit `merchant` + `merchant_parameters`. Il ne renvoie jamais d'erreur (journalise et renvoie le cadre par défaut) pour ne jamais bloquer un envoi. Branché dans `cmd/api/routes.go` sur `bookingcomm`, `planningcomm` et le webhook Stripe (confirmation de commande) via `SetBrandResolver`.

## Autres corrections faites au passage

- **Planning** : le modèle lisait `{{.Title}}` sur `planningcomm.ShiftSummary`, qui n'a pas ce champ. Le rendu échouait dès qu'il y avait un créneau : seuls les plannings vides partaient.
- **Code de vérification** : les `**` autour de la durée s'affichaient tels quels.
- **Confirmation de commande** : la date était `CreationDate.String()` (« 2026-10-26 19:21:00 +0000 UTC »), maintenant `jj/mm/aaaa`.
- **Remboursement** : montant affiché « 15.50 EUR » au lieu de « 15.50 eur ».
- **Virement en cours** : objet « WR ScanNOrder - Virement en cours » remplacé par « Votre virement est en route ».
- Commentaires de `dunning` et `subscriptions` qui disaient ces modèles hébergés chez Brevo : ils sont rendus localement.

## Corrections du 2026-11-04

- **Confirmation de commande** : le montant s'affichait « 33.00EUR » (devise collée par le webhook, `MerchantCurrency` jamais renseigné) ; il passe au format français via `FormatEuros`. Le **numéro de retrait** (`orders.order_num`, déjà envoyé par SMS) est ajouté (`ScanNOrderConfirmationData.OrderNumber`).
- **Remboursement** : le montant affiché était celui du paiement (`charge.Amount`), faux pour un remboursement partiel ; c'est maintenant le cumul remboursé (`charge.AmountRefunded`), au format français (`refundAmountLabel`, webhook Stripe).
- **PDF de facture client** (`invoice_pdf.go`) : la rubrique « Encaissements » imprimait le code brut du moyen de paiement ; un paiement Stripe s'imprime désormais « En ligne » (`invoicePaymentLabel`, à l'impression seulement : le ticket figé `payments_snapshot` n'est pas modifié). Les autres codes (`CB`, `ES`, `TR`, `KIOSK`…) restent imprimés tels quels.
- **Liens légaux** de la confirmation de commande déplacés dans le pied. Le webhook Stripe ne renseigne ni `PrivacyURL` ni `TermsURL` : ils ne s'affichent pas encore (R12).

## Reste à faire

| # | Point | Détail |
|---|---|---|
| R1 | **Recette dans de vraies messageries** | Envoi test de chaque famille vers Gmail, Apple Mail, Outlook (ordinateur et mobile), après déploiement de la vitrine (R11). Outlook ordinateur affiche les coins carrés et Arial. Vérifier aussi un logo d'établissement très allongé. |
| R2 | **Remboursement au nom du restaurant** | `HandleChargeRefunded` ne connaît pas l'établissement : le mail part au nom de « Wello Resto », bandeau sans nom. Retrouver le `merchant_id` (paiements de l'intent ou métadonnées de la charge), puis `Brand` + nom d'expéditeur. |
| R3 | ~~Facture client aux couleurs~~ | **Fait (2026-10-26)** : l'envoi depuis la caisse passe par `sendOrderInvoiceMail` (identité de l'établissement). `SendInvoiceEmailToCustomer` ne sert plus que de repli si le mailer ne renvoie pas de messageId. |
| R4 | **Bouton dans les relances** | Les relances citent la rubrique « Abonnement & facturation » sans lien : les données n'ont pas d'URL de back-office. Ajouter `BackOfficeURL` (comme `PayoutDocumentsData`). |
| R5 | **Version texte** | Non générée ; Brevo n'envoie que le HTML. |
| R6 | **Campagnes marketing** | Le cadre client (direction A) est la base prévue du « cadre unique » de `docs/campagnes-marketing.md` (P2) ; pied légal et désinscription à ajouter le moment venu. |
| R7 | `account_confirmation.html` | Restylé mais appelé nulle part. |
| R8 | ~~Duplicata~~ | **Tranché (Ilies, 2026-10-26) : pas de journal des remises ni de mention « duplicata ».** On s'en tient aux textes du gouvernement : le BOFiP (BOI-TVA-DECLA-30-10-30, version du 25/03/2026) impose inaltérabilité, sécurisation, conservation et archivage des données, sans exigence sur les copies remises au client ; la mention « duplicata » et le compteur de réimpressions relèvent de la certification NF525, qui n'est pas notre voie (attestation éditeur). Le mail de facture reste tracé dans `outbound_messages`. |
| R9 | ~~Avoir après remboursement~~ | **Écarté (Ilies, 2026-10-26)** : pas d'avoir par e-mail. |
| R11 | **Déployer la vitrine avant l'API** | `wello-resto-vitrine/public/email/` (2 PNG, 3 polices) doit être en ligne sur welloresto.fr avant les nouveaux mails. |
| R12 | **Liens légaux de la commande** | Renseigner `PrivacyURL` / `TermsURL` dans le webhook Stripe si l'établissement en a (rien n'est lu aujourd'hui). |
| R10 | **Recette .ics** | Vérifier dans Apple Calendar, Google Agenda et Outlook que la modification déplace l'événement et que l'annulation le retire. |

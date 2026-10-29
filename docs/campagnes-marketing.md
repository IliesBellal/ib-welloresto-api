# Campagnes marketing (SMS / e-mail)

Envoi groupé de SMS et d'e-mails par un établissement à ses clients (promotion, code promo, nouveauté, événement), depuis un nouvel item sous **Clients** au back-office. Document de référence pour les trois dépôts (API, back-office, ScanNOrder). Discussion avec Ilies démarrée le 2026-10-24.

> Statut : **conception en cours, rien n'est codé.** Les décisions validées sont actées ; dans le tableau des propositions, ✅ = validée, ⏳ = en attente de réponse.

## Décisions validées

| # | Décision | Pourquoi |
|---|---|---|
| D1 | Nouvel item sous le menu **Clients** du back-office (après « Liste clients » et « Fidélité »). | Les campagnes s'adressent à la base clients de l'établissement. |
| D2 | **Permission dédiée** `marketing.manage` (créer, programmer et envoyer une campagne), distincte de `customers.manage`. Nom aligné sur les permissions existantes (`customers.manage`, `cds.manage`, `bookings.manage`). | Un envoi coûte de l'argent et engage l'image de l'établissement : tout le personnel qui gère les fiches ne doit pas pouvoir envoyer. |
| D3 | Quatre **modes d'envoi** par campagne : **SMS seul**, **e-mail seul**, **SMS sinon e-mail**, **e-mail sinon SMS** (règles §3). | Joindre le maximum de clients au coût choisi par le restaurateur. |
| D4 | **Estimation du coût** affichée avant l'envoi, puis confirmation explicite du montant (§4). | Pas de mauvaise surprise sur la facture. |
| D5 | On documente avant de coder. | Décision Ilies. |

## Propositions

| # | Proposition | Pourquoi | Statut |
|---|---|---|---|
| P1 | Les **comptes clients ScanNOrder ne sont pas un prérequis**. Seule la phase « 0 bis Marketing » de `wello-resto-scannorder/docs/comptes-client.md` (consentement) l'est. Les comptes viendront ensuite consolider le consentement (validé une fois, plus de risque de l'écraser). | Voir §1. | ✅ 2026-10-25 |
| P2 | **E-mail : un cadre unique verrouillé** par WelloResto, habillé aux couleurs de l'établissement, et **3-4 modèles de contenu** à champs fixes. Pas d'éditeur libre, pas de HTML importé. | Voir §5. | ✅ 2026-10-25 |
| P3 | **SMS : texte libre** avec variables, nom de l'établissement en expéditeur, mention STOP, **normalisation** des caractères qui doublent le coût. Brevo ne propose pas de devis avant envoi : l'estimation est calculée par WelloResto (§4, §6.1). | Voir §6. | ✅ 2026-10-25 |
| P4 | Le repli d'un canal à l'autre se décide **sur la joignabilité au moment de l'envoi**, pas sur un échec de distribution. | Simple, prévisible et estimable à l'avance. Le repli sur échec (asynchrone) est repoussé. | ✅ 2026-10-25 |
| P5 | **Un seul message par personne et par campagne**, doublons fusionnés (même téléphone ou même e-mail sur plusieurs fiches de l'établissement). | Les doublons de fiches existent ; deux SMS identiques font fuir. | ✅ 2026-10-25 |
| P6 | **Plafond de fréquence** : un client reçoit **au plus une campagne par établissement tous les 7 jours** (glissants), soit environ 4 par mois sans pouvoir les concentrer sur une semaine. Les clients sous le plafond sont exclus et comptés à part dans l'estimation. | Protège la réputation d'envoi et le taux de désinscription. | ✅ principe ; ⏳ forme de la règle (7 jours glissants plutôt que « 4 sur 30 jours ») |
| P7 | **Mesure du résultat** d'une campagne : commandes avec le code de la campagne (lien certain) et commandes des destinataires dans les 7 jours (indicatif). Détail §8. | C'est l'argument commercial : WelloResto possède les commandes, un outil d'e-mailing générique non. | ⏳ |
| P8 | L'établissement est **responsable de traitement** de ses envois marketing, WelloResto **sous-traitant** (art. 28 RGPD), clause à ajouter aux CGV. | Cohérent avec `comptes-client.md` §9.2 : la base légale vaut par restaurant, jamais pour WelloResto. | ✅ 2026-10-25 |
| P9 | Nom de l'item : **« Campagnes »**. | Court, explicite ; « Marketing » est déjà employé pour les catégories, le ruban borne et l'écran client. | ✅ 2026-10-25 |
| P10 | **Page de désinscription publique, sans connexion**, sur le domaine **ScanNOrder** hors slug (`/desinscription/{jeton}`), pas sur le back-office ni sur welloresto.fr. Détail §10. | Validé : une page publique sans connexion. Proposé : son emplacement. | ✅ page publique ; ⏳ emplacement |
| P11 | **Sous-compte Brevo dédié aux SMS marketing**, séparé des SMS de service (OTP, suivi, confirmations). | Un STOP met le numéro sur la liste noire **du compte Brevo entier** (§6.1). | ⏳ |

## 1. Comptes clients d'abord ? (P1)

Non, pour trois raisons :

- **Les campagnes ciblent les fiches `customer` de l'établissement**, et le consentement marketing est **par établissement**, porté par la fiche (`comptes-client.md` §8 : « jamais global »). Le compte ScanNOrder est une identité cross-merchant dont WelloResto est responsable de traitement : il n'intervient pas dans le ciblage.
- **Ce dont les campagnes ont besoin est déjà séquencé avant les comptes** : la phase « 0 bis Marketing » du plan des comptes (correction serveur, contrat `marketing_opt_out`, case de refus, opt-in sur le suivi) précède la phase « 1 Socle ».
- **Les comptes sont un gros chantier** (domaine `api.welloresto.fr`, OAuth Google, Apple Developer, CGU ScanNOrder) : les attendre repousserait les campagnes de plusieurs mois sans rien débloquer.

**La mémoire locale de ScanNOrder ne suffit pas à protéger le consentement.** ScanNOrder renvoie bien le dernier choix du client (mémorisé par établissement sur l'appareil), mais une commande passée **en caisse ou en borne** arrive sans consentement et le serveur remet la fiche à `false` (bug §2 point 1). La correction serveur de la phase 0 reste donc indispensable, comptes ou pas.

Ce que les comptes apporteront plus tard :

- **Un consentement validé une seule fois**, attaché au compte, qui ne dépend plus de l'appareil.
- **Plus d'e-mails exploitables.** Aujourd'hui l'e-mail d'une fiche ne vient que du paiement Stripe en ligne ; un client connecté le transmettra à chaque commande. Conformément à D4 des comptes, la fiche ne reçoit l'e-mail que s'il a été transmis lors d'une commande chez cet établissement.
- La **page de préférences par restaurant** (D7 des comptes) : désinscription et réinscription en libre-service.
- Moins de doublons de fiches (rattachement sur téléphone vérifié).

Conséquence : au lancement, **le SMS joindra beaucoup plus de clients que l'e-mail**. Les modes « SMS sinon e-mail » / « e-mail sinon SMS » compensent en partie.

## 2. État des lieux (2026-10-24)

Déjà là :

- **Envoi Brevo** SMS (`internal/infrastructure/brevo_sms`) et e-mail (`internal/infrastructure/brevo_mailer`).
- **`merchantsms`** : envoi pour le compte d'un établissement, nom d'expéditeur propre (`merchant_marketing_settings.sms_sender_name`, 11 caractères max), comptage du coût par établissement et par mois (`merchant_sms_monthly`, prix unitaire `sms_unit_price`, 7 c par défaut).
- **Codes promo panier** : `discounts.discount_code`, `max_redemptions_per_customer`.
- **Données de segmentation** sur la fiche : nombre de commandes, total dépensé, dernière commande (tâche `internal/tasks/customer_stats.go`).
- **Tâches de fond** (`internal/tasks/manager.go`) pour les envois programmés.
- **E-mail de l'établissement** dans les paramètres (back-office), utilisable en `Reply-To`.
- **Conception du consentement** : `wello-resto-scannorder/docs/comptes-client.md` §9.

Manquant ou à corriger :

1. **Bug : le consentement est effacé à chaque commande.** `order_life_cycle/repository.go:1616` remet `advertising_consent` à `false` quand la commande n'envoie pas la valeur (tous canaux). Déjà relevé dans `comptes-client.md` §9.1, toujours présent.
2. **`customer.advertising_consent` a `DEFAULT true`** : des fiches anciennes ou importées peuvent être « contactables » sans consentement réel. Avec le bug 1, la donnée actuelle n'est fiable dans aucun sens.
3. **Modèle de consentement de `comptes-client.md` §9.4-9.6 non implémenté** : `marketing_basis`, `marketing_opted_out_at`, statut **par canal**, historique `customer_marketing_events`.
4. **Aucune désinscription** : ni traitement des STOP SMS (webhook Brevo), ni lien / page de désinscription e-mail.
5. **SMS envoyés en `"type": "transactional"`**, valeur figée dans `brevo_sms/service.go:103`. Un SMS promotionnel doit partir en `marketing`.
6. **SMS comptés à l'unité, pas au segment** : `merchantsms.recordSent` enregistre 1 SMS par message alors que Brevo facture chaque segment (§4). Sous-facturation des SMS longs, y compris pour le suivi de livraison actuel. Brevo renvoie pourtant `smsCount` et `usedCredits` à l'envoi (§6.1) ; `brevo_sms` ne lit que `messageId`.
7. **E-mails transactionnels et marketing sur la même identité d'envoi** : une campagne signalée comme spam dégraderait les e-mails critiques (mot de passe, reçus, virements).
8. **Clients Uber Eats / Deliveroo** non exclus (coordonnées relais, démarchage interdit), cf. `comptes-client.md` §9.6.
9. Pas de tables de campagnes, pas d'envoi en lots, pas de gestion des rebonds ni des plaintes.

## 3. Modes d'envoi (D3, P4, P5)

**Joignable par SMS** : consentement SMS actif, numéro mobile valide (E.164, mobile), fiche `customer_brand = 'WELLO_RESTO'`, pas de numéro marqué invalide.
**Joignable par e-mail** : consentement e-mail actif, e-mail valide, pas de rebond définitif ni de plainte, même règle de marque.

| Mode | Client joignable SMS et e-mail | SMS seulement | E-mail seulement | Aucun |
|---|---|---|---|---|
| SMS seul | SMS | SMS | exclu | exclu |
| E-mail seul | e-mail | exclu | e-mail | exclu |
| SMS sinon e-mail | SMS | SMS | e-mail | exclu |
| E-mail sinon SMS | e-mail | SMS | e-mail | exclu |

- Le restaurateur rédige **une version par canal utilisé**. Proposition : le SMS est pré-rempli à partir du titre et du code de l'e-mail, modifiable.
- La répartition est figée au lancement de l'envoi. Les consentements retirés entre la programmation et l'envoi sont respectés (recalcul à l'envoi).

## 4. Estimation du coût (D4)

L'écran affiche, avant confirmation :

- clients du segment ; dont **SMS** n ; **e-mails** m ;
- **exclus**, par motif : refus, pas de mobile, pas d'e-mail, plateforme, plafond de fréquence (P6), doublons fusionnés ;
- **coût total** = coût SMS + coût e-mail.

**SMS**, calculé côté serveur, destinataire par destinataire (les variables comme `{prenom}` changent la longueur) :

| Encodage | 1 segment | Par segment au-delà |
|---|---|---|
| GSM-7 (alphabet de base) | 160 caractères | 153 |
| Unicode (dès un seul caractère hors GSM-7) | 70 | 67 |

- Coût = segments × `sms_unit_price` de l'établissement. La **mention STOP** ajoutée compte dans la longueur (texte exact à vérifier avec Brevo).
- Pièges en français : `ê â î ô û œ`, l'apostrophe typographique `’`, les guillemets `« »` et les emoji font passer le message en Unicode et **multiplient le coût par 2 à 3**. Proposition : remplacer automatiquement `’ « »` par `' "`, signaler les autres caractères (« ce caractère double le coût ») dans l'aperçu.
- L'éditeur affiche en direct : caractères, encodage, segments par SMS.
- Le comptage réel (`merchant_sms_monthly`) doit passer **au segment**, à partir du `smsCount` renvoyé par Brevo (§2 point 6, §6.1). Le rapport compare estimation et réel.

**E-mail** : modèle de facturation à décider (question ouverte Q2).

**Confirmation** : fenêtre récapitulative avec le montant, bouton « Envoyer pour XX,XX € ». Plafond de dépense mensuel par établissement (Q3).

## 5. E-mail : cadre et modèles (P2)

**La délivrabilité dépend d'abord de l'infrastructure et de la qualité de la liste, pas du modèle.** Ce qui compte, par ordre d'importance :

1. **Identité d'envoi séparée** du transactionnel : sous-domaine dédié (proposition `m.welloresto.fr`), SPF, DKIM, DMARC alignés ; idéalement un sous-compte Brevo distinct.
2. **Liste propre** : uniquement des clients ayant consenti ou relevant de l'exception « client existant », rebonds définitifs et plaintes retirés automatiquement (webhooks Brevo).
3. **Désinscription en un clic** : en-têtes `List-Unsubscribe` et `List-Unsubscribe-Post` (RFC 8058), exigés par Gmail et Yahoo pour les envois en masse, plus un lien visible dans le pied.
4. **Taux de plainte bas** : plafond de fréquence (P6), contenu attendu par le client.

Le modèle joue à la marge, mais **le verrouiller supprime les causes de spam que le restaurateur ne maîtrise pas** : image unique sans texte, HTML cassé, liens raccourcis ou vers des domaines douteux, pied de page légal absent, rendu mobile cassé. D'où :

- **Un cadre unique** maintenu par WelloResto : en-tête avec logo et couleur de l'établissement (contraste contrôlé), pied verrouillé (nom et adresse de l'établissement, raison de la réception, désinscription, politique de confidentialité). Version texte générée automatiquement.
- **3-4 modèles de contenu** à champs fixes : **Code promo**, **Nouveauté / plat du moment**, **Événement**, **Message simple**.
- **Champs** : objet, pré-en-tête, titre, une image facultative (hébergée sur R2, redimensionnée), texte avec mise en forme minimale (gras, retours à la ligne), un bouton dont la destination est choisie dans une liste (commander sur ScanNOrder, réserver, site de l'établissement), bloc code promo généré depuis la remise choisie (code, validité, conditions).
- **Expéditeur** : « Nom de l'établissement » `<adresse@m.welloresto.fr>`, `Reply-To` = e-mail de l'établissement.
- Aperçu ordinateur et mobile, **envoi test** à l'utilisateur connecté.

## 6. SMS (P3)

- Texte libre, variables `{prenom}`, `{code}`, `{lien}` (lien court ScanNOrder de l'établissement).
- Expéditeur = `sms_sender_name` de l'établissement (11 caractères), défaut « Wello Resto » à éviter pour du marketing : proposition de rendre le nom d'expéditeur obligatoire pour envoyer une campagne.
- Envoi en `type: marketing`, mention STOP ajoutée, STOP traité par webhook (coupe le SMS seulement, `comptes-client.md` §9.6).
- **Plages d'envoi** (règle des opérateurs pour le SMS commercial) : du lundi au samedi, 8h-20h, hors jours fériés, à l'heure de l'établissement. « Envoyer maintenant » en dehors de ces plages propose le prochain créneau.

### 6.1 Ce que Brevo fournit (vérifié le 2026-10-25 dans la documentation API)

| Besoin | Brevo | Conséquence |
|---|---|---|
| Devis avant envoi | **Aucun endpoint d'estimation.** | Estimation calculée par WelloResto : encodage, segments, mention STOP, prix unitaire (§4). |
| Coût réel d'un SMS | Réponse de l'envoi (`POST /v3/transactionalSMS/sms`) : `messageId`, `reference`, **`smsCount`** (segments), **`usedCredits`**, **`remainingCredits`**. | Enregistrer `smsCount` et `usedCredits` par destinataire ; c'est la base du comptage et de la facturation. Le code actuel appelle `/v3/transactionalSMS/send` : vérifier que c'est un alias de l'endpoint documenté. |
| Type marketing | Champ `type` : `transactional` (défaut) ou `marketing`. Un contenu contenant un code STOP est traité comme marketing. | Toujours envoyer les campagnes en `marketing`. |
| Événements | **Webhook** par message (champ `webUrl` de l'envoi) ou webhook de compte (canal `sms`). Événements consultables aussi par API (`GET /v3/transactionalSMS/statistics/events`) : `sent`, `accepted`, `delivered`, `softBounces`, `hardBounces`, `blocked`, `rejected`, `skipped`, **`unsubscription`**, `replies` (avec le texte de la réponse). **Aucun champ de coût dans les événements.** | Webhook pour les statuts de distribution et les STOP ; le coût vient de la réponse à l'envoi. |
| STOP | Mention obligatoire en France. Sur STOP, Brevo **désinscrit et met le numéro sur liste noire** pour les envois suivants. | Voir ci-dessous. |
| Unicode | Champ `unicodeEnabled` (défaut `false`). | À positionner selon l'encodage détecté, sinon les accents hors GSM-7 risquent d'être mal affichés. |

**Point d'attention STOP** : la liste noire Brevo est **celle du compte**, et tous les établissements envoient depuis le même compte. Un client qui répond STOP à une campagne du restaurant A serait bloqué pour les campagnes de tous les autres restaurants, et potentiellement pour les SMS de service (suivi de livraison, confirmation). D'où :

- **Sous-compte Brevo dédié aux SMS marketing** (P11), pour que les SMS de service ne soient jamais bloqués.
- **Désinscription par établissement côté WelloResto** : à réception d'un `unsubscription`, retrouver le dernier SMS marketing envoyé à ce numéro, donc l'établissement, et retirer le consentement SMS de la fiche de cet établissement uniquement.
- **Reste à vérifier sur staging** : la mention STOP est-elle ajoutée automatiquement par l'API ou faut-il l'écrire (et avec quel numéro court) ? Peut-on retirer un numéro de la liste noire par API quand un client se réinscrit chez un autre restaurant ? Sinon, un STOP chez A bloque de fait les campagnes SMS de B.

## 7. Audience

Segments prédéfinis au lancement :

- Tous les clients joignables.
- Nouveaux clients (1 commande, il y a moins de 30 jours).
- Habitués (au moins N commandes sur 90 jours).
- Inactifs (dernière commande il y a plus de 30 / 60 / 90 jours).
- Meilleurs clients (20 % du total dépensé le plus élevé).

Plus tard : anniversaire (`customer_birthdate` peu renseigné aujourd'hui), mode de commande habituel, filtres combinables.

## 8. Rapport (P7)

**Envoi** : destinataires par canal, délivrés, rebonds, STOP / désinscriptions, plaintes, clics (phase 2, redirection par le domaine d'envoi), coût estimé et coût réel.

**Résultat** : la question du restaurateur est « qu'est-ce que la campagne m'a rapporté ? ». Deux chiffres, affichés séparément :

| Mesure | Calcul | Fiabilité |
|---|---|---|
| **Commandes avec le code** | Commandes où le code promo de la campagne a été appliqué, et leur CA. | Certaine : sans la campagne, le client n'avait pas le code. |
| **Commandes des destinataires sous 7 jours** | Commandes passées par les destinataires (même fiche client) dans les 7 jours suivant l'envoi, avec ou sans code. | Indicative : certains seraient venus de toute façon. |

Exemple : campagne SMS envoyée mardi à 800 clients avec le code `PIZZA20`. Rapport : « 38 commandes avec le code, 1 240 € » et « 112 destinataires ont commandé dans les 7 jours, 3 100 € ».

Plus tard (phase 3) : **groupe témoin**. On retient 5-10 % du segment qui ne reçoit rien et on compare son taux de commande à celui des destinataires. La différence, c'est l'effet réel de la campagne.

## 9. Plan

| Phase | Contenu |
|---|---|
| **0. Prérequis** | Correction du bug de consentement (§2.1), puis modèle de consentement par canal + historique (`comptes-client.md` §9.4-9.6), audit et migration des fiches existantes (`DEFAULT true`), exclusion des plateformes, désinscription (webhooks STOP et e-mail, page publique §10, désinscription en un clic), sous-compte Brevo SMS marketing (P11) et tests STOP sur staging (§6.1), SMS `marketing`, comptage au segment (`smsCount`), identité d'envoi e-mail dédiée, permission `marketing.manage`, validation juridique. |
| **1. Campagnes** | Item « Campagnes » : liste, création (mode, audience, contenus, code promo), estimation du coût, aperçu, envoi test, envoi immédiat ou programmé, rapport de base. |
| **2. Automatisations** | Relance des inactifs, anniversaire, après la 1ʳᵉ commande, demande d'avis ; codes uniques par client ; suivi des clics. |
| **3. Finitions** | Tests A/B, page de préférences ScanNOrder (D7 des comptes), repli sur échec de distribution. |

## 10. Désinscription (P10)

**Validé** : une page publique, sans connexion.

**Proposé : domaine ScanNOrder, hors slug** (`/desinscription/{jeton}`, à côté du futur `/compte`).

- C'est l'application **côté client final**, déjà aux couleurs des établissements, et elle hébergera la page de préférences des comptes (D7 de `comptes-client.md`) : même endroit pour se désinscrire aujourd'hui et gérer ses préférences demain.
- Hors slug, la page **ne dépend pas de l'activation de ScanNOrder** : elle fonctionne pour un établissement qui n'utilise que la caisse.
- **Pas le back-office** : c'est l'application du personnel. Y ouvrir des pages publiques mélange deux publics et élargit la surface d'attaque d'une application authentifiée.
- **Pas welloresto.fr** : c'est le site commercial de WelloResto pour les restaurateurs. Le client final ne connaît pas WelloResto, il connaît le restaurant.

Fonctionnement :

- **Jeton signé** (HMAC) portant fiche client, établissement, canal et campagne : aucune donnée personnelle dans l'URL, aucune connexion. Pas d'expiration : un lien de désinscription doit fonctionner durablement.
- La page affiche « Vous ne recevrez plus les offres de {restaurant} par e-mail », propose de se désinscrire aussi des SMS, et un bouton « Annuler » en cas d'erreur.
- **Désinscription en un clic** des messageries (en-tête `List-Unsubscribe-Post`) : appel direct à l'API, sans passer par la page.
- **SMS** : pas de page, le STOP est traité par le webhook Brevo (§6.1).
- Chaque désinscription écrit un événement `OPT_OUT` dans `customer_marketing_events` (source `UNSUBSCRIBE_LINK`, `ONE_CLICK` ou `SMS_STOP`).

## Questions ouvertes

| # | Question |
|---|---|
| ~~Q1~~ | ~~Nom de l'item~~ : « Campagnes » (P9, validé le 2026-10-25). |
| Q2 | Facturation de l'e-mail : inclus dans l'abonnement avec un quota mensuel, puis payant au-delà ? Ou toujours à l'unité ? |
| Q3 | Facturation des SMS : sur la facture mensuelle (comme aujourd'hui) ou crédits prépayés ? Plafond de dépense mensuel par défaut ? `merchant_sms_monthly` est-il réellement refacturé aujourd'hui ? |
| Q4 | Forme du plafond de fréquence (P6) : 1 tous les 7 jours glissants (proposé) ou 4 sur 30 jours ? Les automatisations de la phase 2 (anniversaire, relance) comptent-elles dans le plafond ? |
| Q8 | Brevo, à tester sur staging : mention STOP ajoutée automatiquement ou à écrire ? Retrait d'un numéro de la liste noire par API ? (§6.1) |
| Q5 | Technique e-mail : API transactionnelle Brevo en lots sur un sous-compte dédié, ou campagnes Brevo (synchronisation des contacts) ? Préférence : lots via API, la liste reste chez nous. |
| Q6 | Fiches importées ou saisies au back-office (base `IMPORT` / `MANUAL`) : envoi autorisé sur simple déclaration du restaurateur, avec case « j'atteste avoir recueilli le consentement » à l'import ? |
| Q7 | Validation juridique : libellés de collecte, pied des e-mails, clause sous-traitant des CGV. |

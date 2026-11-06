# Connexion Uber Eats et Deliveroo depuis le back-office

Chantier du 2026-11-05/06. Objectif : le restaurateur relie lui-même son restaurant Uber Eats et son site Deliveroo à Wello Resto depuis le back-office (pages `/integrations/uber-eats` et `/integrations/deliveroo`), sans intervention en base.

Aucune migration. Code non commité à la livraison.

## 1. État de départ (analyse du 2026-11-05)

- **Uber Eats** : un début de flux OAuth existait (`/integrations/uber-eats/connect`, `/callback`, `/disconnect`), inutilisable en l'état :
  - `/connect` et `/disconnect` hors du middleware d'auth, mais lisant `GetUser(r).MerchantID` (nil → panic) ;
  - `redirect_uri` codée en dur sur une route inexistante (`/api/integrations/uber/callback`) ;
  - retour vers `app.welloresto.com/settings/integrations`, page non implémentée dans le back-office ;
  - `state` = `merchant_id` en clair : n'importe qui pouvait rattacher son restaurant Uber à n'importe quel établissement ;
  - mauvais scope (`eats.order eats.report eats.store` au lieu de `eats.pos_provisioning`) ;
  - pas d'appel `POST pos_data` : l'intégration n'aurait jamais été activée chez Uber ;
  - premier restaurant pris d'office, sans pagination ;
  - `INSERT` voué à l'échec (`delay_duration`, `auto_accept_orders` NOT NULL sans défaut) ;
  - déconnexion purement locale (Uber continuait d'envoyer les commandes) ;
  - webhooks `store.provisioned` / `store.deprovisioned` non traités.
- **Deliveroo** : rien. Les lignes `integration_deliveroo` étaient insérées à la main. Le seul outil, `POST /test/deliveroo/brandID`, a le marchand « 2 » codé en dur (scénarios de certification, laissé tel quel).
- **Back-office** : aucun bouton de connexion. Tutoriels génériques et faux (« Paramètres → Intégrations → Sélectionnez Wello Resto » chez Uber n'existe pas).

## 2. Uber Eats

### 2.1 Ce que dit Uber (Integration Activation)

Sources : [Integration Activation Flows](https://developer.uber.com/docs/eats/guides/integration-activation-flows), [Authentication](https://developer.uber.com/docs/eats/guides/authentication), [POST pos_data](https://developer.uber.com/docs/eats/references/api/v1/post-eats-stores-storeid-posdata), [GET stores](https://developer.uber.com/docs/eats/references/api/v1/get-eats-stores), [DELETE pos_data](https://developer.uber.com/docs/eats/references/api/v1/delete-eats-stores-storeid-posdata).

1. Redirection vers `https://auth.uber.com/oauth/v2/authorize?client_id&response_type=code&redirect_uri&scope=eats.pos_provisioning`.
2. Retour sur `redirect_uri?code=…` ; échange `POST /oauth/v2/token` (`grant_type=authorization_code`). Le token vaut 30 jours, sans refresh documenté.
3. `GET /v1/eats/stores` (paginé `limit` / `start_key` → `next_key`) liste les restaurants du compte.
4. `POST /v1/eats/stores/{store_id}/pos_data` active l'intégration. `is_order_manager: true` : c'est nous qui acceptons les commandes. `pos_integration_enabled` est déprécié, non envoyé.
5. Ensuite, tout passe par le token `client_credentials` existant. Le token utilisateur ne sert qu'à lister, activer et retirer.
6. Retrait : `DELETE …/pos_data`, avec le token utilisateur. Uber envoie `store.provisioned` / `store.deprovisioned`.
7. En production, le scope `eats.pos_provisioning` doit être validé et autorisé par Uber, et la `redirect_uri` enregistrée dans le dashboard développeur.

### 2.2 Parcours implémenté

```
Back-office  « Connecter mon compte Uber Eats »
  └─ GET  /integrations/uber-eats/connect                 → { url }  (state signé)
Uber         connexion + autorisation
  └─ GET  /integrations/uber-eats/callback?code&state      (public)
       échange du code, liste des restaurants, session Redis 15 min (token compris)
       → 302 {UBER_EATS_CONNECT_RETURN_URL}?uber=select&session=…
Back-office  fenêtre « Choisissez vos restaurants »
  ├─ GET  /integrations/uber-eats/connect/sessions/{id}    → restaurants (sans le token)
  └─ POST /integrations/uber-eats/connect/sessions/{id}    { store_ids: [...] }
       par restaurant : POST pos_data puis ligne integration_uber_eats active
```

Déconnexion (icône « Désactiver » de la carte) : `POST /integrations/uber-eats/disconnect?store_id=` → `{ url }` → page Uber → callback (purpose `disconnect`) → `DELETE pos_data` → ligne désactivée → `?uber=disconnected`.

Retours d'erreur vers le back-office : `?uber=error&reason=` avec `denied`, `invalid_state`, `no_store`, `store_not_in_account`, `not_configured` ou `failed`. Libellés dans `marketplaceOnboardingService.UBER_RETURN_REASONS`.

### 2.3 Décisions

| # | Décision | Pourquoi |
|---|---|---|
| U1 | `state` = charge JSON (marchand, utilisateur, but, restaurant, nonce, expiration 15 min) signée HMAC-SHA256, avec une clé dérivée du client secret (`sha256("welloresto/uber-eats/oauth-state/v1\0" + secret)`). | Corrige la faille du `merchant_id` en clair. Aucune nouvelle clé à gérer : le client secret n'est connu que de nous et d'Uber. La doc Uber ne parle pas de `state` ; c'est la pratique OAuth standard. |
| U2 | Le `state` ne sert qu'une fois (`SETNX uber:connect:state:{nonce}`, 15 min). | Empêche de rejouer un retour. |
| U3 | Le token utilisateur n'est **jamais stocké en base**. Il vit 15 min dans la session Redis, le temps du choix, puis la session est supprimée dès qu'un restaurant est connecté. | Pas de secret long terme à chiffrer, donc pas de nouvelle clé de chiffrement. |
| U4 | La déconnexion refait le passage chez Uber. | Corollaire de U3 : `DELETE pos_data` exige le token utilisateur. Une déconnexion seulement locale laisserait Uber envoyer les commandes. |
| U5 | Au retour d'une déconnexion, on vérifie que le compte Uber utilisé donne bien accès au restaurant, avant le `DELETE`. Sinon : `store_not_in_account`. | Sinon `DELETE` répondrait 404, et l'on désactiverait chez nous un restaurant resté relié chez Uber. |
| U6 | Un restaurant n'est relié qu'à **un** établissement Wello Resto (`ActiveStoreOwner`, revérifié au moment de connecter). Statut `linked_elsewhere` dans la fenêtre, non cochable. | Le routage des webhooks se fait par `store_id`. |
| U7 | Restaurant déjà géré par un autre logiciel (`pos_data.order_manager_client_id` ≠ notre client id) : connexion permise, avec un avertissement. | C'est le cas d'un restaurateur qui change de caisse. Uber gère la bascule du gestionnaire de commandes. |
| U8 | `integrator_store_id` = `merchant_id`. | Identifiant de l'établissement côté intégrateur, visible chez Uber. |
| U9 | Ligne `integration_uber_eats` (`ActivateStore`) : `bearer_token = ''`, `auto_accept_orders = false`, `delay_duration = 0`, `pos_provisionning_* = '' / now()`. Une réactivation conserve les réglages. | Voir U10 pour `bearer_token`. Acceptation automatique désactivée par défaut, réglable dans la carte. |
| U10 | `bearer_token` ne porte plus de jeton, mais « `bearer_token IS NOT NULL` » est le marqueur « Uber Eats relié » lu par la réponse de connexion du POS (`auth/repository.go`, `users/repository.go`). `''` pose le marqueur sans rien stocker. La désactivation le remet à `NULL` (comportement de l'ancien `DisableIntegration`). | Ne pas toucher à la réponse de connexion du POS, tout en n'y gardant plus de secret. |
| U11 | Webhook `store.deprovisioned` : toutes les lignes actives du `store_id` sont désactivées et le cache webhook `webhook:uber:store:{id}` purgé. `store.provisioned` : journalisé (avertissement si aucun établissement ne porte le restaurant). | Retrait fait chez Uber, ou par Uber : on s'aligne. Un restaurant activé côté Uber sans passer par nous ne dit pas à quel établissement il revient. |
| U12 | Restaurant visé par un webhook `store.*` : `meta.user_id` (« corresponds to store_id » selon la doc Uber), repli sur `meta.resource_id`. | Le contenu de ces événements n'est pas documenté : **à confirmer au premier événement reçu** (`api_request_logs`). |
| U13 | Sans `UBER_EATS_REDIRECT_URI`, `UBER_EATS_CONNECT_RETURN_URL`, les identifiants ou Redis : `503 not_configured`, et le back-office affiche « pas encore disponible ». | Mieux qu'une redirection vers une adresse fausse. |

### 2.4 Configuration

| Variable | Production |
|---|---|
| `UBER_EATS_REDIRECT_URI` | `https://welloresto-api-prod.onrender.com/integrations/uber-eats/callback` |
| `UBER_EATS_CONNECT_RETURN_URL` | `https://my-wello-resto-prod.onrender.com/integrations/uber-eats` |

Staging : mêmes chemins sur les hôtes de staging. Chez Uber (dashboard développeur) : enregistrer chaque `UBER_EATS_REDIRECT_URI` comme Redirect URI, vérifier que le scope `eats.pos_provisioning` est accordé (sandbox puis prod), et que les webhooks `store.provisioned` / `store.deprovisioned` arrivent sur `/webhooks/uber-eats` (URL primaire unique).

`UBER_EATS_BASE_URL` sert désormais aussi à `GET /v1/eats/stores` et à `pos_data` (repli `https://api.uber.com`).

## 3. Deliveroo

### 3.1 Ce que dit Deliveroo

- Pas de flux OAuth « connectez votre compte ». La liaison se fait **dans le Partner Hub, par le restaurateur** : Integrations → Connect sites → choix du logiciel de caisse → saisie d'un « Site location ID » par site. Ce parcours est décrit par plusieurs éditeurs de caisse ([Atlas](https://help.atlas.kitchen/configuring-atlas/deliveroo-integration), [GoodTill](https://support.thegoodtill.com/deliveroo)), pas par la doc publique de Deliveroo. Wello Resto figure dans la liste des partenaires (confirmé par Ilies le 2026-11-05).
- Dans une commande, `location_id` est décrit comme « The in-store system location ID » ([get-order-v2](https://api-docs.deliveroo.com/reference/get-order-v2)). C'est donc **notre** identifiant, celui que le restaurateur a saisi dans le Partner Hub.
- Type de webhook par site : `GET` / `PUT /order/v1/integrator/brands/{brand_id}/sites-config`, `{"sites":[{"location_id","orders_api_webhook_type": "POS" | "ORDER_EVENTS" | "POS_AND_ORDER_EVENTS"}]}` ([put-sites-config](https://api-docs.deliveroo.com/reference/put-sites-config)).

### 3.2 Parcours implémenté

```
Back-office  carte « Connecter Deliveroo »
  └─ GET  /integrations/deliveroo/connect   → { location_id: "212", sites: [...] }
Restaurateur  Partner Hub : Intégrations → Connecter des sites → Wello Resto,
              « Site location ID » = 212
Back-office  « Vérifier la connexion »
  └─ POST /integrations/deliveroo/connect { location_id: "212" }
       GET /site/v1/restaurant_locations/212 (identifiants intégrateur) → brand_id
       ligne integration_deliveroo active, puis sites-config du site réglé sur ORDER_EVENTS
```

### 3.3 Décisions

| # | Décision | Pourquoi |
|---|---|---|
| D1 | Identifiant à saisir = `merchant_id`, puis `merchant_id-2` … `merchant_id-9` pour des sites supplémentaires. | C'est déjà la convention des établissements reliés à la main (staging : 212 → « 212 », 230 → « 230 »). Un identifiant dérivé de l'établissement ne peut pas être revendiqué par un autre. Il tient dans `location_id varchar(20)`. |
| D2 | Identifiant proposé : le premier qui n'est ni actif pour cet établissement, ni actif chez un autre. Un site déconnecté retrouve donc le même identifiant. | Reconnexion sans ressaisie dans le Partner Hub. |
| D3 | `POST connect` n'accepte qu'un identifiant de la forme D1, ou une ligne existante de l'établissement (identifiants historiques comme le site sandbox `102330` du marchand 2). | Empêche de capter les commandes d'un autre établissement. |
| D4 | La preuve de liaison est l'appel `GET /site/v1/restaurant_locations/{location_id}` avec nos identifiants d'intégrateur. Réponse 400/403/404, ou sans marque → `not_linked` (« Deliveroo ne nous a pas encore transmis ce site… »). | Deliveroo ne répond que pour un site relié à notre intégration. **Codes d'erreur exacts non documentés, à confirmer en sandbox.** |
| D5 | Après activation, réglage best-effort du site sur `ORDER_EVENTS` : la configuration de la marque est relue puis renvoyée **en entier**, le site ajouté ou corrigé. Si le site est déjà en `ORDER_EVENTS` ou `POS_AND_ORDER_EVENTS`, rien n'est envoyé. En cas d'échec : journalisé, la connexion reste valide, et le réglage est refait à chaque vérification. | `/webhooks/deliveroo/orders` ne traite que les Order Events. La doc ne dit pas si `PUT` remplace ou fusionne la liste ; renvoyer la liste complète est correct dans les deux cas. |
| D6 | Pas de déconnexion par API : Deliveroo n'en a pas. Le bouton désactive le compte chez nous (`PATCH /integrations/deliveroo/disable`, existant), et le texte de confirmation demande de retirer aussi la liaison dans le Partner Hub. | Le routage des webhooks de commande ne filtre pas `enabled` (comportement existant, inchangé) : tant que le site est relié chez Deliveroo, ses commandes arrivent. |
| D7 | Le cache `location_id → établissement` du webhook (24 h) n'est pas purgé à la connexion. | Avec D1/D3, un identifiant ne change jamais d'établissement. |

## 4. Changement transverse : compte « principal »

Uber comme Deliveroo choisissaient le compte « principal » d'un établissement par `ORDER BY store_id` / `location_id`, sans tenir compte de `enabled`. Dès qu'on peut déconnecter un restaurant puis en relier un autre, l'écran aurait affiché l'ancien compte, désactivé. **Le compte actif passe désormais en premier** (`ORDER BY enabled DESC, …`) dans :

- `integrations/repository.go` : `GetUberEatsIntegration`, `ListUberEatsAccounts`, `GetDeliverooIntegration`, `ListDeliverooAccounts` (le premier de la liste est `is_primary`) ;
- `ubereats/repository.go` : `GetStoreData`, `GetAccountsByMerchant`, et le routage webhook `GetStoreByStoreIDOnly` / `GetMerchantIDFromStoreID` (`LIMIT 1` ajouté : l'index `store_id` n'est pas unique) ;
- `deliveroo/repository.go` : `GetBrandIDByMerchant` et `GetSiteIDByMerchant` (même ordre, `resolveAccount` combine les deux), `GetAccountsByMerchant` ;
- `webhook/deliveroo_orders/repository.go` : `GetMerchantByLocationID` (`LIMIT 1`).

Pour un établissement mono-compte actif, ce qui est le cas de tous aujourd'hui, le résultat est identique.

## 5. Back-office

- `services/marketplaceOnboardingService.ts` : appels et libellés d'erreur.
- `components/integrations/UberEatsConnect.tsx` : `UberEatsConnectPanel` (écran non connecté) et `UberEatsStoreSelectDialog` (choix des restaurants ; un seul restaurant libre est pré-coché).
- `components/integrations/DeliverooConnect.tsx` : étapes Partner Hub, identifiant à copier, « Vérifier la connexion ».
- `IntegrationCard` : props `inactiveContent` (remplace l'écran « non active ») et `disableDescription`.
- Pages `UberEats.tsx` / `Deliveroo.tsx` : anciens tutoriels supprimés ; retour `?uber=…` traité au chargement (comme `?accounting=` de la comptabilité) ; après désactivation, l'intégration est relue. Avant, la page devenait blanche : l'API renvoie `{status}` et non l'intégration.
- Une seule carte affichée par plateforme (le compte principal) ; la fenêtre Uber permet de relier plusieurs restaurants en une fois.

## 6. Tests

- Unitaires : `ubereats/onboarding_test.go` (URL d'autorisation, state falsifié / étranger / expiré / rejoué, parcours complet sur faux serveur Uber avec pagination, refus `linked_elsewhere`, échec isolé d'un restaurant, déconnexion, `store_not_in_account`, webhook deprovisioned) ; `deliveroo/onboarding_test.go` (`LookupBrandID`, réglage sites-config dans les trois cas, identifiants).
- Intégration Postgres (passés sur staging le 2026-11-06) : `TestUberEatsRepository_Postgres` (activation, désactivation, réactivation, principal actif, deprovision), `TestDeliverooOnboardingRepository_Postgres`, plus les tests existants d'`integrations` et de `deliveroo_orders`. Le test `integrations` ne compilait plus (`NewService` avait gagné le paramètre Redis) : corrigé.

## 7. Recette à faire

**Uber (sandbox, compte de test Uber)**
1. Renseigner les deux variables de staging, puis enregistrer la Redirect URI chez Uber.
2. Back-office, page Uber Eats d'un établissement non relié → « Connecter » → page Uber → retour avec la fenêtre de choix.
3. Connecter : vérifier `integration_uber_eats` (ligne active, `bearer_token = ''`) et `api_request_logs` (`POST pos_data` en 2xx), puis le webhook `store.provisioned` reçu ; noter où se trouve le `store_id` (U12).
4. Passer une commande test : elle arrive sur l'établissement.
5. Désactiver : page Uber, retour `?uber=disconnected`, `DELETE pos_data` en 2xx, ligne inactive, `store.deprovisioned` reçu.

**Deliveroo (sandbox)**
1. Établissement de test sans ligne `integration_deliveroo` → la carte affiche son identifiant.
2. « Vérifier la connexion » avant toute liaison → message `not_linked` ; noter le code HTTP réellement renvoyé par Deliveroo (D4).
3. Relier le site dans le Partner Hub (ou demander à Deliveroo de le faire en sandbox) avec cet identifiant → « Vérifier » → ligne active avec `brand_id`, `GET`/`PUT sites-config` dans `api_request_logs`.
4. Commande test : elle arrive sur l'établissement.

## 8. Points ouverts

- Validation du scope `eats.pos_provisioning` par Uber pour la production.
- Contenu des webhooks `store.provisioned` / `store.deprovisioned` (U12).
- Codes d'erreur de `restaurant_locations` pour un site non relié (D4) et sémantique du `PUT sites-config` (D5).
- Libellés exacts du Partner Hub en français (« Intégrations », « Connecter des sites ») : à ajuster après un essai réel.
- Les établissements déjà reliés à la main ne sont pas touchés : leurs lignes et identifiants restent valides.

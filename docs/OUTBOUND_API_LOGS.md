# Journal des appels API sortants (`api_request_logs`)

2026-10-15. Rédigé au fil des décisions. Non commité, non déployé. **Aucune migration.**

## Pourquoi

Pour déboguer une intégration (Uber Eats, Deliveroo, Brevo, Stripe...), il faut voir ce que nous
avons envoyé et ce que le tiers a répondu, comme `api_request_logs` le fait déjà pour les appels
entrants. Les webhooks reçus y sont déjà (ils passent par le middleware global) ; seuls les appels
sortants manquaient.

## Fonctionnement

[`internal/middleware/request_logger/outbound.go`](../internal/middleware/request_logger/outbound.go)
fournit `requestlogger.Transport(nil)`, un `http.RoundTripper` à poser sur un client :

```go
&http.Client{Timeout: 15 * time.Second, Transport: requestlogger.Transport(nil)}
```

Chaque appel produit une ligne `api_request_logs` via le **même `Logger`** que les entrants (file
mémoire, insertion par lot, jamais bloquant). `routes.go` le désigne avec `requestlogger.SetDefault`.
Sans `SetDefault` (tests, outils `cmd/`), le transport est un simple passe-plat.

| Colonne | Valeur d'une ligne sortante |
|---|---|
| `url` | **URL absolue** (`https://api.brevo.com/v3/smtp/email`) ; les entrants portent un chemin relatif |
| `method`, `status_code`, `duration_ms` | ceux de l'appel ; `status_code` = 0 en cas d'échec de transport |
| `payload` | corps envoyé |
| `response_payload` | corps reçu, ou `{"transport_error":"..."}` (timeout, DNS, connexion refusée) |
| `user_id`, `merchant_id` | repris de la requête entrante quand le contexte la porte, sinon `NULL` (cron, goroutines en `context.Background()`) |
| `ip` | vide |

Distinguer les sortants : `WHERE url LIKE 'http%'`. Exemples :

```sql
-- Derniers échanges Brevo en erreur
SELECT created_at, status_code, duration_ms, url, payload, response_payload
FROM api_request_logs WHERE url LIKE 'https://api.brevo.com%' AND status_code NOT BETWEEN 200 AND 299
ORDER BY created_at DESC LIMIT 50;

-- Tout ce qui touche une commande Uber, entrant (webhook) et sortant, dans l'ordre
SELECT created_at, method, url, status_code, payload, response_payload
FROM api_request_logs
WHERE payload::text LIKE '%<id commande uber>%' OR response_payload::text LIKE '%<id commande uber>%'
ORDER BY created_at;

-- Échecs réseau
SELECT created_at, url, response_payload FROM api_request_logs
WHERE url LIKE 'http%' AND status_code = 0 ORDER BY created_at DESC;
```

## Clients branchés

Uber Eats (`modules/ubereats`), Deliveroo (`client.go`, `repository.go`, `service.go`), Brevo mail et
SMS, Google Maps, FCM (client et gestionnaire de jeton), Pennylane (client par défaut), Stripe
(`stripeclient.NewStripeManager`, mêmes réglages que le SDK : 80 s ; chaque nouvelle tentative
réseau du SDK est journalisée).

Pas branchés, volontairement ou parce que hors de portée :

- **IA (Anthropic, OpenAI)** : prompts volumineux, déjà couverts par le cache. Une ligne à ajouter si besoin.
- **Techniques secondaires** : certificats Google, SIRENE, logos, upload S3 Deliveroo (binaire).
- **Code mort** : `internal/client/*`, `modules/deliveroo/client_old.go`,
  `webhook/ubereats/client` (aucun appelant).
- **Stripe via l'API globale** (`stripe.Key`, `webhook/stripe/service.go`, `paymentintent` v78) : ne passe pas par `NewStripeManager`.
- Les commandes `cmd/` (backfills) : pas de `SetDefault`, donc pas de journal.

## Garde-fous (pas de régression, pas de crash)

- **Le résultat d'un appel n'est jamais modifié.** Le corps de la requête est lu sur une copie
  (`GetBody`) ; le corps de la réponse est lu par anticipation (32 Ko) puis **restitué en entier**
  à l'appelant (début relu + suite du flux). Une erreur de lecture survenue pendant la lecture
  anticipée est rejouée à l'appelant. La lecture anticipée garantit une ligne même quand
  l'appelant jette la réponse sans la lire ni la fermer.
- **Jamais bloquant** : `Logger.Log` abandonne l'entrée si la file (1000) est pleine.
- **Panics absorbés** dans la construction de l'entrée.
- **JSON toujours valide** : `payload` et `response_payload` sont `jsonb`, un seul corps invalide
  ferait échouer tout le lot d'insertion (entrants compris, déjà arrivé : voir
  [audit-request-logger-postgres-reactivation.md](audit-request-logger-postgres-reactivation.md) §1.2).
  JSON valide conservé tel quel ; texte non-JSON (page HTML d'erreur d'un tiers) conservé comme
  chaîne JSON ; binaire remplacé par `{"non_json_body_bytes":N}` ; séquence `\u0000` (refusée par
  `jsonb`) également remplacée.
- **Interrupteur sans redéploiement** : `OUTBOUND_REQUEST_LOG=false` + redémarrage désactive tout.
- Les entrants ne sont pas modifiés. Seul risque partagé : les sortants occupent la même file de
  1000 entrées ; en cas de rafale, des lignes sont perdues (avec un `Warn`), jamais l'appel.

## Données sensibles

- **Les en-têtes ne sont jamais stockés** (aucune colonne) : pas d'`Authorization` ni de clé d'API.
- **Endpoints d'authentification** (chemin contenant `oauth` ou `/token`) : aucun corps stocké,
  seulement statut et durée (`{"redacted":"auth endpoint"}`).
- **Masquage des secrets** dans les autres corps : `client_secret`, `access_token`, `refresh_token`,
  `id_token`, `api_key`, `password`, `secret`, `authorization`, `assertion` (JSON et formulaire).
  Le `client_secret` d'un PaymentIntent Stripe, qui figure dans la réponse, est donc masqué.
- **Query string** : paramètres contenant `token`, `key`, `secret`, `sig`, `signature`, `password`,
  `code`, `auth` masqués (la clé `key=` de Google Maps).
- **Données personnelles non masquées** : corps Brevo (emails, téléphones, contenu), adresses
  Google Maps, données clients Stripe. Rétention **90 jours** (purge mensuelle
  `CleanupOldRequestLogs`, donc jusqu'à environ 120 jours en pratique) ; à garder en tête pour le RGPD.
- **Plafond 32 Ko** par corps, début conservé et marqué `…[truncated]`. Une pièce jointe base64
  dans un mail Brevo y est coupée.

## Décisions

1. **Même table, pas de colonne `direction`** : zéro migration, donc zéro risque de déploiement
   (ajouter une colonne à l'`INSERT` aurait fait échouer *tous* les journaux tant que la migration
   n'était pas jouée). L'URL absolue suffit à distinguer. Si le besoin d'index ou de filtre se
   fait sentir, ajouter une colonne `source` nullable plus tard.
2. **Webhooks inchangés** : déjà dans `api_request_logs`. Leurs en-têtes (signatures) restent non
   journalisés.
3. **Lecture anticipée plutôt que journalisation à la fermeture du corps** : certains appelants
   ne lisent ni ne ferment la réponse (`deliveroo/service.go` le faisait), la ligne aurait manqué.
4. **`http.DefaultClient` de `deliveroo/service.go` remplacé** par un client dédié sans délai
   d'expiration (comme avant) ; la réponse est maintenant fermée (fuite de connexion corrigée).
5. **Rétention** : la constante valait déjà 90 jours ; seuls des commentaires disaient 30.

## Vérifié

- `go build ./...`, `go vet` des paquets touchés : OK.
- 12 tests dans [outbound_test.go](../internal/middleware/request_logger/outbound_test.go) : passe-plat
  sans logger, corps intacts côté appelant, réponse jamais lue, réponse tronquée seulement dans le journal,
  JSON toujours valide (HTML, binaire), échec de transport, masquage (query, corps, OAuth), identité,
  file pleine non bloquante, erreur de lecture propagée.
- Tests des paquets touchés : OK, sauf `TestUberEatsBYOCStatusUpdate_UsesBrandOrderID`
  (`modules/ubereats`), qui échoue **identiquement sur HEAD** (erreur de `Scan` sqlmock, sans rapport).
- **Non testé** : insertion réelle en base Postgres avec des lignes sortantes (pas de base locale), et
  appels réels aux tiers. À vérifier sur staging : déclencher un SMS/mail Brevo ou un appel Uber, puis
  `SELECT ... WHERE url LIKE 'http%'`.

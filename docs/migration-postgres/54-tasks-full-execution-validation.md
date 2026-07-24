# 54 — Exécution réelle et complète des 7 fonctions cron `internal/tasks/` contre un Postgres jetable

Date : 2026-07-24
Branche : `staging`

## Objectif

Le rapport [52](52-tasks-cron-conversion.md) avait converti les 6 tâches cron vers `dbx` et
vérifié leur portabilité SQL (fragments réutilisés, upserts scopés à un marchand sentinelle),
mais avait **délibérément évité** d'appeler `CloseOrders`/`DenyOrders`/`CapturePayments`/
`CancelPayments` tels quels, car ces points d'entrée bouclent sur l'intégralité des tables
`orders`/`payments`/`stripe_payments` sans filtre marchand et déclenchent des effets externes
réels (Stripe, Deliveroo, Uber Eats, notifications). Ce rapport comble ce trou : exécution
**réelle et complète** (pas des fragments, pas des services scopés à un sentinelle) des 7
fonctions exportées, en appel direct Go, contre un Postgres Docker de dev rechargé à neuf avec
une copie complète des données réelles (147/147 fichiers). **Aucune donnée réelle n'est citée
dans ce rapport — uniquement des comptages structurels.** Rien n'a été commité.

## 0. Décision préalable — effets externes réels

Quatre des sept tâches (`CloseOrders`, `DenyOrders`, `CapturePayments`, `CancelPayments`)
appellent, via `OrderService`/`StripeService`, de vrais clients externes (Stripe
capture/refund — [service.go:14-115](../../internal/infrastructure/stripe/service.go#L14-L115),
webhooks Deliveroo/Uber Eats —
[service.go:317](../../internal/modules/order_life_cycle/service.go#L317),
[service.go:766-786](../../internal/modules/order_life_cycle/service.go#L766-L786) — et des
notifications client réelles, systématiques quel que soit le brand). Point signalé et arbitré
avant toute exécution : le chef de projet a confirmé que les identifiants d'intégration utilisés
pour ce test **ne sont pas des identifiants de production** et a autorisé l'exécution telle
quelle. En pratique, aucun identifiant réel (Stripe test, Deliveroo, Uber Eats, Brevo, FCM)
n'était disponible dans cette session : la posture retenue a donc été d'utiliser des valeurs
manifestement factices (`sk_test_dummy_not_real`, etc.) ou de laisser ces variables absentes —
`internal/config/*.go` ne valide au démarrage que `POSTGRES_URL`/`MYSQL_URL`, `GOOGLE_API_KEY`,
`R2_PRIVATE_BUCKET`, `PIN_PEPPER` (`internal/config/config.go:58-75`) ; Stripe/Deliveroo/Uber
Eats/Brevo/FCM sont optionnels et échouent proprement (erreur loguée, jamais de panic — chaque
appel externe est protégé par un `recover()`, cf. `internal/infrastructure/stripe/service.go`) si
absents ou invalides. Conséquence documentée au §5 : les 4 tâches ont bien été exécutées via leur
point d'entrée réel et complet, mais aucune n'a en pratique déclenché d'appel externe (0 ligne
éligible dans les 4 cas — §5), donc la validation du chemin d'appel externe lui-même reste, comme
au rapport 52, non exercée pour de vrai dans cette session.

## 1. Réinitialisation et rechargement — même protocole que les rapports 43/51

```
docker compose -f docker-compose.postgres.yml down -v
docker compose -f docker-compose.postgres.yml up -d
```

Schéma cible chargé (`04-schema-postgres-target.sql`, copie de travail incluant déjà le correctif
`audit_logs.id varchar(36)→varchar(64)` du rapport [53](53-audit-logs-column-width.md)) : 0 erreur.
Vérifié directement (`\d audit_logs`) : `id` bien `character varying(64)`.

147 fichiers de données réelles régénérés depuis `data-migration/migration_welloresto_data.sql`
(`transform_mysql_csv.py generate-all-sql`, jamais commité — gitignored) puis chargés
séquentiellement (`psql -v ON_ERROR_STOP=1`, un fichier à la fois) :

```
tables_checked=147 mismatches=0 total_expected=472774 total_actual=472774
```

**147/147 tables, 0 écart, 472 774 lignes** — identique aux rapports 43/51. Fichiers `.sql`
régénérés supprimés en fin de session (jamais dans le dépôt, `.gitignore` du dossier
`data-migration/`).

## 2. Mécanisme d'appel direct — jetable, jamais commité

Aucun mécanisme existant n'exposait les 7 fonctions pour un appel direct hors scheduler (seul
`RecomputeUpsellPatterns` est câblé sur un endpoint admin,
[upsell_handler.go:34](../../internal/modules/admin/upsell_handler.go#L34)). Reconstruire tout le
graphe de dépendances de `OrdersLifeCycleService` en dehors de `SetupRoutes` (30+ modules
interdépendants, cf. `cmd/api/routes.go:278-296`) aurait dupliqué le câblage réel et risqué de
diverger de la prod. Solution retenue, même esprit que les outils jetables des rapports 43/51 :

- `cmd/api/routes.go` : `SetupRoutes` retourne temporairement aussi le `*TasksManager` déjà
  construit en interne (`return r, taskManager` au lieu de `return r`).
- `cmd/api/main.go` : après `SetupRoutes`, une branche `RUN_TASK_ONCE=<NomFonction>` appelle la
  fonction demandée directement puis quitte (au lieu de démarrer le serveur HTTP), avec une pause
  de 5s pour laisser une chance aux goroutines fire-and-forget (Stripe, Deliveroo/Uber Eats) de
  s'exécuter avant la sortie du process.

**Découverte non anticipée, transverse à ce chantier** : `cmd/api/tasks.go:SetupTasks` n'a **pas**
de `return` précoce sur cette branche — contrairement à ce que documente `CLAUDE.md`
("actuellement désactivées via un return précoce"), le scheduler cron réel démarre bel et bien à
chaque lancement de `cmd/api` (log observé : `✅ Système CRON démarré (toutes tâches actives...)`,
confirmé en lisant `cmd/api/tasks.go:1-70` — jobs `@hourly`/`@every 1m`/`@every 15m` bien
enregistrés). Sans garde, une exécution planifiée concurrente (ex. `DenyOrders @every 1m`) aurait
pu polluer les comptages avant/après de ce rapport. Correctif appliqué dans le même patch jetable :
`SetupTasks(log, taskManager)` n'est appelé que si `RUN_TASK_ONCE` est vide. **`CLAUDE.md` était
donc obsolète sur ce point précis** — corrigé le 2026-07-24 (section "Real-time & background jobs") :
le texte affirmait un `return` précoce désactivant `SetupTasks` ; il documente désormais
l'absence de toute garde par environnement (`SetupTasks` et son appel depuis `SetupRoutes` sont
inconditionnels), confirmée ici sur `staging`. Les autres mentions de ce même early-return dans
`audit-reservation-existant.md`, `cadrage-technico-fonctionnel-lot1.md` et
`docs/audits/2026-07-01-upsell-v2.md` restent, elles, non corrigées (hors périmètre de cette
mise à jour, qui ne portait que sur `CLAUDE.md`).

Les deux fichiers ont été **intégralement rétablis** en fin de session
(`git checkout -- cmd/api/main.go cmd/api/routes.go`) — confirmé par `git status` identique avant/
après ce chantier (§7).

## 3. Redis — nécessaire pour `RecomputeUpsellPatterns`

`RecomputeUpsellPatterns` écrit ses résultats dans Redis, pas Postgres
(`tm.AICache.Set(...)`, [upsell.go:218](../../internal/tasks/upsell.go#L218)) et s'auto-annule si
`tm.AICache` est indisponible ([upsell.go:30](../../internal/tasks/upsell.go#L30)). Le conteneur
`redis-local` (préexistant, arrêté avant cette session) a été démarré pour la durée du test,
`FLUSHDB` puis arrêté en fin de session (retour à l'état constaté au démarrage — §7).

## 4. Environnement d'exécution commun

```
DB_DIALECT=postgres
POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev
REDIS_URL=redis://localhost:6379/0
GOOGLE_API_KEY / R2_PRIVATE_BUCKET / PIN_PEPPER / STRIPE_API_KEY = valeurs factices explicites
```

Stripe/Deliveroo/Uber Eats/Brevo/FCM : aucun identifiant réel fourni (§0). `go build ./cmd/api` :
OK.

## 5. Résultats — par tâche, appel réel et complet

| # | Tâche | Statut | Lignes/clés mutées | Cause du volume observé |
|---|---|---|---|---|
| 1 | `CloseOrders` | ✅ 0 erreur | **0** (vérifié : `orders_total` et `qrcodes_total` inchangés avant/après) | Éligibilité exige `isPaid AND isDistributed AND state<>'CLOSED'` — vérifié en lecture seule **avant** l'appel : sur les 111 commandes non `CLOSED` de la base, **0** sont simultanément `isPaid`/`isDistributed`. Zéro cohérent, cause structurelle (état des commandes), pas un artefact temporel — les conditions de délai de cette requête sont des bornes basses (`>= N minutes depuis`), trivialement satisfaites par des données vieilles de plusieurs jours. |
| 2 | `DenyOrders` | ✅ 0 erreur | **0** (`orders_brand_status_denied` inchangé, `orders_total` inchangé) | Seules 2 commandes de toute la base ont `brand_status='PENDING_APPROVAL'` ; **0** ne satisfont en plus `state<>'DONE' AND brand='WELLO_RESTO' AND scheduled=false`. Zéro cohérent (rareté structurelle de l'état transitoire ciblé dans un instantané), pas un artefact temporel (même raisonnement que #1). |
| 3 | `UpdateAverageDistributionTime` | ✅ 0 erreur | **0** (`average_distribution_time` : 19→19 lignes) ; log applicatif : `merchants_analyses=28, merchants_mis_a_jour=0` | **Seule cause temporelle réelle des 7 tâches** : la fenêtre d'analyse est une borne *haute* (`oi.ordered_on >= now() - 1440 min`, soit les dernières 24h). Or la commande la plus récente de cet instantané date de ~3,5 jours avant l'horloge réelle au moment du test (instantané figé vs horloge vivante) — aucun `orderitem` ne peut tomber dans cette fenêtre glissante. Les 28 marchands ont bien été analysés (capacité de production configurée sur 28/28), 0 mise à jour est l'effet attendu de cet artefact horloge, pas un bug. |
| 4 | `CapturePayments` | ✅ 0 erreur | **0** (aucun appel Stripe déclenché) | Vérifié en lecture seule avant l'appel : sur 94 lignes `stripe_payments` au statut `REQUIRES_CONFIRMATION`, seules 4 ont un `payment_intent_id` non nul, seule 1 est jointe à une commande `CLOSED`, seules 3 sont jointes à un compte Stripe connecté (`stripe_accounts`) — intersection de plusieurs sous-ensembles déjà rares, vide par construction. Zéro cohérent (0 appel externe déclenché — confirme aussi qu'aucun risque n'a été couru malgré la décision du §0). |
| 5 | `CancelPayments` | ✅ 0 erreur | **0** (aucun appel Stripe déclenché) | Même requête que #4, branche `brand_status IN ('DENIED','CANCELED')` — même intersection vide. |
| 6 | `UpdatePopularProducts` | ✅ 0 erreur, `merchants_ok=27, merchants_failed=0` | **Mutation réelle confirmée** : `products.is_popular=TRUE` passe de 89 à 88 lignes ; `products_total` inchangé (2493→2493, confirme UPDATE seul, aucun INSERT/DELETE) | Fenêtre 30 jours (borne haute large) : 1 534 commandes réelles tombent dans cette fenêtre (contre 0 pour la fenêtre 24h de #3) — le recalcul a bien eu de la matière réelle à traiter sur 27/27 marchands abonnés. Magnitude cohérente (~3 produits/marchand en moyenne), ni nulle ni un balayage complet de la table. |
| 7 | `RecomputeUpsellPatterns` | ✅ 0 erreur, `merchants_ok=27, merchants_failed=0, total_patterns=370` | **Mutation réelle confirmée (Redis, pas Postgres)** : `DBSIZE` 0→157 clés (149 clés de patterns produit + 8 clés `_meta`, 8 marchands actifs sur 27 ayant produit au moins un pattern qualifiant) ; TTL vérifié ≈36h (constante `upsellPatternTTL`) | Fenêtre 90 jours : 4 612 commandes réelles dans la fenêtre. Le seuil `upsellMinCoOccur=5` co-occurrences (assez strict) explique que seuls 8/27 marchands produisent des patterns — cohérent avec une distribution réaliste de volume de commandes entre petits et gros marchands, pas une anomalie. |

**7/7 tâches exécutées réellement et intégralement, 0 erreur, 0 volume incohérent.** Aucun arrêt
requis au sens du point 4 de la consigne : chaque zéro a été vérifié par une requête de lecture
seule indépendante *avant* l'appel (pas seulement déduit après coup), et les deux mutations non
nulles sont bornées et plausibles au regard des invariants de table (compte total inchangé,
magnitude proportionnée au nombre de marchands actifs).

## 6. Nettoyage et vérifications finales

- `cmd/api/main.go` / `cmd/api/routes.go` : rétablis (`git checkout`), `git status` identique à
  l'état constaté avant ce chantier (mêmes fichiers modifiés/non suivis qu'en début de session —
  aucun résidu de ce chantier).
- `redis-local` : `FLUSHDB` puis conteneur arrêté (état constaté au démarrage de la session).
- `welloresto-postgres-dev` : **détruit** (`docker compose -f docker-compose.postgres.yml down
  -v`) — ce Postgres contient désormais des données mutées par ce test (`is_popular`), il n'est
  pas conservé comme référence propre, conformément à la consigne.
- 147 fichiers `.sql` régénérés, binaire de test compilé, logs de chargement : supprimés du
  répertoire de travail temporaire.
- Aucun fichier du dépôt modifié par ce chantier n'a été laissé en l'état — `git status` ne montre
  que les éléments déjà présents avant cette session (rapports 48-53, `sqlcompat.go`,
  `postgres_integration_test.go`, migration 067, modifications déjà en cours de
  `internal/tasks/*.go`/`04-schema-postgres-target.sql`). **Rien n'a été commité par ce chantier.**

## 7. Limites et points ouverts

1. **Chemin d'appel externe non exercé pour de vrai** : les 4 tâches à risque externe ont été
   appelées via leur point d'entrée réel et complet (conformément à la consigne), mais comme
   0 ligne n'était éligible dans cet instantané, aucun appel Stripe/Deliveroo/Uber Eats n'a
   effectivement été déclenché. La portabilité de la requête SQL et la logique d'éligibilité sont
   donc vérifiées de bout en bout ici, mais le comportement du client Stripe/Deliveroo/Uber Eats
   lui-même face à un vrai appel (succès/erreur, effet sur `stripe_payments`/notifications) reste
   à couvrir séparément — soit avec de vraies données de test comportant des lignes éligibles,
   soit avec un identifiant Stripe sandbox dédié.
2. **`CLAUDE.md` obsolète sur l'état du scheduler cron** — **corrigé** (2026-07-24) : le fichier
   documentait `SetupTasks` comme désactivé par un `return` précoce ; ce n'était plus le cas sur
   `staging` (`cmd/api/tasks.go` démarre bien `cron.New(...).Start()` sans condition, appelé
   inconditionnellement depuis `SetupRoutes`). `CLAUDE.md` documente désormais le comportement réel
   (scheduler actif sur tous les environnements). D'autres documents internes portent la même
   affirmation obsolète (`audit-reservation-existant.md`, `cadrage-technico-fonctionnel-lot1.md`,
   `docs/audits/2026-07-01-upsell-v2.md`) et n'ont volontairement pas été touchés ici (hors
   périmètre : seul `CLAUDE.md` était visé).
3. **Précision de la mesure `UpdatePopularProducts`** : le delta net (89→88) est un plancher, pas
   un compte exact de lignes touchées par les deux `UPDATE` de chaque marchand (un produit
   remis à `FALSE` puis un autre remis à `TRUE` chez un même marchand se compenserait dans le
   delta net sans apparaître) — suffisant pour juger la cohérence du volume (ni nul, ni un
   balayage complet), mais pas un audit ligne à ligne.
4. **Instantané vs horloge vivante** : la commande la plus récente de ce chargement date
   structurellement de plusieurs jours avant l'exécution de ce test (§5 #3) — toute future
   répétition de ce protocole avec le même dump verra la même tâche (`UpdateAverageDistributionTime`,
   fenêtre 24h) retourner 0 par construction, indépendamment de tout changement de code. À garder
   en tête pour ne pas interpréter à tort un futur 0 comme une régression.

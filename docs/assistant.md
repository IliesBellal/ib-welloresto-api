# Assistant conversationnel (`internal/modules/assistant`)

2026-11-10. Bouton flottant du back-office : actions rapides et demandes en
langage courant (« plus de tiramisu », « CA d'hier », « qui bosse ce soir ? »).
Conçu pour être repris plus tard par le POS (prise de commande orale) et la
borne. Aucun LLM, aucun coût par requête.

## Répartition des rôles

| Où | Quoi |
|---|---|
| Client (wello-back-office, `src/lib/assistant/`) | Comprend la phrase (moteur à intentions), exécute l'action via les routes habituelles, mesure l'usage |
| API (ce module) | Sert le « modèle » (vocabulaire), reçoit la mesure d'usage, **apprend** des demandes incomprises, expose l'administration |

Le moteur tourne côté client : réponse instantanée, fonctionne hors ligne
avec le modèle en cache, aucune charge serveur par phrase. L'API ne voit
jamais une action passer « par l'assistant » : rupture, CA, planning… passent
par leurs routes existantes, avec leurs propres droits.

## Tables (migrations `206_assistant`, `207_assistant_seed`)

| Table | Rôle |
|---|---|
| `assistant_intents` | Ce que l'assistant sait faire : clé, libellé, type (`action`, `navigate`, `info`, `system`), gestionnaire client (`action`), slots, droit requis, confirmation, intention **sensible**, surfaces (`backoffice`, `pos`, `kiosk`) |
| `assistant_terms` | Mots-clés et phrases de chaque intention. `source` : `seed` (pré-rempli), `manual` (saisi), `learned` (appris). `status` : `candidate`, `active`, `validated`, `rejected`. `merchant_id = ''` : global |
| `assistant_entity_aliases` | Surnoms appris par établissement (« dessert du jour » → produit X) |
| `assistant_learning_signals` | Observations brutes : `learned`, `matched`, `negative`. Unique par événement : un lot renvoyé ne compte pas deux fois |
| `assistant_lexicon` | Mots vides, mots d'orientation (`hint`), liste noire (jamais appris seuls) |
| `assistant_sessions` | Une ligne par ouverture : qui (utilisateur, rôle, établissement), où (page, appareil), durée, compteurs, issue |
| `assistant_events` | Une ligne par interaction, horodatée à la ms, avec le temps écoulé depuis l'ouverture |

`207_assistant_seed` est **générée** depuis le back-office
(`src/lib/assistant/seed.ts`, même vocabulaire que le modèle embarqué) :

```bash
# dans wello-back-office
npm run assistant:seed-sql -- ../ib-welloresto-api/migrations/todo 207_assistant_seed
```

Rejouable : les intentions sont mises à jour, un terme déjà présent n'est
jamais modifié (un rejet ou un poids changé par l'administration est conservé).

`208_assistant_seed_channels` (2026-11-10) : même pré-remplissage régénéré
(canal de vente facultatif sur le CA et les meilleures ventes) et suppression
des termes appris qui n'étaient qu'une période (« janvier »). Rejouable.

**Ordre de déploiement** : 206, 207, 208, puis le code. Sans les tables, l'API
sert un modèle vide (le back-office garde son modèle embarqué) et ignore les
événements avec un avertissement.

## Routes

### Back-office (authentifié, toute personne connectée)

`GET /assistant/model?surface=backoffice&version=v1-…`

Modèle de l'établissement : intentions, termes globaux et propres à
l'établissement (actifs ou validés), surnoms, lexique. Avec `version` connue
et inchangée : `{ "unchanged": true }`. La version est une empreinte des
dates de modification ; le recomptage horaire des compteurs ne la change pas.
Les intentions ne sont pas filtrées par droits : le client le fait, et chaque
action reste protégée par sa propre route. Un terme appris non validé est
servi avec un poids × 0,8.

`POST /assistant/events` → 204 (limite : 120 requêtes/min par IP)

```json
{
  "session": { "id": "uuid", "surface": "backoffice", "started_at": 1762800000000,
               "origin_path": "/menu/products", "device": "mobile", "platform": "ios",
               "model_version": "v1-…", "online": true },
  "events": [
    { "event_id": "uuid", "type": "query_submitted", "seq": 2, "ts": 1762800001200,
      "turn_id": "uuid", "data": { "text": "mets la margherita en sommeil",
      "query_norm": "mets la margherita en sommeil", "input": "voice" } }
  ]
}
```

- 50 événements par lot, `data` ≤ 4 Ko. Le type doit figurer dans
  `allowedEventTypes` (`models.go`), aligné sur `ASSISTANT_EVENT_TYPES` côté client.
- Marchand, utilisateur et rôle viennent du **jeton**, jamais du client. Une
  session existante appartenant à quelqu'un d'autre est refusée.
- Idempotent (`event_id` unique).
- Apprentissage, lu dans les événements :
  - `action_succeeded.data.learning` (`{ intent_key, query_norm, term?, alias? }`)
    crée ou retrouve un candidat **propre à l'établissement**, plus un signal `learned` ;
  - `intent_matched.data.term_ids / alias_ids` donnent des signaux `matched` ;
  - `cancelled` / `rephrased` (mêmes champs) donnent des signaux `negative`.

  Le texte est re-normalisé (`normalize.go`, identique au client) et refusé
  s'il est vide, trop long, fait seulement de mots vides / liste noire, ou
  seulement d'une période (« janvier », « samedi soir », « 2025 »).

### Plateforme admin (`RequirePlatformAdmin`, staff WelloResto)

| Route | Usage |
|---|---|
| `GET /admin/assistant/stats?from=&to=&merchant_id=` | Sessions, utilisateurs, taux de compréhension, taux de réussite, voix/clavier, médianes (temps jusqu'à l'action, durée), issues, appareils, top intentions (avec annulations et score moyen), courbe hebdomadaire, état de l'apprentissage |
| `GET /admin/assistant/fallbacks?from=&to=&limit=` | Requêtes incomprises regroupées par mots, avec ce que les gens ont choisi ensuite et le nombre d'abandons |
| `GET /admin/assistant/intents` | Intentions et nombre de termes |
| `GET /admin/assistant/terms?status=&source=&intent_key=&merchant_id=&review=true` | Termes (à revoir en premier) |
| `POST /admin/assistant/terms` | Terme saisi à la main (validé d'office) |
| `PATCH /admin/assistant/terms/{id}` | Valider, rejeter, déplacer vers une autre intention, repondérer, passer en global |
| `GET /admin/assistant/aliases` · `PATCH /admin/assistant/aliases/{id}` | Surnoms |
| `GET /admin/assistant/sessions/{id}/events` | Rejeu d'une session |
| `POST /admin/assistant/learning/run` | Passage d'apprentissage immédiat |

## Apprentissage autonome (`learning.go`, tâche `@hourly`)

| Règle | Seuil |
|---|---|
| Candidat → actif dans l'établissement | ≥ 3 occurrences, par ≥ 2 personnes **ou** 1 personne sur ≥ 3 jours (restaurant tenu seul) |
| Conflit (même texte appris pour une autre intention) | Promotion seulement si l'intention réunit ≥ 80 % des occurrences, sinon revue humaine (`review_reason = conflict`) |
| Intention sensible (déconnexion…) | Jamais de promotion automatique (`review_reason = sensitive`) |
| Actif → global | Actif dans ≥ 3 établissements |
| Rétrogradation | ≥ 5 utilisations dont > 30 % annulées ou reformulées → candidat + revue (`negative_feedback`) |
| Surnom d'entité | ≥ 2 occurrences, ≥ 80 % vers le même produit |
| Décision humaine | Toujours prioritaire ; un rejet est définitif |

Fenêtre glissante de 180 jours. Toute la politique tient dans `decideTerm` et
`decideAlias` (fonctions pures, testées).

## Conservation (RGPD, tâche quotidienne 5 h 40)

- Texte libre (`text`, `query_norm`, `transcript`, `learning`…) retiré des
  événements après **90 jours** (`anonymized_at`).
- Événements et sessions supprimés après **400 jours**, signaux après **365 jours**.
- Le client masque e-mails et numéros de téléphone avant tout envoi.
- Les termes appris (vocabulaire, sans donnée personnelle) sont conservés.

## Réutilisation POS / borne

- `surface` dans le modèle et les sessions : le POS demandera
  `GET /assistant/model?surface=pos` et ne recevra que les intentions qui le
  déclarent (`surfaces` contient `pos`).
- Le moteur client gère déjà les **quantités** (« deux margherita ») et les
  entités multiples : une intention `pos.order.add_item` avec les slots
  `product` + `quantity` suffira pour la prise de commande orale.
- Le format du modèle est documenté dans `wello-back-office/docs/assistant/README.md` :
  un portage Dart du moteur (5 fichiers, ≈ 1 300 lignes commentées, sans dépendance) réutilise le même modèle et le
  même apprentissage.

## Tests

```bash
go test ./internal/modules/assistant/
DB_DIALECT=postgres POSTGRES_URL=postgres://… \
  go test -tags postgres_integration ./internal/modules/assistant/
```

Le test d'intégration déroule le cycle complet : modèle servi, événements (et
renvoi du même lot), promotion, modèle enrichi et isolé par établissement,
rétrogradation, session protégée, statistiques et replis, passage en global,
anonymisation.

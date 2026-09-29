# Note de cadrage — Import de carte par photo (IA)

**Statut : arbitré et instruit le 2026-09-29, prêt pour le développement (non commencé).**
- Décisions : § 7.
- Réponses aux questions ouvertes : § 8.
- *(défaut proposé)* : point non discuté explicitement, contestable.

Retirés du périmètre le 2026-09-29 :
- l'objet « Carte » (versions de carte, bascule programmée) : ni un besoin client, ni un prérequis de l'onboarding ;
- **l'import de PDF** : photos uniquement, pour simplifier. Un marchand qui n'a qu'un PDF peut en faire des captures d'écran. Le rendu des pages en images dans le navigateur reste possible plus tard, sans changement côté API.

---

## 0. En bref

- **Le principe.** La lecture de photo devient une quatrième source (« porte ») de l'import produits existant : photos → IA → aperçu → relecture → validation. Le pipeline preview/commit et l'assistant de relecture du back-office existent et sont testés. L'IA ne fait que remplir le modèle canonique ; les décisions restent au marchand.
- **Prérequis vérifiés dans le code :**
  1. Les mappers Uber Eats et Deliveroo n'envoient pas les sous-produits des groupes. Défaut confirmé en lisant le code, visible dans les données de staging (§ 5.8).
  2. La couche IA doit être remise à niveau : elle ignore le modèle configuré par tâche, ne gère pas les images et lit mal la réponse des modèles actuels (§ 5.3).
  3. Il faut un brouillon durable, le snapshot actuel expirant en 30 min.
- **Modèle** : Opus 5.5, effort `medium`, changeable **uniquement par variable d'environnement**.
- **Crédits** : 10 extractions par marchand.
- **Coût** : environ 0,3 $ par carte type (§ 5.10).

---

## 1. Contexte et objectifs

**Problème.** Créer sa carte est l'étape la plus longue de l'onboarding. Trois portes existent : un export de caisse (Zelty), le modèle Excel Wello, et la saisie manuelle en masse. Un restaurateur qui n'a qu'une carte papier ou un PDF doit tout ressaisir.

**Objectif.** Qu'un marchand obtienne une carte exploitable (catégories, produits, prix, groupes, suppléments) en quelques minutes à partir de photos de sa carte, **après relecture par lui**.

**Indicateurs**
- Temps médian entre la création du compte et le premier produit actif.
- Part des lignes extraites validées sans modification.
- Part des imports photo lancés qui vont jusqu'à la validation.

**Hors périmètre**
- Objet « Carte » (abandonné).
- Import de PDF (retiré).
- Photos de plats et descriptions générées.
- Création de formules : le concept n'existe pas dans Wello, voir [MENUS_COMBOS_AUDIT.md](MENUS_COMBOS_AUDIT.md).
- Allergènes (Q7).
- Traduction.
- OpenAI pour cette tâche.

---

## 2. Analyse concurrentielle

### 2.1 Import de carte par photo ou PDF

| Acteur | Formats | Qui traite | Délai | Relecture avant création | À retenir |
|---|---|---|---|---|---|
| **Lightspeed Restaurant (K-Series)** | PDF, PNG, JPG (+ HTML, CSV, TXT) | IA, dans le back-office | quelques minutes | **Oui, obligatoire** : aperçu, édition ligne à ligne, suppression | Le plus proche de notre cible. Alerte sur les doublons de nom. Stratégie de modificateurs réglable. Onboarding accompagné payant à côté (500–2 000 $) |
| **Square for Restaurants** | PDF, JPG, PNG, URL | IA (intégration WoFlow), en asynchrone | « moins de 24 h », e-mail quand c'est prêt | Oui pour les imports depuis une autre plateforme ; pas documenté pour les fichiers | Asynchrone assumé |
| **Toast** | PDF (anglais, 2 Mo max) via l'intégration OpenTable | IA | — | — | Toast IQ modifie noms, descriptions et prix avant publication. Outils tiers dédiés (Menu Checkpoint) |
| **Outils de menu digital** (SmartlyMenu, ClickyMenu, IAMenu, Foodvizer) | photos, PDF, captures, Word, Excel | IA | minutes | **Oui**, le résultat est un brouillon | IAMenu accepte jusqu'à 16 photos et **suggère** les allergènes. Foodvizer propose aussi un service humain |
| **Uber Eats** | — | Humain (séance photo offerte) | — | — | L'IA sert à enrichir, pas à créer la carte |
| **Deliveroo** | — | Formulaire de contact | jusqu'à 3 jours | — | Pas d'import IA public |
| **Concurrents FR (Zelty, Innovorder, L'Addition, Tiller)** | — | — | — | — | Rien de public trouvé. **Wello pourrait être parmi les premiers en France** |

**Ce qui revient partout :** relecture obligatoire, photos et PDF multipages, asynchrone accepté, allergènes et options traités comme des suggestions, service humain en complément.

### 2.2 Ce qu'on en retient

- Relecture obligatoire et brouillon : nous avons déjà l'aperçu à blanc et l'assistant, c'est notre avance.
- Les concurrents modélisent des « menus » partagés et programmables. Nous n'en avons pas besoin pour l'onboarding (objet Carte abandonné).

---

## 3. Existant Wello Resto

### 3.1 Réutilisable

| Brique | Où | Ce qu'elle apporte |
|---|---|---|
| Modèle canonique d'import | [importer/models.go](../internal/modules/menu/importer/models.go) | Catégories, tags, produits (prix par canal, TVA brute par canal), groupes d'options, rattachement produit → options |
| Aperçu à blanc | `POST /menu/import/preview`, [phase 4](import-produits-phase4-preview.md) | Lectures seules, confrontation à l'existant, décisions proposées |
| Validation | `POST /menu/import/commit`, [phase 5](import-produits-phase5-commit.md) | Transaction unique, décisions revérifiées, blocages explicites |
| Porte JSON manuelle | [import_service.go:128](../internal/modules/menu/import_service.go#L128) | Modèle pour la porte IA |
| Assistant de relecture | `wello-back-office/src/components/menu/import/` | Porte, sélection, collisions, catégories, tags, TVA, avertissements |
| Page produits | `wello-back-office/src/pages/Menu.tsx` (`/menu/products`) | Ouvre déjà l'assistant d'import |
| Création de groupes | [repository.go:2591](../internal/modules/menu/repository.go#L2591) `insertProductTx` (écrit `is_product_group`) ; rattachement par `UPDATE products SET by_product_of` ([repository.go:4313](../internal/modules/menu/repository.go#L4313)) | Chemin d'écriture existant, réutilisable par le commit |
| Registre IA | [internal/ai/](../internal/ai/) | Tâches, coupe-circuit, métriques |
| Envoi de fichiers | Module HACCP (plusieurs photos, `MaxBytesReader`) ; R2 : `UploadPrivateFile`, `GenerateSignedURL`, `DeleteFile` | Modèle d'envoi, stockage privé, affichage de la photo source en relecture |
| Tâche de fond | Motif « goroutine + `recover` + 202 » ([admin/upsell_handler.go](../internal/modules/admin/upsell_handler.go)) ; tâches planifiées ([cmd/api/tasks.go](../cmd/api/tasks.go)) | Traitement asynchrone et nettoyage |
| Routes staff Wello | `/admin/*` sous `middleware.RequirePlatformAdmin` (`users.is_platform_staff`) | Recharge de crédits |

### 3.2 Manquant

| Manque | Détail |
|---|---|
| Modèle par tâche | `AI_TASK_*_MODEL` est **ignoré** aujourd'hui (§ 5.3) |
| Image dans la couche IA | `CompletionRequest` n'a que du texte |
| Réponse des modèles actuels | Lecture de `Content[0]` seulement |
| Brouillon durable | Snapshot Redis de 30 min |
| Groupes dans l'import | `CanonicalProduct` n'a pas de parent |
| TVA par produit en relecture | Les décisions ne portent qu'une table taux → `tva_id` (`TvaMapping`), pas d'ajustement produit par produit |
| Sous-produits sur les plateformes | § 5.8 |
| Point d'entrée après inscription | Le tunnel se termine par `navigate('/')` sur un tableau de bord sans checklist d'onboarding |

---

## 4. Démarche retenue

La lecture de photo est une quatrième porte de l'import produits, sans changement du modèle de données produit. L'IA respecte l'invariant du paquet `importer` : « le parser ne décide rien ». Elle *propose* (groupes, nature du produit pour la TVA, rattachement des options), et le marchand tranche en relecture.

---

## 5. Cadrage technique

### 5.1 Flux

```
Back-office : porte « Photos de ma carte »
  │  1 à 10 photos normalisées dans le navigateur (JPEG, orientation corrigée, ≤ 2576 px)
  ▼
POST /menu/import/ai               → 202 { draft_id }     (crédit vérifié ; fichiers → R2 privé)
  │
  ▼  traitement asynchrone (goroutine + recover)
  ├─ 1 appel IA par photo, 3 en parallèle max
  ├─ fusion des pages (catégories et groupes d'options par nom, doublons signalés)
  ├─ BuildAIMenuImport(json) → *IntermediateImport     (pur, dans importer/)
  └─ BuildPreview(...)       → PreviewResult           (existant)
  │
  ▼
GET /menu/import/ai/{draft_id}     → { status, pages_done, pages_total, preview }
  │
  ▼  relecture : assistant existant + photo source par ligne + étapes « Groupes » et « Nature / TVA »
POST /menu/import/commit           → existant, jeton pointant vers le brouillon
```

**Pourquoi un appel par page** (conséquence des recherches, § 8.4) :
- Une grande carte (jusqu'à ~290 produits sur staging) produirait 20 000 jetons de sortie ou plus en un seul appel. C'est plusieurs minutes, avec un risque de troncature (`max_tokens`).
- Par page, les sorties restent courtes, les pages tournent en parallèle, et une page ratée se relance seule, gratuitement.
- La consigne système est identique à chaque appel, donc mise en cache (lectures facturées à 10 %).

### 5.2 Contrat de sortie de l'IA (par page)

La sortie est contrainte par `output_config.format` (schéma JSON), puis revalidée côté Go.

**Contraintes du schéma**, d'après la doc Anthropic :
- `additionalProperties: false` sur chaque objet ;
- champ nullable = `anyOf` avec `null` ;
- enums de scalaires uniquement ;
- pas de `minimum`, `maxLength`, `pattern`, `maxItems`, ni de schéma récursif.

Les bornes (prix ≥ 0, etc.) sont donc vérifiées en Go.

```jsonc
{
  "page": 1,
  "categories": [{ "ref": "c1", "name": "Boissons" }],
  "product_groups": [                  // proposés si ≥ 2 déclinaisons d'une même base
    { "ref": "grp1", "category_ref": "c1", "name": "Coca-Cola" }
  ],
  "products": [{
    "ref": "p1",
    "category_ref": "c1",
    "group_ref": "grp1",               // null = à la racine
    "name": "Coca-Cola Zero",
    "description": "",
    "price_cents": 350,                // null si illisible ou absent
    "option_group_refs": [],
    "kind": "soft_drink_sealed",       // enum § 5.7 → proposition de TVA
    "confidence": "high",              // high | medium | low
    "issues": []
  }],
  "option_groups": [{
    "ref": "g1", "name": "Suppléments", "min": 0, "max": 5,
    "options": [{ "title": "Cheddar", "extra_price_cents": 100 }]
  }],
  "formulas": [{ "name": "Menu midi", "price_cents": 1590, "description": "Entrée + plat" }],  // signalées, jamais créées
  "warnings": ["bas de page coupé"]
}
```

`BuildAIMenuImport` fusionne les pages et traduit vers le canonique. Les `ExternalID` sont synthétiques (`ai-<draft>-p<page>-<ref>`) ; `confidence` et `issues` deviennent des avertissements de preview.

### 5.3 Couche IA : changements nécessaires

Constats faits dans le code, pas seulement dans la doc :

| # | Constat | Conséquence | Correction |
|---|---|---|---|
| 1 | Les providers sont construits **une seule fois avec un modèle vide** ([routes.go](../cmd/api/routes.go), `buildAIRegistry`), et `CompletionRequest` n'a pas de champ modèle | **`AI_TASK_*_MODEL` n'a aucun effet** : traduction et upsell tournent toujours sur `claude-haiku-4-5`, défaut du provider. `CLAUDE.md` laisse croire le contraire | Ajouter `Model` (et `Effort`) à la requête, alimentés par `TaskConfig` ; le provider les envoie tels quels |
| 2 | La réponse est lue dans `Content[0].Text` | Sur Opus 5.5 et Sonnet 5.5, le premier bloc est un bloc de réflexion, donc **réponse vide** | Concaténer tous les blocs `text` |
| 3 | `temperature` est envoyée quand elle est > 0 | Les modèles actuels la **rejettent (400)**. Sans effet pour `menu_ocr`, qui n'en fixe pas. Mais la traduction la code **en dur à 0,3** ([translation/service.go:369](../internal/modules/translation/service.go#L369)) : passer la traduction sur un modèle récent casserait | Pour `menu_ocr` : ne pas la fixer. Traduction : la lire depuis la config (hors périmètre, à signaler) |
| 4 | Délai de 30 s par provider | Insuffisant pour une page dense avec réflexion | Délai propre à la tâche (`AI_TASK_MENU_OCR_TIMEOUT_MS`, 180 s) |
| 5 | Pas d'image | — | Blocs `image` (base64 JPEG), image **avant** le texte comme le recommande la doc |
| 6 | JSON obtenu par consigne + retrait des balises de code | Fragile | `output_config.format` (`json_schema`), disponible sur **tous les modèles Claude actuels, Haiku 4.5 compris** |
| 7 | `stop_reason` non lu | Troncature ou refus silencieux | `max_tokens` → page en échec, relançable ; `refusal` → échec propre, crédit non consommé |

**Décision Q2, vérifiée :** une fois les points 1, 2 et 7 corrigés, **changer de modèle Claude se fait uniquement par variables d'environnement.** Tous les modèles actuels gèrent l'image et les sorties structurées. Les réglages ne sont envoyés que s'ils sont renseignés :

| Variable | Valeur de départ | Note |
|---|---|---|
| `AI_TASK_MENU_OCR_PROVIDER` | `anthropic` | |
| `AI_TASK_MENU_OCR_MODEL` | `claude-opus-5-5` | |
| `AI_TASK_MENU_OCR_EFFORT` | `medium` | Vide = non envoyé (Haiku 4.5 refuse `effort`) |
| `AI_TASK_MENU_OCR_MAX_TOKENS` | 16000 | Par page |
| `AI_TASK_MENU_OCR_TIMEOUT_MS` | 180000 | |
| `AI_TASK_MENU_OCR_ENABLED` | `false` puis `true` | Coupe-circuit |

**Seule limite :** repasser sur un modèle *standard* (Haiku 4.5, long côté 1568 px) réduit la définition vue par le modèle. C'est automatique côté API, sans code, mais les petits caractères peuvent en pâtir.

### 5.4 Fichiers en entrée (Q1)

- **Photos.** Le navigateur normalise avant l'envoi : décodage, orientation appliquée, redimensionnement à 2576 px de long côté (la définition maximale d'Opus 5.5 ; au-delà l'API réduit de toute façon), ré-encodage JPEG qualité ~0,9.
  - *HEIC* : sans objet. Safari convertit automatiquement en JPEG quand le champ n'accepte que `image/jpeg,image/png`, et la normalisation couvre les autres cas.
  - *Orientation* : Claude **ne lit pas les métadonnées**, donc une photo « droite » par son EXIF lui arriverait tournée. La normalisation le règle.
- **PDF** : non accepté (retiré du périmètre).
- **Limites.**
  - 10 photos, 20 Mo au total après normalisation, `MaxBytesReader` comme HACCP.
  - Le serveur n'accepte que du JPEG (vérification du type réel, pas de l'extension).
  - Côté API : 10 Mo base64 par image, 32 Mo par requête ; un appel par photo reste très en dessous.

### 5.5 Brouillon durable

Table `menu_import_drafts` (migration Postgres dans `migrations/todo/`) :

| Colonne | Rôle |
|---|---|
| `id` uuid, `merchant_id`, `created_by` | Rattachement et traçabilité (marchand ou staff Wello, Q10) |
| `source` | `ai_photo` |
| `status` | `pending` → `processing` → `ready` \| `failed` → `committed` \| `expired` |
| `consumes_credit` bool | Faux si échec technique ou refus (Q9) |
| `pages` jsonb | Par page : clé R2, statut, sortie IA validée, erreur, usage (jetons, latence, modèle) |
| `snapshot` jsonb | Équivalent durable du snapshot Redis |
| `error`, `created_at`, `updated_at`, `expires_at` | `expires_at` = création + 30 jours (Q8) |

**Robustesse**
- **Reprise.** Au démarrage de l'API, puis toutes les 5 min (tâche planifiée), un brouillon resté en `processing` plus de 10 min passe en `failed` avec `consumes_credit = false`. Les pages déjà réussies sont conservées et la relance ne refait que les autres.
- **Plusieurs instances.** Les tâches planifiées n'ont aucun verrou distribué, ce qui laisse penser que l'API tourne sur une seule instance. La conception n'en dépend pas : le passage `pending` → `processing` se fait par `UPDATE … WHERE status = 'pending'` (une seule instance le gagne).
- **Purge.** Les lignes ne sont jamais supprimées (décompte des crédits). Les photos R2 sont purgées 30 jours après validation ou expiration, par la même tâche.

### 5.6 Crédits (Q9)

- **10 par marchand**, défaut fixé par `AI_MENU_OCR_DEFAULT_CREDITS`, surcharge par marchand. **Pas de limite journalière.**
- Consommés = brouillons `ai_photo` avec `consumes_credit = true`. Pas de registre séparé.
- **Une extraction en cours à la fois** par marchand (409 sinon). Plus de crédit : 402.
- **Gratuit :** un échec technique, un refus de l'IA, la relance d'une page en échec.
- **Recharge :** `PUT /admin/merchants/{id}/menu-ocr-credits`, dans le groupe `/admin` existant protégé par `RequirePlatformAdmin`.

### 5.7 Nature du produit et TVA (Q5)

L'IA indique une **nature** ; `BuildAIMenuImport` en déduit des **taux proposés** par canal. Le marchand confirme ou change la nature en relecture.

| `kind` | Exemples | Sur place | À emporter | Livraison |
|---|---|---|---|---|
| `food` | plats, sandwichs, desserts servis | 10 % | 10 % | 10 % |
| `hot_drink` | café, thé | 10 % | 10 % | 10 % |
| `soft_drink_served` | soda au verre, jus pressé, eau en carafe | 10 % | 10 % | 10 % |
| `soft_drink_sealed` | canette, bouteille capsulée | 10 % | 5,5 % | 5,5 % |
| `packaged_food` | pâtisserie emballée, bocal, sous vide (consommation différée) | 10 % | 5,5 % | 5,5 % |
| `alcohol` | bière, vin, cocktails, spiritueux | 20 % | 20 % | 20 % |
| `other` | indéterminé | — | — | — (à saisir) |

Sources : BOFiP via les guides cités en fin de note. **Table validée le 2026-09-29.**

**Implémentation**
- Les taux proposés alimentent les champs canoniques existants `TvaRateIn/TakeAway/Delivery`. La table taux → `tva_id` (`TvaMapping`) fait le reste, comme pour Zelty.
- Il faut **une nouvelle décision** « nature par produit », modifiable en relecture, qui recalcule les taux.
- Il faut **un nouveau blocage** `tva_not_confirmed` propre à la porte IA : rien n'est validable tant que l'étape « Nature / TVA » n'est pas confirmée.

### 5.8 Correctif préalable : sous-produits sur Uber Eats et Deliveroo (Q3)

**Constat confirmé par le code.**
1. L'envoi vers les plateformes (`SyncUberEatsMenu`, `SyncDeliverooMenu`) passe par `GetMenuWithMarketingCategories`, qui appelle `GetMenu`.
2. `GetMenu` **retire les enfants du premier niveau** (`LEFT JOIN products subp … WHERE subp.product_id IS NULL OR …`, [repository.go:1110](../internal/modules/menu/repository.go#L1110)) et les range dans `SubProducts` du parent ([repository.go:1510](../internal/modules/menu/repository.go#L1510)).
3. [mapper_ubereats.go](../internal/modules/menu/mapper_ubereats.go) et [mapper_deliveroo.go](../internal/modules/menu/mapper_deliveroo.go) ne parcourent que `cat.Products`.
4. Résultat : **le parent part comme un article vendable, les enfants ne partent jamais.**

**Mesure sur staging** (non représentatif, ordre de grandeur) :
- 95 groupes et 363 sous-produits, répartis sur 8 marchands.
- Les 363 sous-produits sont marqués pour Uber Eats et Deliveroo, et aucun n'est envoyé.
- Les 95 parents sont envoyés comme articles, à des prix de 0 à 12 €.

**Correctif décidé :** aligner les deux mappers sur le kiosk (`flattenKioskProducts`) : on envoie les enfants, jamais le parent. C'est un livrable indépendant, **à livrer en premier, avant d'activer le regroupement dans l'import**.

**Effet visible pour les marchands concernés.** L'envoi vers les plateformes est **manuel** : `PATCH /menu/uber-eats/sync` et `/menu/deliveroo/sync`, déclenchés depuis le back-office ; `menu_change_notifier` ne fait qu'invalider le cache et diffuser en temps réel. Au prochain envoi après le correctif, les articles « groupe » disparaîtront de leur carte Uber Eats ou Deliveroo et les sous-produits apparaîtront. Il faut prévenir ces marchands, ou vérifier avec eux que les sous-produits sont prêts à être vendus (prix livraison, photos). Pour mesurer l'impact en production :

```sql
SELECT COUNT(*) AS sous_produits_non_envoyes, COUNT(DISTINCT c.merchant_id) AS marchands
FROM products c JOIN products p ON p.product_id = c.by_product_of
WHERE c.enabled AND c.status <> 'removed_from_menu' AND (c.sync_uber_eats OR c.sync_deliveroo);
```

### 5.9 Groupes de produits (Q3)

- **Règle :** l'IA propose un groupe dès qu'**au moins deux produits sont des déclinaisons d'une même base** : parfums ou variétés (Coca-Cola → Zero, Cherry, Light, Vanille) comme tailles ou contenances (Reine 26 / 33 cm). **Un produit sans déclinaison reste à la racine.**
- Le parent (`is_product_group = true`) n'a **pas de prix** ; chaque enfant porte son prix et sa TVA. Le parent reçoit la catégorie et la TVA de ses enfants, parce que `GetMenu` fait une jointure stricte sur `tva_categories`.
- **Import :**
  - `CanonicalProduct` gagne `ParentExternalID` et un marqueur de groupe.
  - Le commit crée les parents avant les enfants (`insertProductTx`), puis rattache les enfants (`by_product_of`) dans la même transaction.
- **Relecture :** une étape « Groupes » permet de dégrouper, de déplacer un produit ou de renommer le parent.
- **Affichage par canal :** le POS Flutter gère les groupes (`sub_products_dialog.dart`) ; le kiosk et Scan&Order affichent les enfants à plat ; Uber Eats et Deliveroo aussi, après le correctif du § 5.8.

### 5.10 Coût et latence

- **Tarifs Opus 5.5 :** 4 $ / 20 $ par million de jetons (entrée / sortie) ; lectures de cache à 0,20 $.
- **Coût d'une image :** `⌈largeur/28⌉ × ⌈hauteur/28⌉` jetons, plafonné à 4 784 jetons (2576 px de long côté).

| Cas | Entrée | Sortie (réflexion comprise) | Coût estimé |
|---|---|---|---|
| Carte type : 4 photos, ~100 produits | 4 × (4 784 + ~3 000 de consigne, en cache après le 1er appel) ≈ 25 000 | ≈ 10 000 | **≈ 0,3 $** |
| Grande carte : 8 pages, ~290 produits | ≈ 50 000 | ≈ 25 000 | ≈ 0,7 $ |

- **Budget :** 10 crédits font au plus 3 à 7 $ par marchand.
- **Latence :** de l'ordre de 30 à 90 s par photo, photos en parallèle. **À mesurer** pendant les tests (§ 8.6).
- **Taille des cartes sur staging :** médiane 2 produits (comptes de test), 90e centile 146, maximum 288. Pour la production :

```sql
SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY n), percentile_cont(0.9) WITHIN GROUP (ORDER BY n), MAX(n)
FROM (SELECT merchant_id, COUNT(*) n FROM products WHERE enabled AND status <> 'removed_from_menu' GROUP BY merchant_id) t;
```

### 5.11 Endpoints (permission `HasMenuAccess`, sauf mention)

| Méthode | Route | Rôle |
|---|---|---|
| POST | `/menu/import/ai` | Envoi ; 202 + `draft_id` ; 402 sans crédit ; 409 si une extraction est déjà en cours |
| GET | `/menu/import/ai/{id}` | Statut par page, puis preview |
| GET | `/menu/import/drafts` | Brouillons en cours + crédits restants |
| POST | `/menu/import/ai/{id}/retry` | Relance des pages en échec (gratuite) |
| DELETE | `/menu/import/drafts/{id}` | Abandon |
| POST | `/menu/import/commit` | Existant |
| PUT | `/admin/merchants/{id}/menu-ocr-credits` | Recharge, `RequirePlatformAdmin` |

### 5.12 Back-office

- Quatrième carte dans `ImportDoorPicker` : « J'ai des photos de ma carte », crédits restants affichés.
- Champ fichier `accept="image/jpeg,image/png"` (conversion HEIC par Safari), prise de photo possible sur mobile, normalisation dans le navigateur (§ 5.4).
- Écran d'attente avec progression par page, reprise possible plus tard.
- **Relecture :**
  - photo source à côté de chaque ligne (URL R2 signée) ;
  - lignes à faible confiance mises en avant ;
  - nouvelles étapes « Groupes » et « Nature / TVA » ;
  - récapitulatif final (« N produits, prix de X à Y € »).
- **Points d'entrée (Q11) :**
  - la page produits `/menu/products` ;
  - un **encart sur le tableau de bord** (`/`), là où arrive le marchand en fin de tunnel, affiché tant qu'il n'a aucun produit.

---

## 6. Risques

| Risque | Parade |
|---|---|
| Erreur de prix non vue en relecture | Photo source par ligne, lignes à faible confiance en avant, récapitulatif final |
| Regroupement erroné | Simple proposition ; étape « Groupes » ; parent sans prix |
| Mauvaise TVA proposée | Nature modifiable, étape de confirmation obligatoire, table validée |
| Photo floue, tournée ou trop lourde | Normalisation dans le navigateur ; relance par page ; conseils de prise de vue |
| Page tronquée | Un appel par page, `max_tokens` détecté, relance ciblée |
| Extraction interrompue par un redémarrage | Reprise automatique (§ 5.5), crédit non consommé |
| Doublons avec l'existant | Étape collisions existante ; le nom fait foi |
| Régression de la traduction ou de l'upsell en corrigeant la couche IA | Corrections rétrocompatibles avec Haiku 4.5 ; revérifier les deux fonctionnalités |

---

## 7. Décisions

| # | Sujet | Décision |
|---|---|---|
| Q1 | Formats et volume | **Photos uniquement** (JPEG/PNG, HEIC converti par le navigateur, normalisées en JPEG) ; 10 photos, 20 Mo au total. PDF retiré |
| Q2 | Modèle | **Opus 5.5 en effort `medium`**, modèle et réglages **uniquement par variables d'environnement** (§ 5.3). On teste, puis on avise |
| Q3 | Groupes | Regroupement proposé dès 2 déclinaisons, produit seul à la racine, modifiable en relecture (§ 5.9) ; **correctif préalable des mappers** (§ 5.8) |
| Q4 | Formules | Signalées, jamais créées |
| Q5 | TVA | Nature proposée par l'IA → taux proposés → confirmation obligatoire par le marchand (§ 5.7) |
| Q6 | Suppléments et options | Rattachés si la carte le dit explicitement, sinon non rattachés |
| Q7 | Allergènes | Pas maintenant ; plus tard, suggestions non cochées tirées de la carte uniquement |
| Q8 | Conservation | Brouillon 30 jours ; photos purgées 30 jours après validation ou expiration |
| Q9 | Crédits | 10 par marchand, sans limite journalière, rechargeables par le staff ; une extraction à la fois ; échecs non décomptés |
| Q10 | Self-service ou accompagné | Self-service + staff Wello, tracé par `created_by` (§ 8.5) |
| Q11 | Points d'entrée | Page produits + encart sur le tableau de bord après l'inscription |
| Q12–Q16 | Objet Carte | **Sans objet** : retiré du périmètre |

---

## 8. Réponses aux questions ouvertes (recherches du 2026-09-29)

### 8.1 Le défaut des mappers est-il réel ?
**Oui**, confirmé par le code et visible dans les données de staging (§ 5.8). Il touche déjà les groupes créés à la main. Une requête SQL permet d'en mesurer l'impact en production.

### 8.2 Le modèle peut-il changer par variable d'environnement sans autre adaptation ?
**Oui, après une correction qui est de toute façon nécessaire.** Aujourd'hui, `AI_TASK_*_MODEL` n'a aucun effet (§ 5.3, point 1). Une fois le modèle transmis à chaque requête et la réponse lue sur tous les blocs, tout modèle Claude actuel se branche par variable : tous gèrent l'image et les sorties structurées ([doc Structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)).

### 8.3 La correction de la couche IA casse-t-elle la traduction et l'upsell ?
**Non, si elle reste rétrocompatible.**
- Lire tous les blocs `text` donne le même résultat sur Haiku 4.5.
- Transmettre le modèle **fera réellement appliquer** `AI_TASK_MENU_TRANSLATION_MODEL` et `AI_TASK_UPSELL_MODEL`. Il faut vérifier que ces variables ne sont pas renseignées en production avec un autre modèle que Haiku, sinon le comportement changera au déploiement.
- La température de la traduction est codée en dur : à signaler, sans la toucher ici.

### 8.4 Faut-il découper par page ?
**Oui.**
- Les cartes vont jusqu'à ~290 produits sur staging, soit plus de 20 000 jetons de sortie en un appel.
- La doc limite les requêtes à 32 Mo et les images à 10 Mo (base64). Au-delà de 20 images par requête, chaque image est limitée à 2000 px.
- Un appel par page évite ces trois limites, raccourcit la latence grâce au parallélisme, et permet de relancer une seule page.

### 8.5 Comment le staff Wello importe-t-il pour un marchand ?
- Il n'existe **pas d'usurpation d'identité**.
- Le staff doit être **membre de l'établissement** (multi-comptes, voir `MULTI_ACCOUNT_*.md`). Il importe alors avec son propre compte, tracé dans `created_by`.
- La recharge de crédits passe par `/admin`, protégé par `RequirePlatformAdmin`.

### 8.6 Comment valider la qualité ?
- **Décision :** tests personnels d'Ilies **sur staging** (`AI_TASK_MENU_OCR_ENABLED=true` sur staging uniquement), avant l'ouverture en production. La qualité d'extraction dépend de la photo, pas des données de la base : staging suffit pour ces tests.
- **À noter pendant les tests :** produits retrouvés, prix exacts, catégories, groupes, nature/TVA, coût et latence par photo. Le brouillon enregistre déjà l'usage (jetons, latence, modèle) par photo.
- **Méthode suggérée :** photographier la carte d'un marchand déjà bien configuré, et comparer à sa carte en base.

### 8.7 Le traitement en tâche de fond est-il sûr ?
**Oui, avec la reprise du § 5.5.** Le motif existe déjà (goroutine + `recover` + 202). La conception ne dépend pas du nombre d'instances.

### 8.8 HEIC, orientation, taille des photos ?
**Réglés dans le navigateur** (§ 5.4) :
- Safari convertit le HEIC en JPEG quand le champ n'accepte que JPEG ou PNG ;
- Claude ignore les métadonnées, donc l'orientation doit être appliquée avant l'envoi ;
- au-delà de 2576 px, l'image est de toute façon réduite par l'API.

### 8.9 Taux de TVA à proposer ?
Voir le tableau du § 5.7 :
- 10 % pour la consommation immédiate ;
- 5,5 % pour les boissons fermées et aliments conditionnés, à emporter et en livraison ;
- 20 % pour l'alcool, quel que soit le mode.

**Table validée le 2026-09-29.**

La table `tva_categories` est globale : chaque `tva_id` correspond à un couple taux + canal, ce qui explique plusieurs identifiants pour un même taux. Le résolveur de la preview (`newTvaResolver`, `resolve(rate, channel)`) gère déjà ce cas.

### 8.10 Arbitrages du 2026-09-29
1. Validation de la qualité : tests personnels (§ 8.6).
2. PDF : retiré, photos uniquement.
3. Variables de modèle de la traduction et de l'upsell en production : vérifiées par Ilies avant le déploiement de la couche IA.
4. Table de TVA : validée.

### 8.11 Doutes restants (aucun bloquant)
1. **Qualité et latence réelles d'Opus 5.5** sur des photos de cartes : inconnues avant les tests. Les estimations du § 5.10 sont des ordres de grandeur.
2. **La `confidence` est déclarée par le modèle**, pas calibrée. C'est un indice pour attirer l'œil en relecture, pas une garantie : la relecture reste obligatoire sur toutes les lignes.
3. **Fusion entre photos.** Deux photos qui se chevauchent produisent des doublons. La fusion les signale (même nom, même prix) sans les supprimer d'office. Une catégorie coupée entre deux photos est fusionnée par nom.
4. **Effet du correctif des mappers** sur les marchands qui ont déjà des groupes (§ 5.8) : à communiquer.
5. **Consigne système** : elle sera ajustée pendant les tests. Chaque modification change le cache de la consigne, sans autre effet.

---

## 9. Découpage

1. **Correctif des mappers Uber Eats et Deliveroo** (§ 5.8). Indépendant, à livrer en premier.
2. **Couche IA** : modèle et effort par tâche, lecture des blocs, raisons d'arrêt, image, sorties structurées, délai par tâche, tâche `menu_ocr`. Revérifier traduction et upsell.
3. **Porte IA côté API** : schéma, appels par photo, fusion, `BuildAIMenuImport`, groupes et nature dans le canonique et le commit, blocage `tva_not_confirmed`, table `menu_import_drafts`, crédits, reprise, endpoints.
4. **Back-office** : porte, normalisation des photos, attente, relecture (photo source, étapes « Groupes » et « Nature / TVA »), crédits, encart sur le tableau de bord.
5. **Tests personnels sur staging** (§ 8.6), coupe-circuit fermé en production.
6. **Ouverture en production** : `AI_TASK_MENU_OCR_ENABLED=true`.

---

## Sources

- Anthropic : [Vision](https://platform.claude.com/docs/en/build-with-claude/vision) · [Structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)
- HEIC sur iOS : [Hacker News — conversion Safari](https://news.ycombinator.com/item?id=23268189) · [Coping with HEIC in the browser](https://shkspr.mobi/blog/2020/12/coping-with-heic-in-the-browser/)
- TVA restauration : [hr-associes.fr](https://www.hr-associes.fr/blog/tva-restauration-taux-sur-place-emporter-livraison-alcool) · [extencia.fr](https://www.extencia.fr/tva-restauration-2026-guide-taux) · [indy.fr — vente à emporter](https://www.indy.fr/guide/fiscalite/taxes/tva/restauration-a-emporter/) · [l-expert-comptable.com](https://www.l-expert-comptable.com/a/530955-quelle-tva-pour-les-restaurants.html)
- Lightspeed K-Series : [Importing and exporting items in bulk](https://k-series-support.lightspeedhq.com/hc/en-us/articles/1260804656109-Importing-and-exporting-items-in-bulk) · [Capterra — onboarding](https://www.capterra.com/p/211849/Lightspeed-Resturant/)
- Square : [Create and update menus](https://squareup.com/help/us/en/article/6424-create-menus-with-square-for-restaurants)
- Toast : [Toast IQ](https://pos.toasttab.com/products/toast-iq) · [OpenTable — upload menus](https://support.opentable.com/s/article/How-To-Update-Your-Menus-in-GuestCenter?language=en_US) · [Menu Checkpoint](https://menucheckpoint.com/)
- Outils dédiés : [SmartlyMenu](https://smartlymenu.com/features/ai-menu-import) · [ClickyMenu](https://clickymenu.com/features/ai-menu-upload) · [IAMenu — comparatif 2026](https://www.iamenu.ai/en/best-ai-menu-software-2026) · [Foodvizer — comparatif](https://foodvizer.com/comparatif/)
- Uber Eats : [AI menus (MobileSyrup)](https://mobilesyrup.com/2025/07/31/uber-eats-ai-menus-food-images-reviews/) · Deliveroo : [FAQ partenaires](https://merchants.deliveroo.com/faqs)

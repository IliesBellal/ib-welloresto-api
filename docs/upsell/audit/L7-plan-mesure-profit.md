# Livrable 7 — Plan de mesure du gain incrémental

**Révisé le 2026-10-16** avec les données de prod ([L9](L9-resultats-baseline-prod.md)) :
objectif arbitré = **CA**, le moins de friction possible ; ~280 sessions upsell
par semaine sur SNO et borne ; aucun coût saisi pour les produits suggérés.
Ce livrable regroupe la mesure (§1 à §5), les plans de test (§6-§7) et
l'évaluation économique (§8).

## 1. Définitions

Périmètre de mesure : **SNO et borne**. Le POS en est exclu tant que U-27
n'est pas livré (appels sans affichage, suivi perdu en mise à jour).

| Terme | Définition opérationnelle |
|---|---|
| Session éligible | Appel upsell SNO ou borne, panier non vide, upsell activé, client instrumenté (`client_tracks_exposure = true`), **quel que soit le bras** (témoin compris) |
| Session exposée | Session éligible hors témoin dont la liste renvoyée n'est pas vide |
| Commande payée liée | Commande créée avec l'identifiant de la session dans les 30 minutes, clôturée et non annulée |
| Abandon | Session éligible sans commande payée liée |
| CA de session | CA HT de la commande payée liée ; **0 en cas d'abandon** |
| CA upsell de session | CA HT des lignes `is_upsell` de la commande payée liée |
| **CA incrémental par session éligible (CISE)** | moyenne(CA de session \| bras traité) − moyenne(CA de session \| témoin), avec intervalle de confiance à 95 % (bootstrap par session, stratifié par établissement) |
| Marge (secondaire) | (prix HT − coût figé) × quantité, calculable seulement pour les lignes dont le coût est connu, toujours avec son taux de couverture (0 % aujourd'hui pour les produits suggérés) |

À ce volume, le CISE sur le panier entier est **trop bruité pour conclure**
(un effet de 0,10 € par session sur un panier qui varie de plusieurs euros).
On le lit donc par ses trois composantes, chacune mesurable séparément :

1. **CA upsell par session** (faible variance) ;
2. **taux d'abandon** (binaire) × panier moyen ;
3. **cannibalisation** : sur SNO et la borne, le popup arrive une fois le
   panier arrêté et ne propose que des produits absents du panier ; elle se
   limite donc aux achats que le client aurait faits plus tard (seconde
   commande à table sur SNO). À surveiller : part des sessions suivies d'une
   seconde commande dans l'heure, par bras.

Restitution : **euros pour 1 000 sessions éligibles**, par canal et par
établissement.

## 2. Situation de référence (mesurée, 2026-09-28 → 2026-10-16)

| Indicateur | Valeur | Source |
|---|---|---|
| Sessions SNO + borne | 683 (≈ 280 par semaine) | B1 |
| Sessions avec au moins un ajout | 45 suivies (6,6 %), dont **37 payées (5,4 %)** ; de 4,4 % (226 borne) à 11,9 % (231 borne) en suivi | B1, B8 |
| CA upsell | 83,17 € TTC suivis, dont **69,27 € payés, soit 101 € pour 1 000 sessions** ; 1,87 € par session avec ajout payé | B1, B8 |
| Part du CA SNO + borne | 0,5 % à 1,8 % selon l'établissement | B1, B7 |
| Panier moyen SNO + borne | 15,8 € TTC (526 commandes clôturées) | B7 |
| Listes non vides | 99,8 % | B6 |
| Latence p95 | 690 à 835 ms ; 0,4 % au-delà de 2 s | B5 |
| Acceptations sans commande payée | 8 sur 45 : 4 annulées par le personnel (226 borne, semaine du lancement), 2 refusées automatiquement (226 SNO), 2 annulées par le client | B8 |
| Abandon après l'étape | **inconnu** (aucun témoin) | — |

`api_request_logs` garde 90 jours de corps d'appels upsell (paniers exposés),
utilisables pour un rejeu hors ligne, jamais pour conclure.

**Comparaison avant/après activation** : sans valeur ici. B7 ne montre aucun
mouvement du panier moyen (226 borne 15,73 € → 16,00 €, 226 SNO 23,67 € →
23,53 €), ce qui est attendu pour un effet de 0,10 € par session.

## 3. Événements à collecter

Une ligne `upsell_suggestions` par session, enrichie
([L8 R1](L8-recommandations-techniques.md#r1--journal-dexposition-et-indicateurs-fiables)) :

| Champ | Contenu | Existe |
|---|---|---|
| `id`, `merchant_id`, `channel`, `created_at` | identité de la session | oui |
| `order_type` | sur place, emporter, livraison | **non** |
| `session_key` | clé stable envoyée par le client | **non** |
| `policy`, `experiment_key` | bras et sel de l'expérience | **non** |
| `engine_version` | version des règles | **non** |
| `cart_product_ids` | produits du panier au moment de l'appel | **non** (empreinte seulement) |
| `suggested_items` | liste renvoyée, avec `origin`, `score` brut, `reason_product_id` | partiel |
| `returned_count` | articles renvoyés au client après tous les filtres | **non** |
| `latency_total_ms` | durée totale du moteur | **non** |
| `client_tracks_exposure` | le client envoie l'identifiant même sans ajout | **non** |
| `order_id` | commande liée, avec ou sans ajout | seulement en cas d'ajout |
| État de la commande liée | pour ne compter que les commandes payées (C-22) | via jointure |
| Lignes `is_upsell` de la commande | ajout au niveau de la ligne | oui |

Facultatif : durée passée sur l'étape et action de sortie, transmises avec la
commande. Mesure la friction des sessions qui aboutissent, pas des abandons.

## 4. Indicateurs

| Rôle | Indicateur |
|---|---|
| Principal | CA upsell par session éligible (CA HT des lignes `is_upsell` de commandes payées), lu avec l'abandon |
| Garde-fous | taux d'abandon (non-infériorité) ; délai médian appel → commande ; part de sessions suivies d'un nouvel appel (retour au panier) ; annulations client SNO et borne |
| Secondaires | sessions avec ajout ; articles par session avec ajout ; ajout par position et par origine ; CA de session total (indicatif) ; seconde commande dans l'heure (cannibalisation) |
| Qualité de la mesure | équilibre des bras ; part de sessions instrumentées ; acceptations sans commande payée |

Seuils proposés, à valider : abandon pas plus de +3 points (le seul seuil
détectable à ce volume, voir §7), délai médian pas plus de +15 s.

## 5. Groupes de contrôle

- **Témoin** : 20 % des sessions éligibles pendant 10 à 12 semaines (détection
  d'une dégradation forte), puis 10 % en permanence. Une seule
  `experiment_key` pour 212, 226 et 231, afin de mettre les sessions en commun.
- **Bras de test** : partage des sessions exposées entre la politique actuelle
  et une variante.
- **Unité** : la session, affectée par hachage déterministe
  `hash(merchant_id, session_key, experiment_key) mod 100`.
- **Stratification** : analyse par établissement puis agrégée.

## 6. Plans de test

| | T1 — Filet de sécurité | T2 — Classement | T3 — Friction : seuil | T4 — Friction : sans étape |
|---|---|---|---|---|
| Hypothèse | L'étape upsell ne dégrade pas l'abandon de plus de 3 points | Le classement unique calibré + diversité (`unified_v1`) augmente les sessions avec ajout | Sauter l'étape quand le CA espéré est faible réduit la friction sans perte notable de CA upsell | Des suggestions dans le panier, sans popup, gardent l'essentiel du CA upsell sans étape bloquante |
| Bras | témoin / exposé (moteur actuel) | actuel / `unified_v1` | toujours / seuil | popup / intégré |
| Unité | session | session | session | session |
| Population | SNO et borne, 212, 226, 231, clients instrumentés | sessions exposées | idem | idem |
| Allocation | 20/80 puis 10/90 | 50/50 dans le bras exposé | 50/50 | 50/50 |
| Principal | taux d'abandon | sessions avec ajout | CA upsell par session éligible | CA upsell par session éligible |
| Secondaires | CA upsell par session, délai | CA upsell par session, ajout par position | part d'étapes sautées, délai | délai, retours au panier |
| Garde-fous | — | abandon | abandon | abandon |
| Durée | au moins 10 semaines | au moins 14 jours et l'effectif du §7 | idem | idem |
| Succès | borne haute de la hausse d'abandon < 3 points | borne basse de l'écart > 0 | perte de CA upsell < moitié de la part d'étapes sautées, abandon non dégradé | CA upsell ≥ 60 % du popup (seuil à valider), abandon non dégradé |
| Ordre | dès que R1 et R2 sont en place | en parallèle de T1 (bras exposé) | après T2 | après T2, si l'objectif de friction prime |

**Biais et parades** : déséquilibre des bras (contrôle quotidien) ;
nouveauté (comparer les deux premières semaines aux suivantes) ; cache
(décision de bras avant le cache) ; clients anciens (filtrer sur
`client_tracks_exposure`) ; regards répétés (horizon fixé à l'avance) ; cycles
hebdomadaires (semaines complètes) ; hétérogénéité des établissements
(stratification ; 231 se comporte à l'opposé de 212 et 226 sur les
associations) ; un seul test par population à la fois, hormis T1 + T2 qui se
partagent les sessions (témoin d'un côté, bras exposés de l'autre).

## 7. Tailles d'échantillon (taux de base mesurés)

α = 5 % bilatéral, puissance 80 %, ~280 sessions éligibles par semaine.

| Cas | Hypothèse | Allocation | Sessions (total) | Durée |
|---|---|---|---|---|
| T2, sessions avec ajout payé +50 % | 5,4 % → 8,1 % | 50/50 | ≈ 2 700 | ≈ 10 semaines (12 si T1 garde 20 % de témoin) |
| T2, sessions avec ajout payé +30 % | 5,4 % → 7,0 % | 50/50 | ≈ 7 100 | ≈ 25 semaines |
| T1, abandon +3 points | 5 % → 8 % | 20/80 | ≈ 2 900 | ≈ 10 semaines |
| T1, abandon +3 points | idem | 10/90 | ≈ 4 900 | ≈ 18 semaines |
| Abandon +0,6 point (seuil de rentabilité de l'étape) | 5 % → 5,6 % | 50/50 | ≈ 44 000 | ≈ 3 ans |

Le taux d'abandon de base (5 %) est une hypothèse, à remplacer par celui du
témoin dès les premières semaines. Formules : n par bras = (1,96 + 0,84)² ×
(p₀q₀ + p₁q₁) / Δ² ; allocation déséquilibrée : N = (1,96 + 0,84)² ×
(p₁q₁/w₁ + p₀q₀/w₀) / Δ².

**Conséquence** : on ne prouvera pas, à ce volume, que le popup rapporte plus
qu'il ne coûte en abandons ; seuls les effets importants (+50 % d'ajouts, +3
points d'abandon) sont lisibles, en 10 à 12 semaines. La faible friction doit
être recherchée par construction (T3, T4), pas démontrée.

## 8. Évaluation économique

### Valeurs mesurées et hypothèses

| Symbole | Sens | Valeur | Origine |
|---|---|---|---|
| a | CA upsell attribué payé pour 1 000 sessions | 101 € TTC | **mesuré** (B1, B8) |
| i | part réellement incrémentale de ce CA | 0,7 à 1,0 | **hypothèse** (popup après panier arrêté, produits absents du panier ; seule une seconde commande peut cannibaliser) |
| d | hausse d'abandon due à l'étape | inconnue | à mesurer (T1), au mieux à ±3 points |
| B | panier moyen SNO + borne | 15,8 € TTC | **mesuré** (B7) |

**CA net pour 1 000 sessions = a × i − 1 000 × d × B**

| Scénario | i | d | CA upsell incrémental | CA perdu par abandon | **CA net / 1 000 sessions** |
|---|---|---|---|---|---|
| Favorable | 0,9 | 0 | 91 € | 0 € | **+91 €** |
| Central | 0,9 | +0,3 pt | 91 € | 47 € | **+44 €** |
| Défavorable | 0,7 | +0,5 pt | 71 € | 79 € | **−8 €** |
| Perte | 0,9 | +1 pt | 91 € | 158 € | **−67 €** |

Seuil d'équilibre : d = a × i ÷ (1 000 × B) ≈ **0,45 à 0,65 point**
d'abandon selon i. Un dixième de point d'abandon (−16 € pour 1 000 sessions)
efface presque un point d'ajout payé (+18,7 €).

### Effet des chantiers (hypothèses explicites)

Effets cumulés, ligne après ligne :

| Chantier | Effet supposé | CA upsell attribué / 1 000 | Écart cumulé / référence |
|---|---|---|---|
| Référence | — | 101 € | — |
| + U-05, U-10 (places remplies, diversité) | +10 % d'ajouts | 111 € | +10 € |
| + U-09 calibré (`unified_v1`) | +25 % d'ajouts | 139 € | +38 € |
| + U-12, U-22a (CA espéré, variante la plus vendue) | +10 % de CA par ajout | 153 € | +52 € |
| Variante : U-28 (sans étape), sur la référence | −40 % d'ajouts, abandon lié à l'étape ramené à 0 | 61 € | −40 € de CA attribué, mais plus aucun risque d'abandon lié à l'étape |

Les pourcentages sont des hypothèses de travail, pas des résultats ; seuls
des effets de cette taille seraient lisibles par un test (§7). U-28 n'est
préférable que si l'étape coûte plus de 0,25 point d'abandon (40 € ÷ 15,8 €).

### Enjeux, coûts, retour sur investissement

- Au rythme actuel (≈ 14 500 sessions par an) : CA upsell attribué payé ≈
  **1 500 € TTC par an**, tous établissements. L'ensemble des chantiers de
  classement ajouterait environ 750 € par an (52 € × 14,5) si les effets
  supposés se vérifiaient ; un point d'ajout payé vaut environ 270 € par an.
- Coûts de mise en œuvre (estimations d'audit, [L5](L5-backlog-opportunites.md)) :

  | Lot | Contenu | Effort |
  |---|---|---|
  | Mesure | U-01, U-04, U-05, U-06, U-27, U-03 | 3 à 4 semaines |
  | Classement | U-09, U-10, U-12, U-22a | 1 à 2 semaines |
  | Contrôle marchand | U-26 | 2 à 4 jours |
  | Friction | U-13 ; U-28 (deux clients) | 1 semaine ; 2 à 3 semaines |
  | Exploitation | lignes en base, Redis, aucun appel LLM | négligeable |

- **Délai de retour** = coût de développement ÷ gain annuel. Au volume actuel,
  le gain se compte en centaines d'euros de CA par an pour l'ensemble des
  restaurateurs : les chantiers ne se justifient économiquement que par
  l'**adoption** (plus d'établissements avec SNO, borne ou barre POS active)
  et par la valeur produit de la fonctionnalité. D'où la priorité donnée aux
  correctifs peu coûteux et à l'absence de friction, et le renoncement aux
  modèles adaptatifs.

## 9. Règles de décision

1. Plus de changement de classement en prod hors d'un bras de test une fois
   R2 en place.
2. Un test se conclut à l'horizon fixé ; un garde-fou franchi l'arrête
   immédiatement.
3. Une variante gagnante devient la politique par défaut ; le témoin
   permanent (10 %) reste en place.
4. Chaque décision est consignée dans [UPSELL_COMPLETION.md](../../UPSELL_COMPLETION.md)
   avec ses chiffres et son intervalle.

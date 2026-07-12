# Design Decisions — Refonte page de suivi de commande SNO (2026-07-02)

> Compagnon de l'audit [2026-07-02-order-tracking-page-refonte.md](2026-07-02-order-tracking-page-refonte.md)
> (version complète dans le repo SNO : `wello-resto-scannorder/docs/audits/`).
> Documente les choix créatifs de la refonte de `src/pages/OrderTrackingPage.tsx`
> côté SNO : ce qui a été retenu, ce qui a été écarté, et le sens porté par
> chaque micro-interaction. Contrat de données : [public-delivery-session.md](../api-contracts/public-delivery-session.md).

## 1. Architecture visuelle

- **Desktop (≥ 1024px / `lg`)** : side sheet gauche de **400px** fixe et
  scrollable (identité restaurant, ETA, timeline, livreur, récap, actions),
  carte interactive sur tout le reste de l'écran. L'ancien layout était une
  colonne de 480px centrée dans le vide — l'espace horizontal est désormais
  l'espace d'information géographique.
- **Mobile et tablette (< 1024px)** : carte plein écran en fond, bottom sheet
  `vaul` par-dessus, **3 snap points** — *peek* (~224px + safe-area : ETA +
  barre de progression compacte, lisible d'un coup d'œil), *mi-hauteur*
  (timeline détaillée + actions), *plein* (récap complet). Non-dismissible,
  non-modal : la carte reste manipulable derrière. Le peek intègre
  `env(safe-area-inset-bottom)` (barre de gestes iOS), `viewport-fit=cover`
  ajouté au `index.html`.
- **Tablette = layout mobile** : sous 1024px, un side sheet de 400px ne
  laisserait qu'une carte étroite en portrait ; le layout tactile carte +
  bottom sheet reste supérieur jusqu'à `lg`.
- **Mode IN : pas de carte.** Le client est assis dans le restaurant — une
  carte du restaurant serait absurde. Panneau seul centré, avec le
  **numéro de bipeur en bloc dominant** (fond couleur de marque, chiffre en
  5xl) : c'est l'information critique d'un client sur place. Les états
  terminaux (DONE/CLOSED) y affichent "Commande terminée" proprement, sans
  redirection automatique.
- **États d'échec (DENIED/CANCELED/DELETED/DELIVERY_FAILED)** : pas de carte
  non plus — un bandeau d'erreur sobre remplace ETA + timeline. Nouveauté :
  `DELIVERY_FAILED`/`DELIVERY_CANCELED` sont désormais couverts (ils
  retombaient silencieusement sur "En préparation" avant).

## 2. Inspirations retenues (et leurs sources)

- **Deliveroo** — *ETA en fourchette horaire* ("18:00 – 18:05", buffer +5 min)
  plutôt qu'un compte à rebours sec : une plage absorbe les aléas humains sans
  alarmer à chaque minute de dérive. *Livreur nommé* (le prénom, seule donnée
  exposée par le contrat RGPD, humanise la remise). *Progression qui se
  remplit continûment* — le client qui revient sur la page voit que ça avance.
- **Just Eat Takeaway (refonte globale du tracking)** — hiérarchie en trois
  niveaux : le **"quand" d'abord** (EtaBanner, plus gros élément), la
  progression ensuite, les détails et actions "sous le pouce" en bas.
  Consolidation des statuts en étapes-cœur compréhensibles plutôt qu'un
  vocabulaire logistique.
- **Uber Eats** — la barre de tracking segmentée (reprise dans le peek mobile)
  et le principe side sheet + carte sur desktop. Rien d'autre.

## 3. Ce qui a été volontairement écarté d'Uber Eats

- **Sa charte** : ni le noir/vert Uber, ni son wording, ni ses illustrations.
  La page reste sur les tokens SNO existants (Inter, `--radius` 1rem,
  `rounded-card`/`rounded-pill`, `shadow-card`/`shadow-elevated`) et surtout
  sur **l'accent dynamique du restaurant** (`merchant.design.primary_color`
  via `hexToHSL`, mécanisme conservé) : la carte, la timeline, le marqueur
  livreur et le bloc bipeur prennent la couleur de la marque du restaurant,
  pas celle d'une plateforme.
- **Les animations illustratives de statut** (personnages, scooters animés) :
  les 6 animations décoratives de l'ancienne page (dont le scooter 🛵 qui
  traversait l'écran) sont supprimées, pas reproduites. Toute animation
  restante porte une information (voir §4).
- **L'ETA à la minute près en tournée** : quand le livreur a d'autres arrêts
  avant le client (`stops_before_you > 0`), une ETA précise mentirait. On
  affiche le rang, honnête : "2 arrêts avant vous".
- **La caméra de conduite** (follow-cam inclinée du POS driver) : pertinente
  pour celui qui conduit, pas pour celui qui attend. Côté client : cadrage
  automatique par phase + bouton de recentrage, c'est tout.

## 4. Micro-interactions et le sens qu'elles portent

Chaque animation répond à "qu'est-ce que ça dit au client ?" :

| Interaction | Information portée |
|---|---|
| Glissement du marqueur livreur le long de la route (30s, distance d'arc) | La position est réelle et continue — pas de téléportation toutes les 30s. L'animation *est* la donnée. |
| Halo pulsant autour du marqueur | La position est vivante, mise à jour régulièrement. |
| Flèche orientée par le cap **seulement après un premier mouvement observé** | La direction est une mesure, pas un défaut d'affichage (corrige la flèche "plein nord" du POS). Icône point neutre avant. |
| Grisage progressif + mention "position inchangée depuis Xs" | La donnée perd en fraîcheur (formulation neutre : livreur à l'arrêt *ou* remontée en panne — on ne prétend pas savoir). |
| Point de l'étape courante qui "respire" (ping discret) | Le système est actif, la commande avance — remplace l'ancien spinner plein écran. |
| Remplissage animé des segments/connecteurs de timeline | Une étape vient d'être franchie. |
| Transition verticale de la valeur d'ETA quand elle change | La donnée vient d'être recalculée (l'ETA se rafraîchit seule, pas au tap — amélioration vs POS). |
| Check final dessiné une seule fois (spring) à la livraison | Clôture émotionnelle du parcours (l'esprit de l'ancien AnimDone, intégré à la timeline au lieu d'un écran dédié). |
| Polyline pointillée vs trait plein | Pointillés = trajet *prévu* (restaurant→client, avant prise en charge) ; plein = trajet *en cours* (livreur→client). |
| Compte à rebours visible sur "Annuler" (60s) | La fenêtre d'annulation est courte : le chiffre qui décroît rend l'urgence explicite au lieu de faire disparaître le bouton sans explication. |

`prefers-reduced-motion` est respecté partout : glissement remplacé par un
saut (le cap reste informatif), transitions réduites à des fondus, pings
désactivés (`motion-safe:`).

## 5. Décisions de véracité des données

- **QUEUED (stops_before_you > 0)** : ni ETA minute, ni polyline livreur→client
  (son parcours réel passe ailleurs d'abord). Marqueur visible, rang affiché.
- **Staleness approximée côté client** : le contrat n'expose pas
  `last_position_at` — on horodate localement chaque *changement* de position
  et on grise au-delà de 60s (2× la cadence d'écriture DB de 30s). Faux
  positif assumé pour un livreur immobile, d'où la formulation neutre.
- **ETA PREPARING** = `estimated_ready` (prépa cuisine, borné à maintenant) +
  durée OSRM restaurant→client. L'ancienne page réutilisait `estimated_ready`
  seul comme "temps restant" même en livraison — trompeur, corrigé.
- **ETA TO_YOU** = somme des segments non parcourus de la polyline depuis la
  position interpolée projetée, à vitesse moyenne OSRM constante — zéro appel
  OSRM par tick, conforme au contrat.

## 6. Portage POS (pas de réinvention)

`src/lib/geo.ts` est un port fidèle de `driver_marker_animation.dart`
(constantes aux noms d'origine : `kDriverAnimationDuration`,
`kOnRouteThresholdMeters` 45m, `kRerouteThresholdMeters` 75m,
`kRerouteConsecutivePoints` 2, `kMinRerouteInterval` 90s — si le POS change
une valeur, l'alignement SNO est immédiat), testé unitairement (30 tests).
`useOsrmRoute` reproduit le pattern signature-de-stops + reroute sur
déviation de `delivery_google_map.dart` ; `useDriverInterpolation` reproduit
`DriverMarkerAnimation` (retarget depuis la position interpolée courante,
no-op sub-métrique, bearing par frame) en `requestAnimationFrame`.

## 7. Volontairement non fait

- Aucune lib d'animation ajoutée (framer-motion déjà présent suffit) ; aucune
  state machine réinventée (le mapping `brand_status → étapes` est un module
  pur testé, `orderStatusSteps.ts`).
- Polling `useOrder` 10s intouché ; pas de WebSocket.
- OSRM reste l'instance publique `router.project-osrm.org` (auto-hébergement =
  chantier backend séparé, cf. contrat).
- `VITE_GOOGLE_MAPS_MAP_ID` : repli sur `DEMO_MAP_ID` (fonctionnel) — créer un
  vrai Map ID en console Google pour la prod, et activer **Maps JavaScript
  API** + restreindre la clé par referrer.

## 8. Ajouts post-refonte (2026-07-07) : images produits + bloc paiements

Ajout dans `OrderInfoPanel` (récap commande, partagé desktop/mobile) : vignette
produit et bloc "Paiements", sans changement backend — les deux champs
(`OrderProduct.image_url`, `Order.payments[]`) étaient déjà retournés par le
GET order.

- **Vignette produit** : cercle 48px (`ProductThumbnail`), cohérent avec les
  ronds déjà utilisés ailleurs dans le panneau (logo restaurant, icône
  livreur) plutôt qu'un carré/`rounded-card` — évite un troisième radius dans
  une même liste compacte. Repli sur l'initiale du nom (pas une icône
  générique) : plus rapide à scanner visuellement dans une liste de plusieurs
  articles, et fonctionne aussi bien pour `image_url` absent que pour une URL
  cassée (`onError`, état local par ligne — pas de retry).
- **Bloc paiements** : n'existe que si au moins un paiement `enabled` est
  présent (aucune section vide). Placé entre la liste d'articles et le total,
  toujours dans le récap repliable existant — pas un nouveau bloc top-level —
  pour rester "sous le pli" du peek mobile comme le reste du récap.
- **Mapping `mop` → libellé FR** : `STRIPE`/`CB` → "Carte bancaire",
  `CASH` → "Espèces", `TR`/`TICKET_RESTAURANT` → "Titre-restaurant",
  `CHECK`/`CHEQUE` → "Chèque", `REWARD` → "Récompense fidélité", fallback sur
  le code brut sinon (mop futurs sans casser l'affichage).
- **`REFUND`** : icône dédiée (`Undo2`) + libellé "· Remboursement" + montant
  en rouge (`text-destructive`) précédé d'un signe moins — jamais fondu dans
  le total des paiements, pour ne pas laisser croire qu'un remboursement est
  un encaissement.
- **Paiements `enabled: false`** filtrés côté affichage : ce sont des lignes
  annulées/void côté encaissement, pas des paiements réels du client.

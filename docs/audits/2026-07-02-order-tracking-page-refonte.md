# Audit — Refonte page de suivi de commande (ScanNOrder) — 2026-07-02

> Fichier créé en amont de la refonte de la page de suivi SNO. Les sections 1–9
> seront complétées lors du cadrage de la refonte. La section 10 documente un
> correctif de sécurité appliqué immédiatement (hotfix RGPD) afin que la refonte
> démarre sur une base saine.

## 10. Correctif de sécurité RGPD appliqué (2026-07-02)

**Statut : APPLIQUÉ le 2026-07-02.**

L'endpoint public `GET /scannorder/{slug}/orders/{order_id}` exposait, via le
champ `delivery_session` inline, la `delivery_session` interne complète — dont
`orders[]` listant **toutes les commandes de la tournée du livreur** avec les
données personnelles des autres clients (noms, adresses, téléphones, emails,
GPS, `customer_delivery_notes`).

Correctif :

- Nouveau DTO `scannorder.PublicDeliverySession` (+ `PublicDeliveryMan`),
  distinct du modèle interne `models.DeliverySession`.
- Il n'expose que : prénom du livreur, sa position live (`lat`/`lng`/`status`),
  le statut de la session, et un rang non-identifiant (`stops_before_you` /
  `total_stops`, des compteurs uniquement).
- Réponse encapsulée dans `PublicSNOOrder` ; le mapping vit dans
  `internal/modules/scannorder/service.go` (`toPublicDeliverySession`).
- Les endpoints merchant authentifiés (`internal/modules/delivery_sessions`)
  restent inchangés et voient toujours la tournée complète.

Détail dans `docs/decisions.md` (entrée « HOTFIX RGPD » du 2026-07-02).

**Conséquence pour la refonte** : la page de suivi ne dispose plus (et ne doit
plus dépendre) des données des autres clients de la tournée. Toute
fonctionnalité de suivi doit se construire sur `PublicDeliverySession`.

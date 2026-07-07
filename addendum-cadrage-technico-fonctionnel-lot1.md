# Addendum au cadrage technico-fonctionnel Lot 1 — Décisions arbitrées

Ce document consolide les décisions prises sur les points ouverts (§7) et bloquants (§10) du [cadrage-technico-fonctionnel-lot1.md](cadrage-technico-fonctionnel-lot1.md). En cas de contradiction avec le document initial, cet addendum fait autorité.

## Décisions §7

### 7.1 Format `booking_number`

**Option A retenue** : 6 caractères alphanumériques crypto-aléatoires, contrainte UNIQUE (`merchant_id`, `booking_number`), retry sur duplicate key. Statu quo sécurisé, pas de rupture avec les liens d'accès existants.

### 7.2 Fenêtre de reconfirmation

**Hors périmètre Lot 1.** Au Lot 2, implémenter option B (rappel J-1 + relance H-4) avec un toggle BO permettant de désactiver l'H-4 (paramètre `reminder_h4_enabled`, retour à l'option A).

### 7.3 Retries Stripe

**Hors périmètre Lot 1.** Prépa Lot 1 : création de résa et prise d'empreinte en deux étapes distinctes, jamais un INSERT conditionné à Stripe. Décision détaillée au Lot 2.

### 7.4 Idempotence sur `POST /rsv/…/booking/create`

**Faire A + B :**

- **A (backend)** : header `Idempotency-Key` + Redis SETNX TTL 15 min.
- **B (backend)** : garde-fou métier en warning non bloquant — si une résa `pending|confirmed` existe déjà pour même téléphone + même créneau, retourner un avertissement au client web sans bloquer la création. Coût : une requête SQL par création.
- **Front** : protection double-clic côté app web publique (bouton disabled pendant submit).

### 7.5 Stratégie de verrou

**Option A retenue** : SQL seul (`SELECT … FOR UPDATE` dans la transaction d'écriture). Cohérent avec la contrainte 1 connexion MySQL. Redis en verrou distribué à documenter comme évolution future si multi-instance.

### 7.6 Rétention RGPD

**Aucune purge ni anonymisation implémentée au Lot 1.** Décision reportée à la validation du CRM (Lot 4). Contrainte à respecter dès l'app web publique : mention CNIL sur le champ `comment` de la réservation (« Ne pas renseigner de données sensibles »). À intégrer dans les critères d'acceptation du composant de résa côté front.

### 7.7 Bascule UTC

**Option A retenue** : basculer les données existantes en UTC au Lot 1 via `CONVERT_TZ` par marchand (migration 056). Sécurisée par la mort du PHP (cf. 10.1).

### 7.8 `pending` consomme la capacité

**Oui.** Règle simple documentée : `pending` dont le créneau est passé = ignoré par le calcul d'occupation. Ajout d'un mécanisme d'expiration automatique :

- Cron dans `TasksManager` (réactivé en T-25), qui scanne les `pending` dont le créneau est dépassé + un délai de grâce, et les passe en `cancelled` avec `cancelled_by = SYSTEM` et une `deletion_reason` dédiée (« Expiration automatique »).
- Événement correspondant logué dans `booking_events`.
- Paramètre `pending_expiration_hours` dans `bookings_settings` (défaut 24h), champ créé au Lot 1 sans UI de paramétrage BO. UI reportée au Lot 2 avec le paramétrage anti no-show.

### 7.9 `denied` vs `cancelled`

**Conserver les deux statuts.** Enrichissement du modèle :

- Ajouter à `bookings` : `cancelled_by` VARCHAR nullable (valeurs `SYSTEM`, `CUSTOMER`, ou `user_id` staff), et `deletion_reason_id` FK nullable vers la table existante des raisons de suppression (nom exact à confirmer côté code — reprendre le pattern déjà utilisé pour les commandes).
- Ces champs sont remplis uniquement lors du passage à `denied` ou `cancelled`.
- Côté API POS : reprendre le pattern existant des commandes pour la sélection de la raison lors d'un refus (`denied`) ou d'une annulation (`cancelled`).
- L'événement correspondant est logué dans `booking_events` (source, raison, auteur).
- Migration à créer (numéro à ajuster selon l'ordre final, à placer avec ou après la 053 de mapping legacy).

## Décisions §10

### 10.1 PHP historique

Le PHP n'écrit plus dans `bookings`. Liberté totale sur les migrations 050+ (schéma, statuts, UTC).

### 10.4 Contrat POS

**Non gelé.** Le POS ne consomme pas encore `/bookings` en production, refonte coordonnée backend + client Flutter autorisée.

## Impact sur le découpage tickets

- **T-XX (à créer) — Champs d'annulation enrichis** : migration + code applicatif pour `cancelled_by`, `deletion_reason_id`. À placer en Phase 1 (fusion modules).
- **T-25 (à étendre) — Cron TasksManager** : inclure la tâche d'expiration des `pending` en plus des rappels.
- **T-17 (settings) — Champ `pending_expiration_hours`** : ajouter au schéma des settings avec valeur par défaut.
- **T-11 (flux public) — Idempotence + garde-fou B** : ajouter le check warning non bloquant en plus de l'idempotency key.
- **T-11 (flux public) — Mention CNIL** : critère d'acceptation sur le composant front public.

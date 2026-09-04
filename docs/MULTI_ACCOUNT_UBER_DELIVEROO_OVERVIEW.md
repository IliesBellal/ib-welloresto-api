# Multi-Account Uber Eats & Deliveroo - Vue d'Ensemble

**Date:** 2026-09-02  
**Statut:** Planification Phase 1  
**Durée estimée:** 2-3 semaines  
**Risque:** MINIMAL

---

## 🎯 Objectif

Permettre à chaque restaurant de gérer **plusieurs comptes Uber Eats et/ou Deliveroo** indépendamment, sans casser le fonctionnement actuel pour les restaurants mono-compte.

**Migration transparente:** Les restaurants avec 1 seul compte ne voient aucun changement.

---

## 📋 Phases

### Phase 1: Base de Données (1-2 jours)
- Modifier clés primaires: `merchant_id` → `(merchant_id, store_id/location_id)`
- Ajouter `store_id`/`location_id` aux tables de mapping
- Ajouter `store_id`/`location_id` à la table `orders` pour traçabilité
- Scripts de migration pour les données existantes

**Détails:** `MULTI_ACCOUNT_PHASE1_DATABASE.md`

### Phase 2: Backend (5-7 jours)
- Adapter repositories (nouvelle méthode `GetAccountsByMerchant`)
- Adapter models
- Adapter services (paramètres optionnels store_id/location_id)
- Adapter handlers
- ⚠️ Webhooks: AUCUN changement requis

**Détails:** `MULTI_ACCOUNT_PHASE2_BACKEND.md`

### Phase 3: Frontend (5 jours)
- Adapter pages existantes (ajouter select déroulant)
- Adapter services API
- Adapter types TypeScript

**Détails:** `MULTI_ACCOUNT_PHASE3_FRONTEND.md`

### Phase 4: Flutter (3-4 jours)
- Adapter models
- Adapter services

**Détails:** `MULTI_ACCOUNT_PHASE4_FLUTTER.md`

### Phase 5: Tests & Déploiement (3-4 jours)
- Tests locaux, staging, production
- Monitoring 24h

**Détails:** `MULTI_ACCOUNT_DEPLOYMENT_CHECKLIST.md`

---

## 🔑 Décisions Clés

### Authentification API
- **Le token contient déjà le `merchant_id`**
- `store_id` / `location_id` peuvent être optionnels en URL
- Si non fourni, utiliser le compte "primaire" (premier inséré)

### Webhooks
- **AUCUN changement** dans la logique webhook
- Ils continuent à retrouver le `merchant_id` via `store_id` / `location_id`
- La commande créée stockera le `store_id` pour traçabilité

### Tables de Mapping
- Chaque compte (store_id) a ses **propres mappings produits**
- Deux accounts du même restaurant ne partagent PAS les mêmes mappings
- Clé composite: `(merchant_id, store_id, local_product_id)`

### Table Orders
- Ajouter champ `store_id` (Uber) et `location_id` (Deliveroo)
- Permet de tracer l'origine exacte de chaque commande
- Important pour les rapports par account

---

## 📊 Impact par Composant

| Composant | Changes | Risk | Effort |
|-----------|---------|------|--------|
| **BD** | Clés primaires + mappings + orders | Minimal | 2 jours |
| **Webhooks** | Aucun | None | 0 |
| **Backend Services** | Paramètres optionnels | Low | 5-7 jours |
| **Frontend** | Select déroulant | Low | 5 jours |
| **Flutter** | Models adapts | Low | 3-4 jours |
| **Kiosk** | Aucun | None | 0 |

---

## ✅ Migration Transparente

### Restaurants avec 1 seul compte
- Le select déroulant ne s'affiche pas
- Les endpoints fonctionnent exactement comme avant
- Le compte primaire est utilisé par défaut
- **Zéro impact utilisateur**

### Restaurants avec 2+ comptes
- Select déroulant pour choisir le compte
- Chaque compte a sa configuration indépendante
- Les mappings produits sont séparés
- Les commandes tracent leur origine

---

## 🚀 Prochaines Étapes

1. ✅ Valider cette vue d'ensemble
2. → Commencer Phase 1: Modifications BD
3. → Tester sur local
4. → Dry-run sur staging
5. → Déployer en production

---

## 📚 Documents Associés

- `MULTI_ACCOUNT_PHASE1_DATABASE.md` - Migrations SQL détaillées
- `MULTI_ACCOUNT_PHASE2_BACKEND.md` - Code backend exact
- `MULTI_ACCOUNT_PHASE3_FRONTEND.md` - Code frontend exact
- `MULTI_ACCOUNT_PHASE4_FLUTTER.md` - Code Flutter exact
- `MULTI_ACCOUNT_DEPLOYMENT_CHECKLIST.md` - Checklist déploiement

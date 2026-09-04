# Checklist de Déploiement Multi-Account

**Durée:** 3-4 jours (Tests + Déploiement + Monitoring)

---

## ⚠️ Principes

1. **Migration transparente:** Les restaurants avec 1 compte ne voient AUCUN changement
2. **Zéro downtime:** Les webhooks continuent de fonctionner normalement
3. **Rollback facile:** Un script de rollback est prêt en cas de problème

---

## 🧪 Phase 1: Tests Locaux (1-2 jours)

### Avant de toucher à la BD

- [ ] Migration SQL appliquée sur local
- [ ] Tous les scripts de migration des données exécutés
- [ ] Vérification: 0 NULL dans les mappings/orders
- [ ] Tests unitaires backend: TOUS passent
- [ ] Tests unitaires frontend: TOUS passent
- [ ] Tests unitaires Flutter: TOUS passent

### Tests Manuels

#### BD
- [ ] `SELECT * FROM integration_uber_eats WHERE merchant_id='test'` retourne les bons comptes
- [ ] `SELECT * FROM integration_uber_eats WHERE store_id='store-123'` retrouve le bon merchant
- [ ] Les indexes existent: `idx_uber_eats_store_id`, `idx_deliveroo_location_id`
- [ ] Les clés primaires sont composites: `(merchant_id, store_id)`

#### Backend API
- [ ] `GET /integrations/uber-eats?merchant_id=XXX` retourne une liste
- [ ] `GET /integrations/uber-eats?merchant_id=XXX&store_id=YYY` retourne UN compte
- [ ] `PATCH /menu/uber-eats/sync?merchant_id=XXX` (syncer primaire)
- [ ] `PATCH /menu/uber-eats/sync?merchant_id=XXX&store_id=YYY` (syncer spécifique)
- [ ] Webhooks `POST /webhook/uber-eats` fonctionnent toujours

#### Frontend
- [ ] Page UberEats: 1 seul compte → select caché
- [ ] Page UberEats: 2+ comptes → select visible
- [ ] Change de select → page se recharge avec les bons détails
- [ ] Page Deliveroo: même comportement

#### Flutter
- [ ] `UberEatsIntegration.fromJson()` parse correctement
- [ ] Provider `uberEatsIntegrationProvider` retourne la liste
- [ ] Provider `selectedUberEatsStoreIdProvider` fonctionne
- [ ] Screen affiche le select avec 2+ comptes
- [ ] Screen cache le select avec 1 compte

---

## 🚀 Phase 2: Staging (1 jour)

### 1. Backup Complet (CRITIQUE)

```bash
# Faire un backup avant tout déploiement
pg_dump -U postgres wello_resto > backup_staging_$(date +%Y%m%d_%H%M%S).sql
gzip backup_staging_*.sql

# Garder ce backup pendant 7 jours
```

### 2. Déployer la Migration BD

- [ ] Migration appliquée sur staging
- [ ] Tous les scripts de migration des données exécutés
- [ ] Vérification de complétude (voir Phase 1 tests)
- [ ] Performance: les index fonctionnent bien (pas de table scan)

### 3. Déployer le Backend

```bash
cd ib-welloresto-api
git checkout staging
git pull
docker build -t wello-api:multi-account-staging .
docker push wello-api:multi-account-staging
kubectl set image deployment/wello-api wello-api=wello-api:multi-account-staging -n staging
kubectl rollout status deployment/wello-api -n staging
```

- [ ] Backend déployé et sain (health check: 200 OK)
- [ ] Logs: pas d'erreurs
- [ ] Webhooks: test avec données fictives Uber
- [ ] Webhooks: test avec données fictives Deliveroo

### 4. Déployer le Frontend

```bash
cd wello-back-office
git checkout staging
git pull
npm run build
npm run deploy:staging
```

- [ ] Frontend accessible
- [ ] Page UberEats charge
- [ ] Page Deliveroo charge
- [ ] Select déroulant fonctionne (si 2+ comptes en test)

### 5. Déployer Flutter

```bash
cd wello_resto_flutter
git checkout staging
git pull
flutter build apk --release
# ou déployer sur le serveur test
```

- [ ] App compile sans erreur
- [ ] Models chargent correctement
- [ ] Screen affiche les comptes

### 6. Tests End-to-End sur Staging

#### Scénario 1: Restaurant avec 1 seul compte (Backward Compat)

```
1. Récupérer un restaurant ACTUEL avec 1 compte Uber
2. Accéder à sa page UberEats en back-office
   ✓ Pas de select déroulant
   ✓ Configuration affichée normalement
3. Tenter une synchronisation menu
   ✓ Sync fonctionne
4. Envoyer un webhook de commande fictive
   ✓ Commande créée avec store_id rempli
5. Vérifier que la commande est visible en back-office
   ✓ OK
```

#### Scénario 2: Restaurant avec 2 comptes Uber (NOUVEAU)

```
1. Créer un test restaurant avec 2 comptes Uber (via script SQL)
2. Accéder à sa page UberEats en back-office
   ✓ Select déroulant visible
3. Changer de select
   ✓ Page se recharge avec les détails du compte choisi
4. Syncer le menu pour le 1er compte
   ✓ Sync OK
5. Changer de select → syncer le 2e compte
   ✓ Sync OK (mappings différents)
6. Envoyer 2 webhooks différents (1 par compte)
   ✓ Les 2 commandes sont créées
   ✓ Chacune a son store_id
```

- [ ] Scénario 1 PASSÉ
- [ ] Scénario 2 PASSÉ
- [ ] Pas de regression sur les features existantes

### 7. Tests de Performance

```sql
-- Vérifier que les queries sont rapides
EXPLAIN ANALYZE SELECT * FROM integration_uber_eats WHERE store_id = '...';
-- Temps: < 10ms

EXPLAIN ANALYZE SELECT * FROM orders WHERE merchant_id = '...' AND store_id = '...';
-- Temps: < 50ms
```

- [ ] Query par store_id: rapide (< 10ms)
- [ ] Query orders par merchant + store_id: rapide (< 50ms)
- [ ] Pas de table scan complet

### 8. Vérification des Logs

```bash
kubectl logs deployment/wello-api -n staging -f --since=1h

# Chercher les patterns:
# ✓ Pas d'erreurs "account not found"
# ✓ Pas d'erreurs "store_id mismatch"
# ✓ Webhooks traités normalement
```

- [ ] Pas d'erreurs critiques en logs
- [ ] Webhooks traités sans problème
- [ ] Performance: pas de requêtes lentes

---

## 🌍 Phase 3: Production (1 jour)

### ⚠️ CRITÈRES AVANT DÉPLOYER EN PRODUCTION

- [ ] ALL tests locaux PASSENT
- [ ] ALL tests staging PASSENT
- [ ] Backup de production prêt
- [ ] Plan de rollback validé
- [ ] Équipe support alertée
- [ ] Heures creuses sélectionnées (dimanche 02h-04h UTC par ex.)

### 1. Maintenance Window

```
Envoyer une notification utilisateurs:
"Maintenance système dimanche 02h-04h UTC
Les intégrations Uber Eats et Deliveroo peuvent être indisponibles."
```

- [ ] Notification envoyée
- [ ] Heures creuses confirmées (vérifier pas de peak)

### 2. Backup Production (CRITIQUE)

```bash
# Sur le serveur de production
pg_dump -U postgres -h prod-db wello_resto | gzip > backup_prod_$(date +%Y%m%d_%H%M%S).sql

# Copier le backup ailleurs pour sécurité
scp backup_prod_*.sql secure-storage:/backups/
```

- [ ] Backup complet créé
- [ ] Backup vérifié (peut être restauré)
- [ ] 3 copies du backup (local + 2x offsite)

### 3. Déployer en Production

#### 3a. Migration BD

```bash
# Sur prod-db
psql -U postgres wello_resto -f migrations/done/XXX_multi_account_integration.sql

# Vérifier que c'est appliqué
\d integration_uber_eats  # Vérifier clé composite
SELECT COUNT(*) FROM integration_uber_eats_products_mapping WHERE store_id IS NOT NULL;
# Doit être 100% (tous les mappings ont store_id)
```

- [ ] Migration appliquée
- [ ] Vérification: 100% des mappings ont store_id
- [ ] Vérification: 100% des orders UE/DR ont store_id/location_id

#### 3b. Backend

```bash
# Rolling deployment (0 downtime)
kubectl set image deployment/wello-api-prod wello-api=wello-api:multi-account-prod --record -n production
kubectl rollout status deployment/wello-api-prod -n production
```

- [ ] Backend déployé (rolling update)
- [ ] Health check: 200 OK pour tous les pods
- [ ] Logs: pas d'erreurs critiques

#### 3c. Frontend

```bash
# Généralement sans downtime
npm run deploy:production
```

- [ ] Frontend accessible
- [ ] Cache invalidé
- [ ] Pas de 404s sur les ressources

#### 3d. Flutter (Mobile)

```
# Déployer la nouvelle version sur les stores
# Ou créer une build APK testable en interne
```

- [ ] Build publiée ou testée

### 4. Smoke Tests Production (Pendant la maintenance)

```bash
# Tester les endpoints critiques

# Test 1: Un restaurant existant (1 compte)
curl -H "Authorization: Bearer $TOKEN" \
  "https://api.prod.example.com/integrations/uber-eats?merchant_id=restaurant-123"
# Doit retourner: { accounts: [...], primary_store_id: "..." }

# Test 2: Webhook test
curl -X POST \
  -H "Content-Type: application/json" \
  "https://api.prod.example.com/webhook/uber-eats" \
  -d '{"meta": {"user_id": "store-abc"}, ...}'
# Doit retourner: 200 OK

# Test 3: Sync menu
curl -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  "https://api.prod.example.com/menu/uber-eats/sync?merchant_id=restaurant-123"
# Doit retourner: 200 OK
```

- [ ] Endpoint liste comptes: OK
- [ ] Endpoint compte spécifique: OK
- [ ] Webhook: OK
- [ ] Sync menu: OK

### 5. Notification Fin de Maintenance

```
Envoyer notification:
"Maintenance terminée. Tous les systèmes sont opérationnels.
Les intégrations Uber Eats et Deliveroo sont disponibles."
```

- [ ] Notification envoyée

---

## 📊 Phase 4: Monitoring 24h (1 jour complet)

### Pendant 24h après déploiement

#### Métriques à Surveiller

```
1. Taux d'erreur API
   ✓ Doit être < 0.1%
   ✗ Si > 1%: ALERT

2. Latence des endpoints
   ✓ GET /integrations/uber-eats: < 200ms (P95)
   ✗ Si > 500ms: ALERT

3. Webhooks
   ✓ Taux de succès: > 99%
   ✗ Si < 95%: ALERT

4. DB Queries
   ✓ Temps moyen: < 50ms
   ✗ Si > 200ms: ALERT

5. Logs d'erreur
   ✓ 0 erreurs "account not found"
   ✓ 0 erreurs "store_id mismatch"
   ✗ Si des erreurs: ALERT + Investigation
```

#### Points de Vérification

- [ ] Dashboard Grafana: pas d'anomalies
- [ ] Logs centralisés: pas d'erreurs critiques
- [ ] Slack alerts: aucune alerte non résolue
- [ ] Business metrics: nombre de commandes normal

#### Tests Continus

```bash
# Toutes les 2 heures pendant 24h
./scripts/smoke-test-production.sh

# Tester:
# 1. Un restaurant avec 1 compte
# 2. Un restaurant avec 2+ comptes (créé en staging)
# 3. Webhooks Uber et Deliveroo
# 4. Sync menu par account
```

- [ ] Smoke tests: tous les 2h pendant 24h, tous PASSENT

### Si Problème Détecté

**ROLLBACK IMMÉDIAT:**

```bash
# Restaurer depuis le backup
pg_restore -d wello_resto backup_prod_YYYYMMDD_HHMMSS.sql

# Redéployer l'ancienne version du backend
kubectl rollout undo deployment/wello-api-prod -n production
kubectl rollout status deployment/wello-api-prod -n production

# Notifier l'équipe
# Analyser le problème
# Corriger et redéployer
```

- [ ] Procedure de rollback testée avant déploiement

---

## 🎉 Phase 5: Post-Déploiement (1 jour)

### Documentation

- [ ] Documentation API mise à jour
- [ ] Guide utilisateur pour sélectionner les comptes
- [ ] Guide support pour déboguer les problèmes multi-accounts
- [ ] Release notes rédigées

### Formation Équipe

- [ ] Support formé (new feature multi-account)
- [ ] Team Ops formée (monitoring/rollback)
- [ ] Equipe Backend/Frontend au courant

### Suivi

- [ ] Surveiller pendant 1 semaine (métriques)
- [ ] Recueillir le feedback utilisateurs
- [ ] Corriger les bugs mineurs

---

## ✅ Checklist Finale

- [ ] Phase 1 (Tests Locaux): PASSÉE
- [ ] Phase 2 (Tests Staging): PASSÉE
- [ ] Phase 3a (BD Production): APPLIQUÉE
- [ ] Phase 3b (Backend Production): DÉPLOYÉ
- [ ] Phase 3c (Frontend Production): DÉPLOYÉ
- [ ] Phase 3d (Flutter): TESTÉ
- [ ] Phase 4 (Monitoring 24h): COMPLÉTÉE
- [ ] Phase 5 (Post-Déploiement): COMPLÉTÉE
- [ ] **PRODUCTION READY** ✓

---

## 🆘 Contacts d'Urgence

- **DB Team:** [contact]
- **Backend Team:** [contact]
- **Frontend Team:** [contact]
- **Ops Team:** [contact]

Numéro d'escalade: [numéro]

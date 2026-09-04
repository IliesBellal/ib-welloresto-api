# Phase 3: Frontend Back-Office - Modifications Détaillées

**Durée:** 5 jours  
**Impact:** Pages existantes + Services API + Types

---

## 📋 Approche

**Ne PAS créer de nouvelles pages.** Adapter les pages existantes avec un select déroulant pour choisir le compte.

---

## 🎨 UI Pattern: Select Déroulant Conditionnel

```
┌─────────────────────────────────────────┐
│ Uber Eats Configuration                 │
├─────────────────────────────────────────┤
│                                         │
│ Compte: [Store-123 ▼]  [+ Ajouter]    │  ← S'affiche seulement si 2+ comptes
│                                         │
│ ┌─────────────────────────────────────┐ │
│ │ Statut: ✓ Actif                    │ │
│ │ Commission: 30%                     │ │
│ │ Temps préparation: 15 min           │ │
│ │ Synchronisation: Mercredi 14h30     │ │
│ │                                     │ │
│ │ [Synchroniser maintenant]           │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

**Règle:** Le select déroulant ne s'affiche que si `accounts.length >= 2`

---

## 🔄 Flux de Données

### Avant (Mono-Compte)
```
1. Page charge
2. GET /integrations/uber-eats?merchant_id=XXX
3. Affiche le compte (1 seul)
```

### Après (Multi-Account)
```
1. Page charge
2. GET /integrations/uber-eats?merchant_id=XXX (pas de store_id)
3. Récupère liste: { accounts: [], primaryStoreId: "..." }
4. Sélectionne le compte primaire par défaut
5. GET /integrations/uber-eats?merchant_id=XXX&store_id=YYY
6. Affiche les détails du compte sélectionné
7. Si utilisateur change de select:
   → Répéter étape 5-6 avec le nouveau store_id
```

---

## 💻 Code Frontend

### Types TypeScript

**Fichier:** `src/types/integrations.ts`

```typescript
// ==========================================
// UBER EATS
// ==========================================

export interface UberEatsAccount {
    merchant_id: string;
    store_id: string;
    enabled: boolean;
    commission_rate?: number;
    auto_accept_orders?: boolean;
    estimated_preparation_time?: string;
    bearer_token_expiration_date?: string;
    last_sync?: string;
    synced_items?: number;
    created_at: string;
    updated_at: string;
}

export interface UberEatsIntegration {
    merchant_id: string;
    accounts: UberEatsAccount[];
    primary_store_id: string;
}

// ==========================================
// DELIVEROO
// ==========================================

export interface DeliverooAccount {
    merchant_id: string;
    location_id: string;
    brand_id: string;
    enabled: boolean;
    commission_rate?: number;
    preparation_time_minutes?: number;
    auto_accept_orders?: boolean;
    last_sync?: string;
    synced_items?: number;
    created_at: string;
    updated_at: string;
}

export interface DeliverooIntegration {
    merchant_id: string;
    accounts: DeliverooAccount[];
    primary_location_id: string;
}
```

### Service API

**Fichier:** `src/services/integrationsService.ts`

```typescript
import { api } from '@/lib/axios';
import { 
    UberEatsIntegration, 
    UberEatsAccount,
    DeliverooIntegration,
    DeliverooAccount 
} from '@/types/integrations';

export const integrationsService = {
    // ==========================================
    // UBER EATS
    // ==========================================

    /**
     * Récupère tous les comptes Uber Eats du merchant
     * Si store_id est fourni, retourne le compte spécifique
     */
    getUberEats: async (
        merchantId: string,
        storeId?: string
    ): Promise<UberEatsIntegration | UberEatsAccount> => {
        let url = `/integrations/uber-eats?merchant_id=${merchantId}`;
        if (storeId) {
            url += `&store_id=${storeId}`;
        }
        const response = await api.get(url);
        return response.data;
    },

    /**
     * Synce le menu Uber Eats
     * Si store_id est fourni, syncer ce compte spécifique
     */
    syncUberEatsMenu: async (
        merchantId: string,
        storeId?: string
    ): Promise<{ status: string; message: string }> => {
        let url = `/menu/uber-eats/sync?merchant_id=${merchantId}`;
        if (storeId) {
            url += `&store_id=${storeId}`;
        }
        const response = await api.patch(url);
        return response.data;
    },

    /**
     * Met à jour la configuration d'un compte Uber Eats
     */
    updateUberEats: async (
        merchantId: string,
        storeId: string,
        data: Partial<UberEatsAccount>
    ): Promise<UberEatsAccount> => {
        const response = await api.patch(
            `/integrations/uber-eats?merchant_id=${merchantId}&store_id=${storeId}`,
            data
        );
        return response.data;
    },

    /**
     * Déconnecte un compte Uber Eats
     */
    disconnectUberEats: async (
        merchantId: string,
        storeId: string
    ): Promise<{ status: string }> => {
        const response = await api.delete(
            `/integrations/uber-eats?merchant_id=${merchantId}&store_id=${storeId}`
        );
        return response.data;
    },

    // ==========================================
    // DELIVEROO (Idem que Uber Eats)
    // ==========================================

    getDeliveroo: async (
        merchantId: string,
        locationId?: string
    ): Promise<DeliverooIntegration | DeliverooAccount> => {
        let url = `/integrations/deliveroo?merchant_id=${merchantId}`;
        if (locationId) {
            url += `&location_id=${locationId}`;
        }
        const response = await api.get(url);
        return response.data;
    },

    syncDeliverooMenu: async (
        merchantId: string,
        locationId?: string
    ): Promise<{ status: string; message: string }> => {
        let url = `/menu/deliveroo/sync?merchant_id=${merchantId}`;
        if (locationId) {
            url += `&location_id=${locationId}`;
        }
        const response = await api.patch(url);
        return response.data;
    },

    updateDeliveroo: async (
        merchantId: string,
        locationId: string,
        data: Partial<DeliverooAccount>
    ): Promise<DeliverooAccount> => {
        const response = await api.patch(
            `/integrations/deliveroo?merchant_id=${merchantId}&location_id=${locationId}`,
            data
        );
        return response.data;
    },

    disconnectDeliveroo: async (
        merchantId: string,
        locationId: string
    ): Promise<{ status: string }> => {
        const response = await api.delete(
            `/integrations/deliveroo?merchant_id=${merchantId}&location_id=${locationId}`
        );
        return response.data;
    },
};
```

### Composant Page

**Fichier:** `src/pages/UberEats.tsx`

```typescript
import { useState, useEffect } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { integrationsService } from '@/services/integrationsService';
import { UberEatsIntegration, UberEatsAccount } from '@/types/integrations';

export function UberEatsPage() {
    const { merchantId } = useAuth();
    const [integration, setIntegration] = useState<UberEatsIntegration | null>(null);
    const [selectedStoreId, setSelectedStoreId] = useState<string | null>(null);
    const [currentAccount, setCurrentAccount] = useState<UberEatsAccount | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    // Charger la liste des comptes au démarrage
    useEffect(() => {
        (async () => {
            try {
                setLoading(true);
                const data = await integrationsService.getUberEats(merchantId);
                
                // Déterminer si c'est une liste (integration) ou un compte (account)
                if ('accounts' in data) {
                    // C'est une Integration (liste)
                    setIntegration(data);
                    setSelectedStoreId(data.primary_store_id);
                } else {
                    // Backward compat: c'est un Account unique
                    setIntegration({
                        merchant_id: merchantId,
                        accounts: [data],
                        primary_store_id: data.store_id,
                    });
                    setSelectedStoreId(data.store_id);
                }
            } catch (err) {
                setError((err as Error).message);
            } finally {
                setLoading(false);
            }
        })();
    }, [merchantId]);

    // Charger les détails du compte sélectionné
    useEffect(() => {
        if (!selectedStoreId) return;

        (async () => {
            try {
                const account = await integrationsService.getUberEats(
                    merchantId,
                    selectedStoreId
                ) as UberEatsAccount;
                setCurrentAccount(account);
            } catch (err) {
                setError((err as Error).message);
            }
        })();
    }, [selectedStoreId, merchantId]);

    const handleSyncMenu = async () => {
        try {
            await integrationsService.syncUberEatsMenu(merchantId, selectedStoreId || undefined);
            // Recharger le compte pour mettre à jour last_sync
            const account = await integrationsService.getUberEats(
                merchantId,
                selectedStoreId || undefined
            );
            setCurrentAccount(account as UberEatsAccount);
        } catch (err) {
            setError((err as Error).message);
        }
    };

    if (loading) return <div>Chargement...</div>;
    if (error) return <div>Erreur: {error}</div>;
    if (!integration) return <div>Intégration non trouvée</div>;

    const hasMultipleAccounts = integration.accounts.length > 1;

    return (
        <div className="page-uber-eats">
            <h1>Uber Eats</h1>

            {/* Select déroulant: seulement si 2+ comptes */}
            {hasMultipleAccounts && (
                <div className="account-selector">
                    <label>Compte:</label>
                    <select
                        value={selectedStoreId || ''}
                        onChange={(e) => setSelectedStoreId(e.target.value)}
                    >
                        {integration.accounts.map((account) => (
                            <option key={account.store_id} value={account.store_id}>
                                {account.store_id} {account === integration.accounts[0] ? '(Primaire)' : ''}
                            </option>
                        ))}
                    </select>
                    <button className="btn-secondary">+ Ajouter compte</button>
                </div>
            )}

            {/* Configuration du compte sélectionné */}
            {currentAccount && (
                <div className="configuration-box">
                    <div className="status-row">
                        <label>Statut:</label>
                        <span className={currentAccount.enabled ? 'status-active' : 'status-inactive'}>
                            {currentAccount.enabled ? '✓ Actif' : '✗ Inactif'}
                        </span>
                    </div>

                    <div className="form-group">
                        <label>Commission:</label>
                        <input
                            type="number"
                            value={currentAccount.commission_rate ?? ''}
                            onChange={(e) => {
                                // TODO: implémenter la mise à jour
                            }}
                        />
                    </div>

                    <div className="form-group">
                        <label>Temps de préparation:</label>
                        <input
                            type="text"
                            value={currentAccount.estimated_preparation_time ?? ''}
                            onChange={(e) => {
                                // TODO: implémenter la mise à jour
                            }}
                        />
                    </div>

                    {currentAccount.last_sync && (
                        <div className="sync-info">
                            <p>Dernière synchronisation: {new Date(currentAccount.last_sync).toLocaleString()}</p>
                            <p>Articles synchronisés: {currentAccount.synced_items}</p>
                        </div>
                    )}

                    <button onClick={handleSyncMenu} className="btn-primary">
                        Synchroniser le menu
                    </button>

                    <button className="btn-danger">Déconnecter</button>
                </div>
            )}
        </div>
    );
}
```

### Composant Réutilisable (Optionnel)

**Fichier:** `src/components/integrations/AccountSelector.tsx`

```typescript
import { SelectHTMLAttributes } from 'react';
import { UberEatsAccount } from '@/types/integrations';

interface AccountSelectorProps extends SelectHTMLAttributes<HTMLSelectElement> {
    accounts: UberEatsAccount[];
    value: string;
}

export function AccountSelector({ accounts, value, ...props }: AccountSelectorProps) {
    if (accounts.length <= 1) {
        return null; // Ne pas afficher si 1 seul compte
    }

    return (
        <div className="account-selector">
            <label>Compte:</label>
            <select value={value} {...props}>
                {accounts.map((account, index) => (
                    <option key={account.store_id} value={account.store_id}>
                        {account.store_id} {index === 0 ? '(Primaire)' : ''}
                    </option>
                ))}
            </select>
        </div>
    );
}
```

**Usage dans la page:**
```typescript
<AccountSelector 
    accounts={integration.accounts}
    value={selectedStoreId}
    onChange={(e) => setSelectedStoreId(e.target.value)}
/>
```

---

## 🔄 Appliquer le Même Pattern à Deliveroo

Créer `src/pages/Deliveroo.tsx` avec le même pattern, en remplaçant:
- `store_id` → `location_id`
- `integrationsService.getUberEats()` → `integrationsService.getDeliveroo()`
- etc.

---

## 📝 Checklist Phase 3 Frontend

- [ ] Types TypeScript créés (UberEatsAccount, UberEatsIntegration, etc.)
- [ ] Services API adaptés (paramètres optionnels store_id/location_id)
- [ ] Page UberEats.tsx modifiée (select déroulant conditionnel)
- [ ] Page Deliveroo.tsx modifiée (idem)
- [ ] Composant AccountSelector créé (optionnel mais recommandé)
- [ ] Tests: 1 seul compte (select caché)
- [ ] Tests: 2+ comptes (select visible)
- [ ] Vérification: aucune regression sur les pages existantes
- [ ] Mise à jour de la documentation UI/UX

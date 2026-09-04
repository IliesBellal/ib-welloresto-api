# Phase 4: Flutter POS - Modifications Détaillées

**Durée:** 3-4 jours  
**Impact:** Models + Services

---

## 📐 Objectif

Adapter les modèles Flutter pour supporter la liste d'accounts au lieu d'un seul compte. Permettre au POS Flutter de choisir quel account utiliser.

---

## 🔄 Models Dart

### Avant (Mono-Compte)

**Fichier:** `lib/models/uber_eats_integration.dart`

```dart
class UberEatsIntegration {
  final String merchantId;
  final String storeId;
  final bool enabled;
  final int? commissionRate;
  final bool? autoAcceptOrders;
  
  UberEatsIntegration({
    required this.merchantId,
    required this.storeId,
    required this.enabled,
    this.commissionRate,
    this.autoAcceptOrders,
  });
}
```

### Après (Multi-Account)

```dart
// Représente un compte Uber Eats
class UberEatsAccount {
  final String merchantId;
  final String storeId;
  final bool enabled;
  final int? commissionRate;
  final bool? autoAcceptOrders;
  final String? estimatedPreparationTime;
  final DateTime? bearerTokenExpirationDate;
  final DateTime? lastSync;
  final int? syncedItems;

  UberEatsAccount({
    required this.merchantId,
    required this.storeId,
    required this.enabled,
    this.commissionRate,
    this.autoAcceptOrders,
    this.estimatedPreparationTime,
    this.bearerTokenExpirationDate,
    this.lastSync,
    this.syncedItems,
  });

  factory UberEatsAccount.fromJson(Map<String, dynamic> json) {
    return UberEatsAccount(
      merchantId: json['merchant_id'] as String,
      storeId: json['store_id'] as String,
      enabled: json['enabled'] as bool? ?? false,
      commissionRate: json['commission_rate'] as int?,
      autoAcceptOrders: json['auto_accept_orders'] as bool?,
      estimatedPreparationTime: json['estimated_preparation_time'] as String?,
      bearerTokenExpirationDate: json['bearer_token_expiration_date'] != null
          ? DateTime.parse(json['bearer_token_expiration_date'] as String)
          : null,
      lastSync: json['last_sync'] != null
          ? DateTime.parse(json['last_sync'] as String)
          : null,
      syncedItems: json['synced_items'] as int?,
    );
  }

  Map<String, dynamic> toJson() => {
    'merchant_id': merchantId,
    'store_id': storeId,
    'enabled': enabled,
    'commission_rate': commissionRate,
    'auto_accept_orders': autoAcceptOrders,
    'estimated_preparation_time': estimatedPreparationTime,
    'bearer_token_expiration_date': bearerTokenExpirationDate?.toIso8601String(),
    'last_sync': lastSync?.toIso8601String(),
    'synced_items': syncedItems,
  };
}

// Représente l'intégration Uber Eats (1 ou plusieurs comptes)
class UberEatsIntegration {
  final String merchantId;
  final List<UberEatsAccount> accounts;
  final String primaryStoreId;

  UberEatsIntegration({
    required this.merchantId,
    required this.accounts,
    required this.primaryStoreId,
  });

  /// Getter: Retrouver un compte par store_id
  UberEatsAccount? getAccountByStoreId(String storeId) {
    try {
      return accounts.firstWhere((a) => a.storeId == storeId);
    } catch (e) {
      return null;
    }
  }

  /// Getter: Compte primaire
  UberEatsAccount get primaryAccount {
    return getAccountByStoreId(primaryStoreId) ?? accounts.first;
  }

  /// Getter: Y a-t-il plusieurs comptes?
  bool get hasMultipleAccounts => accounts.length > 1;

  factory UberEatsIntegration.fromJson(Map<String, dynamic> json) {
    final accountsList = (json['accounts'] as List<dynamic>?)
        ?.map((a) => UberEatsAccount.fromJson(a as Map<String, dynamic>))
        .toList() ?? [];

    return UberEatsIntegration(
      merchantId: json['merchant_id'] as String,
      accounts: accountsList,
      primaryStoreId: json['primary_store_id'] as String? ?? (accountsList.isNotEmpty ? accountsList.first.storeId : ''),
    );
  }

  Map<String, dynamic> toJson() => {
    'merchant_id': merchantId,
    'accounts': accounts.map((a) => a.toJson()).toList(),
    'primary_store_id': primaryStoreId,
  };
}
```

### Idem pour Deliveroo

```dart
class DeliverooAccount {
  final String merchantId;
  final String locationId;
  final String brandId;
  final bool enabled;
  final int? commissionRate;
  final int? preparationTimeMinutes;
  final bool? autoAcceptOrders;
  final DateTime? lastSync;
  final int? syncedItems;

  DeliverooAccount({
    required this.merchantId,
    required this.locationId,
    required this.brandId,
    required this.enabled,
    this.commissionRate,
    this.preparationTimeMinutes,
    this.autoAcceptOrders,
    this.lastSync,
    this.syncedItems,
  });

  factory DeliverooAccount.fromJson(Map<String, dynamic> json) {
    return DeliverooAccount(
      merchantId: json['merchant_id'] as String,
      locationId: json['location_id'] as String,
      brandId: json['brand_id'] as String,
      enabled: json['enabled'] as bool? ?? false,
      commissionRate: json['commission_rate'] as int?,
      preparationTimeMinutes: json['preparation_time_minutes'] as int?,
      autoAcceptOrders: json['auto_accept_orders'] as bool?,
      lastSync: json['last_sync'] != null
          ? DateTime.parse(json['last_sync'] as String)
          : null,
      syncedItems: json['synced_items'] as int?,
    );
  }

  Map<String, dynamic> toJson() => {
    'merchant_id': merchantId,
    'location_id': locationId,
    'brand_id': brandId,
    'enabled': enabled,
    'commission_rate': commissionRate,
    'preparation_time_minutes': preparationTimeMinutes,
    'auto_accept_orders': autoAcceptOrders,
    'last_sync': lastSync?.toIso8601String(),
    'synced_items': syncedItems,
  };
}

class DeliverooIntegration {
  final String merchantId;
  final List<DeliverooAccount> accounts;
  final String primaryLocationId;

  DeliverooIntegration({
    required this.merchantId,
    required this.accounts,
    required this.primaryLocationId,
  });

  DeliverooAccount? getAccountByLocationId(String locationId) {
    try {
      return accounts.firstWhere((a) => a.locationId == locationId);
    } catch (e) {
      return null;
    }
  }

  DeliverooAccount get primaryAccount {
    return getAccountByLocationId(primaryLocationId) ?? accounts.first;
  }

  bool get hasMultipleAccounts => accounts.length > 1;

  factory DeliverooIntegration.fromJson(Map<String, dynamic> json) {
    final accountsList = (json['accounts'] as List<dynamic>?)
        ?.map((a) => DeliverooAccount.fromJson(a as Map<String, dynamic>))
        .toList() ?? [];

    return DeliverooIntegration(
      merchantId: json['merchant_id'] as String,
      accounts: accountsList,
      primaryLocationId: json['primary_location_id'] as String? ?? (accountsList.isNotEmpty ? accountsList.first.locationId : ''),
    );
  }

  Map<String, dynamic> toJson() => {
    'merchant_id': merchantId,
    'accounts': accounts.map((a) => a.toJson()).toList(),
    'primary_location_id': primaryLocationId,
  };
}
```

---

## 🎯 Services

### ubereats_service.dart

```dart
import 'package:http/http.dart' as http;
import 'dart:convert';
import 'models/uber_eats_integration.dart';

class UberEatsService {
  final String apiBaseUrl;
  final String Function() getAuthToken;

  UberEatsService({
    required this.apiBaseUrl,
    required this.getAuthToken,
  });

  /// Récupère tous les comptes Uber Eats du merchant
  Future<UberEatsIntegration> getIntegration(String merchantId) async {
    final response = await http.get(
      Uri.parse('$apiBaseUrl/integrations/uber-eats?merchant_id=$merchantId'),
      headers: {
        'Authorization': 'Bearer ${getAuthToken()}',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to load Uber Eats integration');
    }

    return UberEatsIntegration.fromJson(jsonDecode(response.body));
  }

  /// Récupère un compte spécifique
  Future<UberEatsAccount> getAccount(String merchantId, String storeId) async {
    final response = await http.get(
      Uri.parse('$apiBaseUrl/integrations/uber-eats?merchant_id=$merchantId&store_id=$storeId'),
      headers: {
        'Authorization': 'Bearer ${getAuthToken()}',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to load Uber Eats account');
    }

    return UberEatsAccount.fromJson(jsonDecode(response.body));
  }

  /// Synce le menu Uber Eats
  /// Si storeId est fourni, syncer ce compte spécifique
  /// Sinon, syncer le compte primaire
  Future<void> syncMenu(String merchantId, {String? storeId}) async {
    String url = '$apiBaseUrl/menu/uber-eats/sync?merchant_id=$merchantId';
    if (storeId != null) {
      url += '&store_id=$storeId';
    }

    final response = await http.patch(
      Uri.parse(url),
      headers: {
        'Authorization': 'Bearer ${getAuthToken()}',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to sync menu');
    }
  }

  /// Met à jour la configuration d'un compte
  Future<UberEatsAccount> updateAccount(
    String merchantId,
    String storeId,
    Map<String, dynamic> data,
  ) async {
    final response = await http.patch(
      Uri.parse('$apiBaseUrl/integrations/uber-eats?merchant_id=$merchantId&store_id=$storeId'),
      headers: {
        'Authorization': 'Bearer ${getAuthToken()}',
        'Content-Type': 'application/json',
      },
      body: jsonEncode(data),
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to update account');
    }

    return UberEatsAccount.fromJson(jsonDecode(response.body));
  }

  /// Déconnecte un compte
  Future<void> disconnectAccount(String merchantId, String storeId) async {
    final response = await http.delete(
      Uri.parse('$apiBaseUrl/integrations/uber-eats?merchant_id=$merchantId&store_id=$storeId'),
      headers: {
        'Authorization': 'Bearer ${getAuthToken()}',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to disconnect account');
    }
  }
}
```

### deliveroo_service.dart

Identique à `ubereats_service.dart`, en remplaçant:
- `store_id` → `location_id`
- `UberEatsAccount` → `DeliverooAccount`
- `UberEatsIntegration` → `DeliverooIntegration`
- `/integrations/uber-eats` → `/integrations/deliveroo`
- `/menu/uber-eats` → `/menu/deliveroo`

---

## 💾 State Management (Provider Pattern)

### uber_eats_provider.dart

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'models/uber_eats_integration.dart';
import 'services/ubereats_service.dart';

final uberEatsServiceProvider = Provider((ref) {
  return UberEatsService(
    apiBaseUrl: 'https://api.example.com',
    getAuthToken: () => ref.read(authTokenProvider),
  );
});

/// Récupère l'intégration Uber Eats (tous les comptes)
final uberEatsIntegrationProvider = FutureProvider.family<UberEatsIntegration, String>(
  (ref, merchantId) async {
    final service = ref.read(uberEatsServiceProvider);
    return service.getIntegration(merchantId);
  },
);

/// Compte Uber Eats sélectionné (stocké localement)
final selectedUberEatsStoreIdProvider = StateProvider<String?>((ref) => null);

/// Compte Uber Eats actuel (basé sur la sélection)
final currentUberEatsAccountProvider = FutureProvider<UberEatsAccount?>((ref) async {
  final merchantId = ref.read(authTokenProvider); // À adapter selon votre logique
  final storeId = ref.watch(selectedUberEatsStoreIdProvider);

  if (storeId == null) {
    final integration = await ref.watch(uberEatsIntegrationProvider(merchantId).future);
    return integration.primaryAccount;
  }

  final service = ref.read(uberEatsServiceProvider);
  return service.getAccount(merchantId, storeId);
});
```

---

## 🎨 Exemple d'Usage dans une Vue

### uber_eats_settings_screen.dart

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class UberEatsSettingsScreen extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final integration = ref.watch(uberEatsIntegrationProvider('merchant-123'));
    final selectedStoreId = ref.watch(selectedUberEatsStoreIdProvider);

    return integration.when(
      data: (data) {
        // Sélectionner le compte primaire par défaut
        if (selectedStoreId == null) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            ref.read(selectedUberEatsStoreIdProvider.notifier).state = data.primaryStoreId;
          });
        }

        return SingleChildScrollView(
          child: Column(
            children: [
              // Select déroulant: seulement si 2+ comptes
              if (data.hasMultipleAccounts) ...[
                Padding(
                  padding: EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Compte Uber Eats'),
                      DropdownButton<String>(
                        value: selectedStoreId ?? data.primaryStoreId,
                        isExpanded: true,
                        items: data.accounts.map((account) {
                          return DropdownMenuItem(
                            value: account.storeId,
                            child: Text(account.storeId),
                          );
                        }).toList(),
                        onChanged: (storeId) {
                          if (storeId != null) {
                            ref.read(selectedUberEatsStoreIdProvider.notifier).state = storeId;
                          }
                        },
                      ),
                    ],
                  ),
                ),
              ],

              // Configuration du compte sélectionné
              _UberEatsAccountDetails(storeId: selectedStoreId ?? data.primaryStoreId),
            ],
          ),
        );
      },
      loading: () => Center(child: CircularProgressIndicator()),
      error: (error, stack) => Center(child: Text('Erreur: $error')),
    );
  }
}

class _UberEatsAccountDetails extends ConsumerWidget {
  final String storeId;

  const _UberEatsAccountDetails({required this.storeId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final account = ref.watch(currentUberEatsAccountProvider);

    return account.when(
      data: (data) {
        if (data == null) return Text('Compte non trouvé');

        return Padding(
          padding: EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ListTile(
                title: Text('Statut'),
                trailing: data.enabled
                    ? Chip(label: Text('Actif'), backgroundColor: Colors.green)
                    : Chip(label: Text('Inactif'), backgroundColor: Colors.grey),
              ),
              ListTile(
                title: Text('Commission: ${data.commissionRate}%'),
              ),
              ListTile(
                title: Text('Temps de préparation: ${data.estimatedPreparationTime}'),
              ),
              if (data.lastSync != null)
                ListTile(
                  title: Text('Dernière sync: ${data.lastSync}'),
                ),
              SizedBox(height: 16),
              ElevatedButton(
                onPressed: () {
                  // TODO: Implémenter la sync
                },
                child: Text('Synchroniser'),
              ),
            ],
          ),
        );
      },
      loading: () => Center(child: CircularProgressIndicator()),
      error: (error, stack) => Center(child: Text('Erreur: $error')),
    );
  }
}
```

---

## 📝 Checklist Phase 4 Flutter

- [ ] Modèles UberEatsAccount et UberEatsIntegration créés
- [ ] Modèles DeliverooAccount et DeliverooIntegration créés
- [ ] Service ubereats_service.dart adapté
- [ ] Service deliveroo_service.dart adapté
- [ ] Provider pour la sélection de compte créé
- [ ] Tests: vérifier fromJson/toJson fonctionne
- [ ] Tests: affichage du select avec 2+ comptes
- [ ] Tests: affichage sans select avec 1 seul compte
- [ ] Migration: anciennes références à un seul compte mises à jour

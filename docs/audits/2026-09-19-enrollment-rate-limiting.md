# Rate limiting des endpoints d'enrôlement device — besoin identifié, non implémenté

Date : 2026-09-19
Statut : **constat documenté, aucun code écrit** — chantier transverse, à coordonner
Origine : conception du module CDS (`wello-resto-customer-display-system`, décision D14)
Concerne : `/kiosk/auth/*`, futur `/cds/auth/*`, et tout futur endpoint d'enrôlement device

---

## 1. Constat

**Il n'existe aucun middleware de rate limiting dans cette API.** Vérifié le 2026-09-19 :
aucune occurrence de `RateLimit` / `rateLimit` / `ratelimit` dans `internal/middleware/`
ni dans `cmd/api/routes.go`.

Or trois endpoints publics acceptent un secret court et sont montés sans aucune limite de
débit ([cmd/api/routes.go](../../cmd/api/routes.go), bloc `/kiosk`) :

```go
r.Route("/kiosk", func(r chi.Router) {
    r.Post("/auth/enroll", kioskHandler.EnrollDevice)          // code d'enrôlement
    r.Post("/auth/token/refresh", kioskHandler.RefreshDeviceToken)
    r.Post("/auth/reclaim", kioskHandler.ReclaimDevice)        // device_id + PIN admin
    // ...
```

`POST /kiosk/auth/reclaim` dispose d'un verrou applicatif (`adminPinMaxAttempts = 5`,
lockout Redis par kiosk — voir `kiosk.Service`, `AdminPinLockoutError`). **`enroll` et
`token/refresh` n'ont rien.**

## 2. Pourquoi ça devient un vrai problème

Le code d'enrôlement kiosk est solide : 8 caractères sur un alphabet de 32 symboles sans
ambiguïté visuelle (`generateEnrollmentCode`, [internal/modules/kiosk/service.go:2307](../../internal/modules/kiosk/service.go)),
soit **32⁸ ≈ 1,1 × 10¹²** combinaisons. À ce niveau, l'absence de rate limiting reste
théorique.

Le module CDS en préparation impose un code à **6 chiffres** : la saisie se fait au pavé
directionnel d'une télécommande, sur un écran non tactile — un code alphanumérique y est
ingérable. L'espace tombe à **10⁶**, soit un million de fois moins.

Deux propriétés du schéma transforment alors l'absence de limite en vulnérabilité réelle :

1. **`kiosk_enrollment_codes.code_hash` porte un index UNIQUE global.** Le code n'est pas
   scopé à un merchant : il *est* la clé de recherche
   (`GetEnrollmentCodeByHash`, [internal/modules/kiosk/repository.go:22](../../internal/modules/kiosk/repository.go)).
   Une tentative aveugle est donc testée contre **l'union de tous les codes en attente de
   la plateforme**, pas contre un seul.
2. **Le coût d'une tentative est celui d'une requête HTTP.** Pas de captcha, pas de délai,
   pas de compteur.

Ordre de grandeur : avec ~50 codes en attente sur la plateforme à un instant donné, une
tentative aveugle a une chance sur 20 000 d'aboutir. À 100 tentatives par seconde, cela
donne un enrôlement pirate **toutes les trois minutes environ**.

Un device ainsi enrôlé obtient un token valide sur le merchant visé. Pour un CDS, cela
expose la liste des commandes en cours, **prénoms clients compris** — donnée personnelle.
Pour une borne kiosk, le token ouvre en plus `POST /kiosk/orders`.

## 3. Ce qu'il faudrait

Un middleware générique `middleware.RateLimit`, adossé à Redis (déjà injecté partout dans
`SetupRoutes`), appliqué au minimum sur :

| Route | Limite proposée |
|---|---|
| `POST /kiosk/auth/enroll` | 5 / min / IP + plafond global |
| `POST /kiosk/auth/reclaim` | 5 / min / IP (complète le lockout par kiosk déjà présent) |
| `POST /kiosk/auth/token/refresh` | 20 / min / IP |
| `POST /cds/auth/*` (à venir) | idem |

Caractéristiques attendues :

- fenêtre glissante sur Redis, clé par IP **et** compteur global par route — l'IP seule ne
  protège pas d'un attaquant distribué, le global seul pénaliserait un merchant légitime ;
- réponse `429` avec `Retry-After`, dans le format d'erreur maison
  (`models.SendJSON` / `models.SendErrorJSON`) ;
- **invalidation du code visé après N échecs** le concernant, en complément du débit — un
  code brûlé ne doit pas rester exploitable jusqu'à son expiration naturelle ;
- attention au `r.RemoteAddr` derrière le reverse proxy de l'hébergeur : la vraie IP
  client est probablement dans `X-Forwarded-For`. À vérifier avant de clé-er dessus, sans
  quoi la limite s'appliquerait à l'IP du proxy et bloquerait tout le monde.

## 4. Mesures complémentaires, indépendantes du middleware

Applicables même sans rate limiting, et à retenir pour tout module d'enrôlement :

- **TTL court** sur le code d'enrôlement (`KIOSK_ENROLLMENT_CODE_TTL_MINUTES`) : 10 min
  suffisent, le restaurateur génère et saisit dans la foulée. Le TTL ne limite pas le
  débit des tentatives, mais il réduit la taille de la cible (le nombre de codes valides
  simultanés).
- **Purge des codes expirés**, pour la même raison.
- Ne **pas** élargir l'usage de codes courts à d'autres flux sans traiter ce point.

## 5. Statut

**Non implémenté.** Le chantier est transverse (il touche des routes kiosk en production)
et sort du périmètre du module CDS pris isolément — il est donc laissé à une décision
d'équipe plutôt qu'embarqué unilatéralement dans un lot CDS.

Le module CDS est construit sans en dépendre : ses endpoints d'enrôlement fonctionnent,
avec un TTL court et la même mécanique de hachage que kiosk. **Mais tant que ce middleware
n'existe pas, le code à 6 chiffres du CDS reste le maillon faible décrit en §2** — c'est
un risque accepté et tracé, pas un oubli.

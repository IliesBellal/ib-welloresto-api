# Images : domaine public R2 et redimensionnement Cloudflare

Contexte : les images produit sont stockées telles quelles dans R2 (WebP 2000×2000, PNG jusqu'à 2,4 Mo), ce qui fait saccader les listes du back-office. Les miniatures sont désormais produites à la volée par Cloudflare Image Transformations ; R2 ne stocke que l'original.

## Architecture

- Bucket R2 public, servi sur `https://storage.welloresto.fr` (domaine personnalisé, DNS chez Cloudflare). L'ancienne URL `https://pub-….r2.dev` reste active pendant la transition.
- Les clés R2 ne changent pas (`wello_resto_images_storage/merchants/{id}/…`).
- Redimensionnement : `https://storage.welloresto.fr/cdn-cgi/image/width=400,format=auto/<clé>` (réglages en préfixe de chemin, pas en query string). Les réglages sont séparés par des virgules.
- Transformations restreintes à `storage.welloresto.fr`.

## Coût (décisions)

- R2 ne facture que l'original (stockage + opérations). Les variantes vivent dans le cache Cloudflare, pas dans le bucket.
- Image Transformations facture à la transformation *unique* : une image source × un jeu de réglages, comptée une fois par mois quel que soit le trafic. Le coût dépend donc du nombre de largeurs distinctes : toutes les applications doivent se limiter à une liste fermée de 2 ou 3 largeurs, avec `format=auto`, sans `dpr` ni largeur calculée par écran. Vérifier les tarifs en vigueur sur Cloudflare.

## Passage de r2.dev à storage.welloresto.fr (partie B)

1. `r2.Client.GetKeyFromURL` reconnaît le domaine courant **et** tout `https://*.r2.dev` : sans cela, les suppressions d'anciennes images échoueraient en silence après le changement de `R2_PUBLIC_BASE_URL` (fichiers orphelins).
2. Déployer ce code.
3. Mettre `R2_PUBLIC_BASE_URL=https://storage.welloresto.fr` sur l'environnement.
4. Réécrire les URLs en base : `cmd/rewrite_storage_urls`.
   - Sans `--apply` : liste les colonnes et le nombre de lignes concernées, ne modifie rien.
   - Avec `--apply` : réécrit tout dans une seule transaction (tout ou rien). Le chemin et le `?v=…` sont conservés.
   - Les colonnes sont découvertes via `information_schema` (texte, varchar, json, jsonb). Les tables dont le nom correspond à `--exclude` (`fiscal|chain|audit|journal|archive|_log`) sont listées mais jamais modifiées : contenu scellé ou historique (chaînes fiscales HMAC, journaux). Leurs anciennes URLs restent valides tant que `r2.dev` est actif.
   - Effets de bord : les colonnes `updated_at` à trigger sont rafraîchies sur les lignes touchées ; le cache du menu (Redis) garde les anciennes URLs jusqu'à son renouvellement, sans conséquence tant que `r2.dev` répond.
5. Ne désactiver `r2.dev` qu'une fois les trois environnements migrés et les clients mis à jour (POS, Kiosk, CDS ont des URLs en cache).

### Staging, état au 2026-11-20

Simulation faite : 16 colonnes, 139 valeurs (dont `products.image_url` 99, `cleaning_executions.photo_url` 14, `configurable_attribute_options.image_url` 6). 4 colonnes ignorées (logs d'API et audit).

## À faire ensuite (partie C)

Les applications demandent les petites tailles : back-office, puis ScanNOrder, puis Kiosk / POS / CDS. Liste fermée de largeurs à fixer avant de commencer.

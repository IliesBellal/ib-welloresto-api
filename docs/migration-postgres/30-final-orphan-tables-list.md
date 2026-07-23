# 30 - Liste finale des tables orphelines (etat post Tiers 1-4)

Date: 2026-07-20
Branche: migration/postgres

## Methode reappliquee (etat actuel du code)

Methode identique a 03-table-usage-audit.md, relancee sur l'arbre courant:
- Recherche des usages SQL via FROM, JOIN, INSERT INTO, UPDATE, DELETE FROM
- Perimetre: internal/, cmd/, migrations/ sur fichiers .go et .sql
- Base des tables candidates: CREATE TABLE du dump docs/migration-postgres/wello-resto-mysql-ddl.md

Resultat de recomputation:
- Tables source detectees dans le DDL: 181
- Tables detectees comme referencees par le code/migrations: 312 tokens SQL uniques (dont aliases/CTE), couvrant les tables vivantes
- Tables non referencees par le code Go vivant: 37

Note:
- La liste brute contient user_vacations, mais ce cas est deja tranche dans cette conversation comme VIVANTE.
- La liste orpheline finale a trancher pour la copie de donnees Postgres est donc de 36 tables.

## Cas deja tranches dans cette conversation

### Cas explicitement tranches

| Table | Statut final | Recommendation | Nombre de lignes |
|---|---|---|---|
| user_vacations | VIVANTE (hors liste orpheline) | N/A (inclure dans la migration active) | a verifier |
| average_distribution_time_by_category | ORPHELINE CONFIRMEE | ARCHIVER | a verifier |
| average_distribution_time_history | ORPHELINE CONFIRMEE | ARCHIVER | a verifier |
| stock_movements_desc | ORPHELINE CONFIRMEE (obsolete, migration done/004) | EXCLURE | a verifier |
| stock_movements_source | ORPHELINE CONFIRMEE (obsolete, migration done/004) | EXCLURE | a verifier |

## Orphelines confirmees (hors cas ambigus)

Tables sans reference dans le code Go vivant et sans dependance metier evidente dans l'etat actuel.

| Table | Recommendation (1 ligne) | Nombre de lignes |
|---|---|---|
| api_calls | ARCHIVER (journal technique potentiellement utile en tracabilite) | a verifier | Ilies BELLAL => supprimer
| broadcast_list | EXCLURE (fonctionnalite legacy non branchee) | a verifier | Ilies BELLAL => supprimer
| calendar | EXCLURE (table legacy non consommee) | a verifier | Ilies BELLAL => supprimer
| cash_reports | ARCHIVER (historique potentiellement comptable) | a verifier | Ilies BELLAL => supprimer
| consumables | EXCLURE (aucune consommation applicative) | a verifier | Ilies BELLAL => supprimer
| integration_deliveroo_attributes_mapping | EXCLURE (mapping non implemente cote code vivant) | a verifier |
| integration_deliveroo_components_mapping | EXCLURE (mapping non implemente cote code vivant) | a verifier |
| integration_uber_eats_components_mapping | EXCLURE (mapping non implemente cote code vivant) | a verifier |
| integration_uber_eats_reports | EXCLURE (table de reporting legacy non lue) | a verifier | Ilies BELLAL => supprimer
| invoices | ARCHIVER (valeur potentielle comptable/legal) | a verifier | Ilies BELLAL => conserver
| migration_users | EXCLURE (table de transit de migration ponctuelle) | a verifier | Ilies BELLAL => supprimer
| order_changes_log | ARCHIVER (historique de changements a valeur de tracabilite) | a verifier | Ilies BELLAL => supprimer
| order_ratings | EXCLURE (feature non active dans le code vivant) | a verifier | Ilies BELLAL => supprimer
| pictures | EXCLURE (aucune lecture/ecriture dans l'API vivante) | a verifier | Ilies BELLAL => supprimer
| product_ratings | EXCLURE (feature non active dans le code vivant) | a verifier | Ilies BELLAL => supprimer
| stock_evolution_records | ARCHIVER (historique stock potentiellement utile en audit) | a verifier | Ilies BELLAL => supprimer
| timezone_info | EXCLURE (aucun usage applicatif detecte) | a verifier | Ilies BELLAL => supprimer
| z_platform_daily_activity_recording | ARCHIVER (historique d'activite plateforme) | a verifier | Ilies BELLAL => supprimer

## Cas encore incertains (A DISCUTER)

Ces tables sont orphelines au grep, mais avec ambiguite metier (renommage legacy probable, donnees RH/marketing/caisse, ou implementation partielle).

| Table | Recommendation (1 ligne) | Nombre de lignes |
|---|---|---|
| hours_amendments | A DISCUTER (table creee en migration mais jamais consommee par le code vivant) | a verifier | Ilies BELLAL => conserver
| cash_funds | A DISCUTER (possible ancetre de cash_registers.cash_fund) | a verifier | Ilies BELLAL => supprimer
| category_discount | A DISCUTER (possible ancien modele de remise par categorie) | a verifier | Ilies BELLAL => supprimer
| checkout_orderitems | A DISCUTER (possible table panier legacy avant orderitems) | a verifier | Ilies BELLAL => supprimer
| customer_advertisement_emails | A DISCUTER (possible ancetre du consentement marketing sur customer) | a verifier | Ilies BELLAL => supprimer
| employment_agreement | A DISCUTER (donnees RH/contractuelles potentiellement sensibles) | a verifier | Ilies BELLAL => supprimer
| employment_contract | A DISCUTER (donnees RH/contractuelles potentiellement sensibles) | a verifier | Ilies BELLAL => supprimer
| merchant_code | A DISCUTER (fonction metier historique non identifiee) | a verifier | Ilies BELLAL => supprimer
| notifications | A DISCUTER (historique notifications possiblement gere hors module actuel) | a verifier | Ilies BELLAL => supprimer
| planned_shifts | A DISCUTER (renommage legacy probable vers planning_shifts) | a verifier | Ilies BELLAL => supprimer
| planning_roles | A DISCUTER (renommage legacy probable vers planning_positions) | a verifier | Ilies BELLAL => supprimer
| shift_templates | A DISCUTER (renommage legacy probable vers planning_shift_templates) | a verifier | Ilies BELLAL => supprimer
| shift_templates_items | A DISCUTER (renommage legacy probable vers planning_shift_templates) | a verifier | Ilies BELLAL => supprimer
| users_nfc_tags | A DISCUTER (champ NFC present cote payload mais backend SQL absent) | a verifier | Ilies BELLAL => supprimer

## Decisionnel rapide pour la copie Postgres

- Exclusion immediate recommandee: stock_movements_desc, stock_movements_source, broadcast_list, calendar, consumables, integration_deliveroo_attributes_mapping, integration_deliveroo_components_mapping, integration_uber_eats_components_mapping, integration_uber_eats_reports, migration_users, order_ratings, pictures, product_ratings, timezone_info.
- Archivage recommande avant exclusion du schema actif: average_distribution_time_by_category, average_distribution_time_history, api_calls, cash_reports, invoices, order_changes_log, stock_evolution_records, z_platform_daily_activity_recording.
- A arbitrer metier avant gel de la copie: toutes les tables du bloc A DISCUTER.

## Sources utilisees

- docs/migration-postgres/03-table-usage-audit.md
- docs/migration-postgres/14-tier1-conversion-log.md
- docs/migration-postgres/25-tier2-conversion-log.md
- docs/migration-postgres/27-tier3-conversion-log.md
- docs/migration-postgres/29-tier4-conversion-log.md
- docs/migration-postgres/wello-resto-mysql-ddl.md
- migrations/done/004_migrate_stock_movements_text_values.sql

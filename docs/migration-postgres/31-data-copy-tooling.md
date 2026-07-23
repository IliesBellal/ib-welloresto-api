# 31 - Data Copy Tooling (offline, sans donnees reelles)

Date: 2026-07-20
Branche: migration/postgres

## Objectif

Preparer un outillage local pour recevoir des exports MySQL (phpMyAdmin) et produire des CSV compatibles avec le chargement Postgres, sans connexion a une base et sans donnees reelles.

Le dossier de travail dedie est:
- data-migration/

Son .gitignore local bloque le commit de tout contenu sauf:
- data-migration/.gitignore
- data-migration/README.md
- data-migration/transform_mysql_csv.py

## Flux attendu

1. Exporter une table MySQL depuis phpMyAdmin (CSV ou SQL par table).
2. Deposer l'export dans data-migration/.
3. Executer le script sur la table cible:
   - inspection (savoir si transformation requise)
   - transformation CSV (si necessaire)
4. Utiliser le CSV produit pour le chargement Postgres.

Aucune execution contre une base n'est requise a ce stade.

## Script

Fichier:
- data-migration/transform_mysql_csv.py

Source de verite utilisee par le script:
- docs/migration-postgres/04-schema-postgres-target.sql
- docs/migration-postgres/13-merchant-id-schema-update.md

Le script detecte automatiquement, pour la table demandee:
- colonnes boolean a convertir (`1/0` -> `true/false`)
- colonnes `merchant_id` (liste sourcee depuis les 72 colonnes du rapport 13) a conserver en texte
- colonnes identity sentinelles explicites a signaler pour chargement avec `OVERRIDING SYSTEM VALUE` (cas connu: `tva_categories.tva_id = -1`)

## Commandes d'usage

Inspection d'une table:

```bash
python data-migration/transform_mysql_csv.py inspect --table tva_categories
```

Transformation d'un CSV:

```bash
python data-migration/transform_mysql_csv.py transform --table tva_categories --input data-migration/tva_categories.mysql.csv --output data-migration/tva_categories.pg.csv
```

Lister les tables par besoin de transformation:

```bash
python data-migration/transform_mysql_csv.py list --mode all
python data-migration/transform_mysql_csv.py list --mode transform
python data-migration/transform_mysql_csv.py list --mode direct
```

## Regle de decision automatique

Pour une table donnee:
- `needs_transformation = true` si au moins un des points suivants existe:
  - colonne boolean
  - colonne `merchant_id` du rapport 13
  - colonne identity sentinelle explicite
- sinon `needs_transformation = false` (chargement direct possible)

## Table sentinelle identity connue

- `tva_categories.tva_id` est une identity et peut contenir un ID explicite sentinelle `-1` (documente dans 22-cash-register-procedures-translation.md).
- Le script le signale via:
  - `identity_columns`
  - `requires_overriding_system_value = true`

## Inventaire actuel (derive du schema cible)

Resultat obtenu via:
- `python data-migration/transform_mysql_csv.py list --mode all`

### Tables necessitant une transformation

api_request_logs, app_version_merchant, audit_logs, availabilities, availabilities_products, availabilities_schedules, available_languages, average_distribution_time, average_distribution_time_by_category, average_distribution_time_history, barcodes, booking_duration_rules, bookings, bookings_settings, cash_desks, cash_funds, cash_registers, cash_registers_custom_items, cash_reports, category_discount, cleaning_executions, cleaning_sessions, cleaning_surfaces, cleaning_zones, component_category, components, configurable_attributes, consumables, customer, customer_loyalty_programs, customer_rewards, delays, deletion_reasons, delivery_session, discounts, discounts_products, discounts_products_options, discounts_schedules, employee_documents, employees, employment_agreement, employment_contract, expiration_dates, extra, floor_areas, floor_obstacles, floors, goods_receipts, haccp_corrective_actions, haccp_settings, holiday_calendar, hours_amendments, hours_of_operation, integration_deliveroo, integration_deliveroo_attributes_mapping, integration_deliveroo_components_mapping, integration_deliveroo_options_mapping, integration_deliveroo_products_mapping, integration_uber_direct, integration_uber_eats, integration_uber_eats_attributes_mapping, integration_uber_eats_components_mapping, integration_uber_eats_options_mapping, integration_uber_eats_products_mapping, invoices, kiosk_settings, kiosks, labor_rules, locations, marketing_categories, merchant, merchant_code, merchant_marketing_settings, merchant_parameters, merchant_sms_monthly, merchant_translation_languages, migration_users, notifications, orderitems, orders, packages, payments, pictures, planned_shifts, planning_holiday_overrides, planning_leave_requests, planning_positions, planning_roles, planning_settings, planning_shift_swap_requests, planning_shift_templates, planning_shifts, planning_time_entries, planning_week_templates, planning_weeks, printers, product_configurable_attribute, productcateg, products, purchased_components, qrcodes, receipts, recipes, requires, restaurant_ticket, scannorder_settings, services_performed, shift_templates, stock_movements, stripe_accounts, subscription_invoices, subscriptions, sys_attendance_sources, sys_contract_types, sys_planning_event_types, tags, temperature_reading_corrective_actions, temperature_readings, temperature_sessions, temperature_zones, tva_categories, users, users_devices, users_nfc_tags, users_rights, welloresto_stripe_customers, without

### Tables en chargement direct

allergens, api_calls, app_version, booked_location, booking_events, booking_waitlist, brands, broadcast_list, calendar, cash_registers_items, checkout_orderitems, configurable_attribute_options, customer_advertisement_emails, customer_loyalty_program_reward_products, customer_loyalty_program_target_products, customer_loyalty_progress, customer_loyalty_progress_order, delivery_position, delivery_session_order, device_link, external_tokens, firebase_fcm_access_token, integration_uber_eats_reports, kiosk_device_tokens, kiosk_enrollment_codes, labels, merchant_google_maps_monthly, order_changes_log, order_comments, order_item_configuration, order_location, order_ratings, planning_day_comments, planning_revenue_forecasts, planning_week_template_shifts, product_allergens, product_marketing_categories, product_ratings, product_tags, scannorder_session, session_orderitem, shift_templates_items, stock_evolution_records, stock_movements_desc, stock_movements_source, stripe_payments, sub_cash_registers, timezone_info, unit_of_measure, unit_of_measure_convert, unit_of_measure_desc, upsell_suggestions, user_vacations, z_platform_daily_activity_recording

## Remarques

- Le script transforme effectivement les booleens au niveau CSV.
- Les colonnes `merchant_id` du rapport 13 sont gardees en texte (pas de cast numerique).
- Les colonnes identity sentinelles explicites sont signalees au niveau inspection pour piloter la strategie de chargement SQL (`OVERRIDING SYSTEM VALUE` quand necessaire).
- Le reste des colonnes est transmis tel quel.

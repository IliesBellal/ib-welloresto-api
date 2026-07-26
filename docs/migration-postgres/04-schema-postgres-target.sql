--
-- 04-schema-postgres-target.sql
-- Schema PostgreSQL cible pour la migration MySQL/MariaDB -> PostgreSQL de u231520952_welloresto.
-- Genere a partir de docs/migration-postgres/wello-resto-mysql-ddl.md (dump phpMyAdmin du 2026-07-13,
-- MariaDB 11.8.8), table par table dans l'ordre du DDL source.
--
-- REGLES DE CONVERSION APPLIQUEES (voir 04-schema-mapping-notes.md pour le detail par table) :
--   * TINYINT(1)                -> BOOLEAN (defaults 0/1 -> false/true) ; exceptions signalees en commentaire
--   * INT/BIGINT UNSIGNED       -> INTEGER/BIGINT (controle unsigned perdu ; CHECK (col >= 0) ajoute
--                                  quand la colonne est un id ou une quantite)
--   * AUTO_INCREMENT            -> GENERATED ALWAYS AS IDENTITY
--                                  (! lors de la copie des donnees : INSERT ... OVERRIDING SYSTEM VALUE,
--                                   puis recaler la sequence avec setval)
--   * ENUM(...)                 -> CREATE TYPE <table>_<colonne>_enum AS ENUM (...)
--   * LONGTEXT + json_valid()   -> JSONB
--   * DATETIME / TIMESTAMP      -> TIMESTAMPTZ (les colonnes stockent de l'UTC d'apres l'audit ;
--                                  defaults current_timestamp()/utc_timestamp() -> now())
--   * ON UPDATE current_timestamp() : pas d'equivalent declaratif en PG -> supprime ; les colonnes
--                                  concernees sont listees en fin de fichier (triggers a decider)
--   * utf8mb3_bin/utf8mb4_bin   -> collation PG par defaut (deja sensible a la casse)
--   * *_unicode_ci/_uca1400_ai_ci -> collation PG par defaut (SENSIBLE a la casse : les colonnes texte
--                                  candidates a CITEXT/LOWER() sont listees dans les notes, non converties)
--   * FLOAT -> REAL, DOUBLE -> DOUBLE PRECISION, DECIMAL -> NUMERIC
--   * VARBINARY -> BYTEA
--   * DEFAULT '0000-00-00 00:00:00' (invalide en PG) -> default supprime, signale
--   * Index MySQL KEY/UNIQUE KEY -> CREATE INDEX / UNIQUE INDEX prefixes par le nom de table
--   * Pas de nouvelles FK : seules les 2 FK existant deja cote MySQL sont conservees ;
--     les candidates evidentes sont listees en commentaire au-dessus de chaque table.
--

BEGIN;

-- =====================================================================
-- TYPES ENUM (un par colonne ENUM MySQL, nommes <table>_<colonne>_enum)
-- =====================================================================

CREATE TYPE booking_waitlist_status_enum AS ENUM ('waiting', 'notified', 'seated', 'expired', 'cancelled');
CREATE TYPE cleaning_surfaces_frequency_unit_enum AS ENUM ('day', 'week', 'month');
CREATE TYPE discounts_discount_scope_enum AS ENUM ('PRODUCT', 'ORDER_TOTAL');
CREATE TYPE employees_role_enum AS ENUM ('employee', 'manager', 'admin');
CREATE TYPE floor_obstacles_type_enum AS ENUM ('wall', 'bar', 'stairs', 'door');
CREATE TYPE hours_amendments_type_enum AS ENUM ('permanent', 'temporary');
CREATE TYPE kiosks_status_enum AS ENUM ('pending', 'active', 'inactive', 'revoked');
CREATE TYPE planning_leave_requests_leave_type_enum AS ENUM ('paid', 'unpaid', 'sick', 'other');
CREATE TYPE planning_leave_requests_status_enum AS ENUM ('pending', 'approved', 'rejected', 'cancelled');
CREATE TYPE planning_shifts_status_enum AS ENUM ('planned', 'confirmed', 'done', 'cancelled');
CREATE TYPE planning_shift_swap_requests_status_enum AS ENUM ('pending', 'approved', 'rejected', 'cancelled');
CREATE TYPE planning_weeks_status_enum AS ENUM ('draft', 'published', 'locked');
CREATE TYPE temperature_readings_status_enum AS ENUM ('ok', 'alert', 'critical');
CREATE TYPE upsell_suggestions_channel_enum AS ENUM ('POS', 'SNO', 'KIOSK');
-- ---------------------------------------------------------------------
-- allergens
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE allergens (
    allergen_id varchar(35) NOT NULL,
    name varchar(50) NOT NULL,
    code varchar(12) NOT NULL,
    icon varchar(12) NOT NULL,
    color varchar(12) NOT NULL,
    PRIMARY KEY (allergen_id)
);

-- ---------------------------------------------------------------------
-- api_calls
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE api_calls (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id varchar(64) NOT NULL,
    query varchar(50) NOT NULL,
    uri text NOT NULL,
    date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- api_request_logs
--   payload: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE api_request_logs (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id bigint,
    merchant_id varchar(64),
    method varchar(10),
    url text,
    payload jsonb,
    status_code integer,
    ip varchar(45),
    created_at timestamptz DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- app_version
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE app_version (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    app_id varchar(25) NOT NULL,
    version_code integer NOT NULL,
    last_functional_version_code integer NOT NULL,
    download_url varchar(255) NOT NULL,
    release_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN app_version.app_id IS '0 => merchant / 1 => delivery / 2 => waiter';

-- ---------------------------------------------------------------------
-- app_version_merchant
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE app_version_merchant (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    version_code integer NOT NULL,
    merchant_id varchar(64) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- audit_logs
--   old_values: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   new_values: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE audit_logs (
    id varchar(64) NOT NULL,
    user_id varchar(36),
    merchant_id varchar(64),
    action varchar(50),
    resource_type varchar(50),
    resource_id varchar(36),
    old_values jsonb,
    new_values jsonb,
    previous_hash varchar(64),
    hash varchar(64) NOT NULL,
    created_at timestamptz DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- availabilities
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE availabilities (
    availability_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    availability_name varchar(50) NOT NULL,
    unavailable_message varchar(50) NOT NULL,
    available boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    update_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (availability_id)
);
COMMENT ON COLUMN availabilities.enabled IS '0 when availability is deleted';

-- ---------------------------------------------------------------------
-- availabilities_products
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : availability_id -> availabilities.availability_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE availabilities_products (
    availability_product_id varchar(50) NOT NULL,
    availability_id varchar(50) NOT NULL,
    product_id integer NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    update_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (availability_product_id)
);
COMMENT ON COLUMN availabilities_products.enabled IS '0 when deleted';

-- ---------------------------------------------------------------------
-- availabilities_schedules
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : schedule_id -> discounts_schedules.schedule_id
--   FK candidate (non creee) : availability_id -> availabilities.availability_id
-- ---------------------------------------------------------------------
CREATE TABLE availabilities_schedules (
    schedule_id varchar(50) NOT NULL,
    availability_id varchar(50) NOT NULL,
    day_of_week integer NOT NULL,
    available_from time NOT NULL,
    available_to time NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    update_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (schedule_id)
);
COMMENT ON COLUMN availabilities_schedules.enabled IS '0 if deleted';

-- ---------------------------------------------------------------------
-- available_languages
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE available_languages (
    code varchar(5) NOT NULL,
    name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (code)
);

-- ---------------------------------------------------------------------
-- average_distribution_time
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE average_distribution_time (
    merchant_id varchar(64) NOT NULL,
    distribution_time integer NOT NULL,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN average_distribution_time.distribution_time IS 'In seconds';

-- ---------------------------------------------------------------------
-- average_distribution_time_by_category
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE average_distribution_time_by_category (
    merchant_id varchar(64) NOT NULL,
    category varchar(11) NOT NULL,
    distribution_time integer NOT NULL,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN average_distribution_time_by_category.distribution_time IS 'In seconds';

-- ---------------------------------------------------------------------
-- average_distribution_time_history
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE average_distribution_time_history (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    category varchar(30) NOT NULL,
    distribution_time integer NOT NULL,
    calculation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_average_distribution_time_history_merchant_id ON average_distribution_time_history (merchant_id, category, calculation_date);

-- ---------------------------------------------------------------------
-- barcodes
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : component_id -> components.component_id
-- ---------------------------------------------------------------------
CREATE TABLE barcodes (
    barcode varchar(25) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    component_id integer NOT NULL,
    quantity integer NOT NULL DEFAULT 0,
    uom integer NOT NULL DEFAULT 0,
    last_scan timestamptz NOT NULL DEFAULT now(),
    price real NOT NULL DEFAULT 0,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merchant_id, barcode)
);
CREATE INDEX idx_barcodes_merchant_id ON barcodes (merchant_id, barcode);

-- ---------------------------------------------------------------------
-- booked_location
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : booking_id -> bookings.booking_id
--   FK candidate (non creee) : location_id -> locations.location_id
-- ---------------------------------------------------------------------
CREATE TABLE booked_location (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    booking_id integer NOT NULL,
    location_id integer NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_booked_location_uq_booked_location ON booked_location (booking_id, location_id);

-- ---------------------------------------------------------------------
-- bookings
--   booking_date_from: DEFAULT '0000-00-00 00:00:00' invalide en PG -> default supprime (NOT NULL conserve : a fournir a l'insertion)
--   booking_date_to: DEFAULT '0000-00-00 00:00:00' invalide en PG -> default supprime (NOT NULL conserve : a fournir a l'insertion)
--   creation_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : customer_id -> customer.customer_id
--   FK candidate (non creee) : deletion_reason_id -> deletion_reasons.deletion_reason_id
-- ---------------------------------------------------------------------
CREATE TABLE bookings (
    booking_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    booking_number varchar(6) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    order_id integer,
    party_size integer NOT NULL,
    customer_id integer NOT NULL,
    comment varchar(255),
    booking_date_from timestamptz NOT NULL,
    booking_date_to timestamptz NOT NULL,
    booking_duration integer NOT NULL,
    created_by varchar(20) NOT NULL,
    updated_by varchar(30),
    status varchar(20) NOT NULL,
    source varchar(16) NOT NULL DEFAULT 'staff',
    creation_date timestamptz NOT NULL DEFAULT now(),
    last_update_date timestamptz,
    sequence_number integer NOT NULL DEFAULT 0,
    deletion_date timestamptz,
    reminder_sent_at timestamptz,
    deletion_reason_id integer,
    deletion_reason_desc text,
    cancelled_by varchar(64),
    PRIMARY KEY (booking_id)
);
COMMENT ON COLUMN bookings.created_by IS 'user id';
COMMENT ON COLUMN bookings.status IS '-1 => deleted / 0 => finished / 1 => order opened / 2 => validated / 3 => pending validation';
COMMENT ON COLUMN bookings.cancelled_by IS 'SYSTEM | CUSTOMER | user_id staff';
CREATE UNIQUE INDEX uq_bookings_uq_bookings_merchant_number ON bookings (merchant_id, booking_number);

-- ---------------------------------------------------------------------
-- bookings_settings
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE bookings_settings (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    code varchar(30) NOT NULL,
    default_booking_duration integer NOT NULL DEFAULT 90,
    auto_accept_reserve_bookings boolean NOT NULL DEFAULT true,
    reserve_maximum_party_size integer NOT NULL DEFAULT 8,
    reserve_minimum_party_size integer,
    first_booking_offset_minutes integer NOT NULL DEFAULT 0,
    last_booking_offset_minutes integer NOT NULL DEFAULT 60,
    overbooking_percent integer,
    max_booking_horizon_days integer,
    min_booking_notice_minutes integer,
    cancel_booking_limit_offset_hours integer NOT NULL DEFAULT 48,
    sms_enabled boolean NOT NULL DEFAULT false,
    waitlist_enabled boolean NOT NULL DEFAULT false,
    waitlist_max_size integer NOT NULL DEFAULT 0,
    waitlist_slot_expiry_minutes integer NOT NULL DEFAULT 15,
    pending_expiration_hours integer NOT NULL DEFAULT 24,
    slot_interval_minutes integer NOT NULL DEFAULT 15,
    cancelable_by_customer boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- booking_duration_rules
--   collation table utf8mb4_uca1400_ai_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE booking_duration_rules (
    rule_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    min_party_size integer NOT NULL,
    max_party_size integer NOT NULL,
    duration_minutes integer NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id)
);

-- ---------------------------------------------------------------------
-- booking_events
--   metadata: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   collation table utf8mb4_uca1400_ai_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : booking_id -> bookings.booking_id
-- ---------------------------------------------------------------------
CREATE TABLE booking_events (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    booking_id integer,
    waitlist_id varchar(64),
    event_type varchar(64) NOT NULL,
    source varchar(64),
    actor varchar(64),
    metadata jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- booking_waitlist
--   collation table utf8mb4_uca1400_ai_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : customer_id -> customer.customer_id
-- ---------------------------------------------------------------------
CREATE TABLE booking_waitlist (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    customer_id varchar(64),
    party_size integer NOT NULL DEFAULT 1,
    customer_name varchar(255) NOT NULL,
    customer_phone varchar(50) NOT NULL,
    notes text,
    status booking_waitlist_status_enum NOT NULL DEFAULT 'waiting',
    notified_at timestamptz,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- brands
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE brands (
    brand_id varchar(35) NOT NULL,
    name varchar(50) NOT NULL,
    slug varchar(50) NOT NULL,
    logo_url varchar(255) NOT NULL,
    banner_url varchar(255) NOT NULL,
    description varchar(255) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (brand_id)
);

-- ---------------------------------------------------------------------
-- broadcast_list
--   create_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE broadcast_list (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    contact varchar(255) NOT NULL,
    create_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- calendar
--   date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE calendar (
    date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (date)
);

-- ---------------------------------------------------------------------
-- cash_desks
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE cash_desks (
    cash_desk_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(50) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cash_desk_id)
);

-- ---------------------------------------------------------------------
-- cash_funds
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE cash_funds (
    cash_fund_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    cash_register_id integer NOT NULL,
    sub_cash_register_id integer,
    user_id varchar(64) NOT NULL,
    initial_amount integer NOT NULL DEFAULT 0,
    expected_amount integer NOT NULL DEFAULT 0,
    actual_amount integer,
    opened boolean NOT NULL DEFAULT true,
    closed boolean NOT NULL DEFAULT false,
    start_date timestamptz NOT NULL DEFAULT now(),
    end_date timestamptz,
    closed_by integer,
    closure_comment varchar(255),
    last_assignment_reason varchar(50),
    PRIMARY KEY (cash_fund_id)
);

-- ---------------------------------------------------------------------
-- cash_registers
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : cash_desk_id -> cash_desks.cash_desk_id
--   FK candidate (non creee) : device_id -> device_link.device_id | users_devices.device_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE cash_registers (
    cash_register_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    cash_desk_id integer NOT NULL,
    device_id varchar(50) NOT NULL,
    user_id varchar(64) NOT NULL,
    cash_fund integer NOT NULL,
    final_cash_fund integer DEFAULT 0,
    start_date timestamptz NOT NULL,
    end_date timestamptz,
    closed boolean NOT NULL DEFAULT false,
    enclosed boolean NOT NULL DEFAULT false,
    closure_comment varchar(255) NOT NULL,
    closed_by varchar(25),
    hash varchar(64),
    signature text,
    previous_hash varchar(64),
    PRIMARY KEY (cash_register_id)
);
COMMENT ON COLUMN cash_registers.cash_fund IS 'in cents';

-- ---------------------------------------------------------------------
-- cash_registers_custom_items
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
-- ---------------------------------------------------------------------
CREATE TABLE cash_registers_custom_items (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    label varchar(25) NOT NULL,
    amount integer NOT NULL,
    merchant_id varchar(64),
    created_by varchar(35),
    enabled boolean NOT NULL DEFAULT true,
    cash_register_id integer NOT NULL,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN cash_registers_custom_items.amount IS 'In cents';

-- ---------------------------------------------------------------------
-- cash_registers_items
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
-- ---------------------------------------------------------------------
CREATE TABLE cash_registers_items (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    cash_register_id integer NOT NULL,
    mop varchar(10) NOT NULL,
    amount integer NOT NULL,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN cash_registers_items.amount IS 'in cents';

-- ---------------------------------------------------------------------
-- cash_reports
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : cash_desk_id -> cash_desks.cash_desk_id
-- ---------------------------------------------------------------------
CREATE TABLE cash_reports (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id varchar(64),
    merchant_id varchar(64) NOT NULL,
    cash_desk_id integer NOT NULL,
    period_from timestamptz,
    period_to timestamptz NOT NULL,
    creation_date timestamptz NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- category_discount
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : categ_id -> productcateg.categ_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE category_discount (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    categ_id integer NOT NULL,
    merchant_id varchar(64) NOT NULL,
    merchant_discount_id integer NOT NULL,
    discount_desc text NOT NULL,
    discount_order_type integer,
    discount_value double precision NOT NULL,
    discount_unit varchar(20) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    valid_from timestamptz,
    valid_to timestamptz,
    available_from time,
    available_to time,
    coupon_code text,
    min_order_value integer,
    min_order_unit varchar(20),
    max_discount_value integer,
    max_discount_unit varchar(20),
    discounted_qty integer,
    is_cumulative boolean NOT NULL DEFAULT false,
    active boolean NOT NULL,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN category_discount.categ_id IS 'merchantcategid';
COMMENT ON COLUMN category_discount.discount_desc IS 'Description';
COMMENT ON COLUMN category_discount.discount_order_type IS '0 = IN, 1 = DELIVERY, NULL = all';
COMMENT ON COLUMN category_discount.discount_unit IS 'PERCENTAGE | CURRENCY | NEWPRICE';
COMMENT ON COLUMN category_discount.valid_from IS 'UTC';
COMMENT ON COLUMN category_discount.valid_to IS 'UTC';
COMMENT ON COLUMN category_discount.available_from IS 'Merchant TimeZone';
COMMENT ON COLUMN category_discount.available_to IS 'Merchant TimeZone';
COMMENT ON COLUMN category_discount.min_order_unit IS 'CURRENCY | QUANTITY';
COMMENT ON COLUMN category_discount.active IS 'Discount activated or not';

-- ---------------------------------------------------------------------
-- checkout_orderitems
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE checkout_orderitems (
    link_key varchar(255) NOT NULL,
    user_code varchar(255) NOT NULL,
    order_item_id integer NOT NULL,
    quantity integer NOT NULL,
    PRIMARY KEY (link_key, user_code, order_item_id)
);

-- ---------------------------------------------------------------------
-- cleaning_executions
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE cleaning_executions (
    id varchar(64) NOT NULL,
    session_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    surface_id varchar(64) NOT NULL,
    comment text,
    photo_url text,
    status varchar(32) NOT NULL DEFAULT 'done',
    created_by varchar(64) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_cleaning_executions_idx_cleaning_executions_session_enabled ON cleaning_executions (session_id, enabled);
CREATE INDEX idx_cleaning_executions_idx_cleaning_executions_surface_enabled ON cleaning_executions (surface_id, enabled);
CREATE INDEX idx_cleaning_executions_idx_cleaning_executions_merchant_create ON cleaning_executions (merchant_id, created_at);

-- ---------------------------------------------------------------------
-- cleaning_sessions
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE cleaning_sessions (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    status varchar(32) NOT NULL DEFAULT 'done',
    created_by varchar(64) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_cleaning_sessions_idx_cleaning_sessions_merchant_enabled ON cleaning_sessions (merchant_id, enabled);
CREATE INDEX idx_cleaning_sessions_idx_cleaning_sessions_merchant_created ON cleaning_sessions (merchant_id, created_at);

-- ---------------------------------------------------------------------
-- cleaning_surfaces
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE cleaning_surfaces (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    zone_id varchar(64) NOT NULL,
    name varchar(255) NOT NULL,
    frequency_unit cleaning_surfaces_frequency_unit_enum NOT NULL,
    frequency_count integer NOT NULL DEFAULT 1,
    active boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_cleaning_surfaces_idx_cleaning_surfaces_merchant_enabled ON cleaning_surfaces (merchant_id, enabled);
CREATE INDEX idx_cleaning_surfaces_idx_cleaning_surfaces_zone_enabled ON cleaning_surfaces (zone_id, enabled);
CREATE INDEX idx_cleaning_surfaces_idx_cleaning_surfaces_merchant_zone ON cleaning_surfaces (merchant_id, zone_id);

-- ---------------------------------------------------------------------
-- cleaning_zones
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE cleaning_zones (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_cleaning_zones_idx_cleaning_zones_merchant_enabled ON cleaning_zones (merchant_id, enabled);
CREATE INDEX idx_cleaning_zones_idx_cleaning_zones_merchant_name ON cleaning_zones (merchant_id, name);

-- ---------------------------------------------------------------------
-- components
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE components (
    component_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(100) NOT NULL,
    component_price integer NOT NULL DEFAULT 0,
    category_id varchar(15),
    stock double precision NOT NULL DEFAULT 0,
    safety_stock real NOT NULL DEFAULT 0,
    safety_triggered boolean NOT NULL DEFAULT true,
    unit_of_measure integer NOT NULL,
    purchase_price integer NOT NULL DEFAULT 0,
    purchase_price_quantity real NOT NULL DEFAULT 1,
    purchase_unit_id varchar(35),
    auto_update_purchase_info boolean NOT NULL DEFAULT true,
    status varchar(20) NOT NULL DEFAULT '1',
    available boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    conservation_days integer,
    conservation_type varchar(20) DEFAULT 'froid',
    storage_temp_min real,
    storage_temp_max real,
    PRIMARY KEY (component_id)
);
COMMENT ON COLUMN components.safety_stock IS 'Minimum stock value before component automatically get set unavailable';
COMMENT ON COLUMN components.purchase_price IS 'in cents';
COMMENT ON COLUMN components.conservation_days IS 'Durée de conservation après ouverture/déconditionnement, en jours';
COMMENT ON COLUMN components.conservation_type IS 'Type de stockage : froid, congele, sec, ambiant';
COMMENT ON COLUMN components.storage_temp_min IS 'Température min de stockage en °C';
COMMENT ON COLUMN components.storage_temp_max IS 'Température max de stockage en °C';

-- ---------------------------------------------------------------------
-- component_category
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE component_category (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    merchant_categ_id varchar(11) NOT NULL,
    name text NOT NULL,
    categ_order integer NOT NULL,
    available boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- configurable_attributes
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE configurable_attributes (
    id varchar(64) NOT NULL,
    product_id integer NOT NULL,
    merchant_id varchar(64) NOT NULL,
    brand varchar(20) NOT NULL DEFAULT 'WELLO_RESTO',
    attribute_type varchar(20) NOT NULL DEFAULT 'CHECK',
    name varchar(50) NOT NULL,
    title varchar(80) NOT NULL,
    max_options integer NOT NULL,
    is_required boolean NOT NULL DEFAULT true,
    min_options integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN configurable_attributes.brand IS 'Origine de la création de l''attribut (WELLO_RESTO, UBER_EATS)';
COMMENT ON COLUMN configurable_attributes.attribute_type IS 'CHECK | QUANTITY';

-- ---------------------------------------------------------------------
-- configurable_attribute_options
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE configurable_attribute_options (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    configurable_attribute_id varchar(64) NOT NULL,
    title varchar(25) NOT NULL,
    max_quantity integer NOT NULL DEFAULT 1,
    extra_price integer NOT NULL DEFAULT 0,
    image_url varchar(500),
    enabled integer NOT NULL DEFAULT 1,
    PRIMARY KEY (id)
);
CREATE INDEX idx_configurable_attribute_options_configurable_attribute_id ON configurable_attribute_options (configurable_attribute_id);

-- ---------------------------------------------------------------------
-- consumables
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE consumables (
    consumable_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(50) NOT NULL,
    unit_of_measure integer NOT NULL,
    purchase_price integer,
    stock real NOT NULL DEFAULT 0,
    status boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    purchase_price_quantity integer,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (consumable_id)
);
COMMENT ON COLUMN consumables.purchase_price IS 'in cents';
COMMENT ON COLUMN consumables.purchase_price_quantity IS 'in uom of consumable';

-- ---------------------------------------------------------------------
-- customer
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   is_migrated: colonne retiree (voir 35-dead-columns-removal.md) - logique morte cote Go, aucune
--   exposition JSON ; reste en base MySQL source telle quelle
-- ---------------------------------------------------------------------
CREATE TABLE customer (
    customer_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    customer_brand varchar(20) NOT NULL DEFAULT 'WELLO_RESTO',
    customer_brand_id varchar(50),
    merchant_id varchar(64),
    customer_name varchar(50),
    customer_first_name varchar(50),
    customer_last_name varchar(50),
    customer_code varchar(4),
    customer_tel varchar(20),
    customer_temporary_phone varchar(20),
    customer_temporary_phone_code varchar(20),
    customer_email varchar(255),
    customer_address varchar(255),
    customer_floor_number varchar(11),
    customer_door_number varchar(25),
    customer_additional_address varchar(255),
    customer_business_name varchar(50),
    customer_birthdate date,
    customer_additional_info varchar(255),
    customer_temporary_address varchar(255),
    customer_temporary_lat double precision,
    customer_temporary_lng double precision,
    customer_temporary_floor_number integer,
    customer_temporary_door_number varchar(25),
    customer_temporary_additional_address varchar(255),
    customer_total_spent integer NOT NULL DEFAULT 0,
    customer_google_place_id varchar(255),
    customer_lat double precision,
    customer_lng double precision,
    customer_nb_orders integer NOT NULL DEFAULT 0,
    customer_nb_bookings integer NOT NULL DEFAULT 0,
    customer_zone_code varchar(4),
    customer_zone_updated_at timestamptz,
    last_order_date timestamptz,
    last_advertisement_date timestamptz,
    loyalty_reminder_count integer NOT NULL DEFAULT 0,
    advertising_consent boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    delivery_notes text,
    PRIMARY KEY (customer_id)
);
CREATE INDEX idx_customer_idx_customer_lookup ON customer (merchant_id, enabled, customer_code, customer_name, customer_tel, customer_address);

-- ---------------------------------------------------------------------
-- customer_advertisement_emails
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : customer_id -> customer.customer_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_advertisement_emails (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    customer_id varchar(30) NOT NULL,
    marketing_campaing_id varchar(20),
    reason varchar(100) NOT NULL,
    communication_date timestamptz NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- customer_loyalty_programs
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_loyalty_programs (
    id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(50) NOT NULL,
    description varchar(120) NOT NULL,
    type varchar(30) NOT NULL,
    target_value integer NOT NULL,
    target_order_type varchar(100) NOT NULL DEFAULT 'IN TAKE_AWAY DELIVERY',
    reward_type varchar(30) NOT NULL,
    reward_value integer NOT NULL,
    rewards_order_type varchar(100) NOT NULL DEFAULT 'IN TAKE_AWAY DELIVERY',
    min_order_value integer NOT NULL DEFAULT 500,
    max_discount_value integer,
    max_rewards_per_order integer NOT NULL DEFAULT 1,
    available boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN customer_loyalty_programs.type IS 'enum("orders_count", "total_spent", "products_count")';
COMMENT ON COLUMN customer_loyalty_programs.reward_type IS 'enum("fixed_discount", "percent_discount", "free_product")';
COMMENT ON COLUMN customer_loyalty_programs.min_order_value IS 'in cents';
COMMENT ON COLUMN customer_loyalty_programs.max_discount_value IS 'in cents';

-- ---------------------------------------------------------------------
-- customer_loyalty_program_reward_products
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_loyalty_program_reward_products (
    id varchar(50) NOT NULL,
    product_id varchar(50) NOT NULL,
    loyalty_program_id varchar(50) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- customer_loyalty_program_target_products
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_loyalty_program_target_products (
    id varchar(50) NOT NULL,
    product_id varchar(50) NOT NULL,
    loyalty_program_id varchar(50) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- customer_loyalty_progress
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : customer_id -> customer.customer_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_loyalty_progress (
    id varchar(64) NOT NULL,
    customer_id varchar(30) NOT NULL,
    loyalty_program_id varchar(64) NOT NULL,
    current_value integer NOT NULL,
    last_update timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- customer_loyalty_progress_order
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_loyalty_progress_order (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    loyalty_program_id varchar(64) NOT NULL,
    progress_id varchar(64) NOT NULL,
    order_id integer NOT NULL,
    increment_value integer NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- customer_rewards
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : customer_id -> customer.customer_id
-- ---------------------------------------------------------------------
CREATE TABLE customer_rewards (
    reward_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    customer_id varchar(30) NOT NULL,
    loyalty_program_id varchar(64) NOT NULL,
    reward_type varchar(30) NOT NULL,
    reward_order_type varchar(100) NOT NULL DEFAULT 'IN TAKE_AWAY DELIVERY',
    reward_value integer NOT NULL DEFAULT 0,
    is_used boolean NOT NULL DEFAULT false,
    issue_date timestamptz,
    usage_date timestamptz,
    used_on_order_id integer,
    creation_date timestamptz NOT NULL,
    PRIMARY KEY (reward_id)
);

-- ---------------------------------------------------------------------
-- delays
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE delays (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    description varchar(15) NOT NULL,
    short_description varchar(10) NOT NULL,
    duration integer NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN delays.id IS 'Do not delete records, place then as disabled instead';
COMMENT ON COLUMN delays.duration IS 'in seconds';
COMMENT ON COLUMN delays.enabled IS 'Do not delete records, place then as disabled instead';

-- ---------------------------------------------------------------------
-- deletion_reasons
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE deletion_reasons (
    deletion_reason_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    deletion_reason_type varchar(30),
    deletion_reason_object varchar(30) NOT NULL,
    deletion_reason_desc varchar(255) NOT NULL,
    requires_comment boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (deletion_reason_id)
);

-- ---------------------------------------------------------------------
-- delivery_position
--   id: UNSIGNED perdu (bigint UNSIGNED -> bigint)
--   delivery_session_id: UNSIGNED perdu (int(10) UNSIGNED -> integer)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE delivery_position (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL CHECK (id >= 0),
    user_id varchar(64) NOT NULL,
    delivery_session_id integer NOT NULL CHECK (delivery_session_id >= 0),
    lat numeric(10,7) NOT NULL,
    lng numeric(10,7) NOT NULL,
    heading real,
    accuracy real,
    speed real,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_delivery_position_idx_delivery_position_session ON delivery_position (delivery_session_id, recorded_at);
CREATE INDEX idx_delivery_position_idx_delivery_position_user ON delivery_position (user_id, recorded_at);

-- ---------------------------------------------------------------------
-- delivery_session
--   current_order_id: UNSIGNED perdu (int(10) UNSIGNED -> integer)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE delivery_session (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    start_date timestamptz NOT NULL,
    end_date timestamptz,
    distance integer NOT NULL DEFAULT 0,
    duration integer NOT NULL DEFAULT 0,
    status varchar(25) NOT NULL DEFAULT '1',
    current_order_id integer CHECK (current_order_id >= 0),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN delivery_session.start_date IS 'UTC';
COMMENT ON COLUMN delivery_session.distance IS 'in meters';
COMMENT ON COLUMN delivery_session.duration IS 'in seconds';

-- ---------------------------------------------------------------------
-- delivery_session_order
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : deletion_reason_id -> deletion_reasons.deletion_reason_id
-- ---------------------------------------------------------------------
CREATE TABLE delivery_session_order (
    delivery_session_id integer NOT NULL,
    order_id integer NOT NULL,
    priority integer NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'pending',
    arrived_at timestamptz,
    delivered_at timestamptz,
    failed_at timestamptz,
    canceled_at timestamptz,
    fail_reason varchar(255),
    deletion_reason_id varchar(20),
    deletion_comment varchar(255),
    PRIMARY KEY (delivery_session_id, order_id)
);

-- ---------------------------------------------------------------------
-- device_link
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : device_id -> users_devices.device_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE device_link (
    device_id varchar(50) NOT NULL,
    user_id varchar(20) NOT NULL,
    on_behalf_of varchar(50) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id)
);

-- ---------------------------------------------------------------------
-- discount_redemptions
--   nouvelle table (migrations/done/041_cart_discounts.up.sql, deja executee en MySQL ; non presente dans le dump wello-resto-mysql-ddl.md audite, meme situation que planning_day_comments/haccp_traceability, rapports 26/56) ; aucun module Go ne la lit/l'ecrit a ce jour (voir rapport 57 - schema pret, non cable)
--   id: BIGINT UNSIGNED AUTO_INCREMENT -> bigint identity + CHECK (perte du UNSIGNED)
--   order_id: BIGINT UNSIGNED alors que orders.order_id est integer (int(11) signe cote MySQL source) -> incoherence de type preexistante, deja signalee par le commentaire de la migration elle-meme ("types exacts de colonnes ne sont pas garantis") ; aucune jointure Go vivante actuellement (voir rapport 57)
--   customer_id: varchar(64) alors que customer.customer_id est integer (int(11) cote MySQL source) -> incoherence de type plus marquee (varchar vs int), meme motif, sans impact tant qu'aucun code ne l'exploite
--   collation non explicite (CHARSET=utf8mb4 sans COLLATE dans la migration, contrairement au reste du fichier) -> sans impact sur la traduction (les deux collations utf8mb4 usuelles se replient sur la collation PG par defaut de toute facon)
--   pas de FK reelle (aucune cote MySQL, choix delibere du migrateur original vu l'incertitude de type ci-dessus) : discount_id -> discounts.discount_id (candidate), order_id -> orders.order_id (candidate, type incoherent), customer_id -> customer.customer_id (candidate, type incoherent), merchant_id -> liste standard ci-dessous
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE discount_redemptions (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL CHECK (id >= 0),
    discount_id varchar(64) NOT NULL,
    order_id bigint NOT NULL CHECK (order_id >= 0),
    merchant_id varchar(64) NOT NULL,
    customer_id varchar(64),
    amount_applied_cents integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_discount_redemptions_uq_discount_order ON discount_redemptions (discount_id, order_id);
CREATE INDEX idx_discount_redemptions_idx_discount_redemptions_discount ON discount_redemptions (discount_id);
CREATE INDEX idx_discount_redemptions_idx_discount_redemptions_customer ON discount_redemptions (discount_id, customer_id);

-- ---------------------------------------------------------------------
-- discounts
--   valid_from: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   discount_scope/max_redemptions/max_redemptions_per_customer : colonnes ajoutees par migrations/done/041_cart_discounts.up.sql, posterieures au dump du 2026-07-13 audite (meme situation que planning_day_comments/haccp_traceability, rapports 26/56) ; absentes de wello-resto-mysql-ddl.md, ajoutees ici par le rapport 57 ; discount_scope ENUM -> discounts_discount_scope_enum
-- ---------------------------------------------------------------------
CREATE TABLE discounts (
    discount_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    discount_name varchar(50) NOT NULL,
    discount_desc varchar(100) NOT NULL,
    prefered_order integer NOT NULL DEFAULT 0,
    discount_code varchar(20),
    discount_scope discounts_discount_scope_enum NOT NULL DEFAULT 'PRODUCT',
    discount_order_type varchar(40),
    discount_value integer NOT NULL DEFAULT 0,
    discount_unit varchar(20) NOT NULL,
    valid_from timestamptz NOT NULL DEFAULT now(),
    valid_to timestamptz,
    min_order_value double precision NOT NULL DEFAULT 0,
    min_order_unit varchar(20),
    max_discount_value double precision,
    max_discount_unit varchar(20),
    discounted_quantity integer NOT NULL,
    is_cumulative boolean NOT NULL,
    is_time_limited boolean NOT NULL,
    available boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    max_redemptions integer,
    max_redemptions_per_customer integer,
    PRIMARY KEY (discount_id)
);
COMMENT ON COLUMN discounts.discount_order_type IS '0 = IN, 1 = DELIVERY, NULL = all';
COMMENT ON COLUMN discounts.discount_unit IS 'PERCENTAGE | CURRENCY | NEWPRICE';
COMMENT ON COLUMN discounts.valid_from IS 'UTC';
COMMENT ON COLUMN discounts.valid_to IS 'UTC';
COMMENT ON COLUMN discounts.min_order_unit IS 'CURRENCY | QUANTITY';
COMMENT ON COLUMN discounts.max_discount_unit IS 'CURRENCY | QUANTITY';
COMMENT ON COLUMN discounts.is_time_limited IS 'Requires discount_shedules ?';
COMMENT ON COLUMN discounts.enabled IS '0 when deleted';

-- ---------------------------------------------------------------------
-- discounts_products
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : discount_id -> discounts.discount_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE discounts_products (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    discount_id varchar(50) NOT NULL,
    product_id integer NOT NULL,
    new_price integer,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN discounts_products.enabled IS '0 when deleted';

-- ---------------------------------------------------------------------
-- discounts_products_options
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : discount_id -> discounts.discount_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE discounts_products_options (
    discount_id varchar(20) NOT NULL,
    product_id varchar(20) NOT NULL,
    option_id varchar(20) NOT NULL,
    new_price integer,
    is_option_mandatory boolean NOT NULL DEFAULT true,
    PRIMARY KEY (discount_id, product_id, option_id)
);

-- ---------------------------------------------------------------------
-- discounts_schedules
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : schedule_id -> availabilities_schedules.schedule_id
--   FK candidate (non creee) : discount_id -> discounts.discount_id
-- ---------------------------------------------------------------------
CREATE TABLE discounts_schedules (
    schedule_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    discount_id varchar(50) NOT NULL,
    day_of_week integer NOT NULL,
    available_from time NOT NULL,
    available_to time NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (schedule_id)
);
COMMENT ON COLUMN discounts_schedules.day_of_week IS 'lundi = 1';
COMMENT ON COLUMN discounts_schedules.available_from IS 'UTC time';
COMMENT ON COLUMN discounts_schedules.available_to IS 'UTC time';
COMMENT ON COLUMN discounts_schedules.enabled IS '0 when deleted';

-- ---------------------------------------------------------------------
-- employees
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE employees (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    user_id varchar(64),
    member_id bigint,
    first_name varchar(150) NOT NULL,
    last_name varchar(150) NOT NULL,
    position_id varchar(64) NOT NULL,
    position_note text,
    job_title varchar(150),
    email varchar(255),
    phone varchar(64),
    role employees_role_enum NOT NULL DEFAULT 'employee',
    contract_type_code varchar(32) NOT NULL,
    contract_start_date date,
    contract_end_date date,
    probation_end_date date,
    last_medical_checkup_date date,
    contract_hours numeric(5,2) NOT NULL DEFAULT 35.00,
    max_weekly_hours numeric(5,2) NOT NULL DEFAULT 35.00,
    required_rest_days integer NOT NULL DEFAULT 2,
    sunday_premium boolean NOT NULL DEFAULT false,
    night_premium boolean NOT NULL DEFAULT false,
    hourly_rate bigint NOT NULL DEFAULT 0,
    gross_monthly_salary bigint NOT NULL DEFAULT 0,
    employer_charges_pct numeric(5,2) NOT NULL DEFAULT 45.00,
    transport_cost bigint NOT NULL DEFAULT 0,
    birth_date date,
    gender varchar(32),
    nationality varchar(80),
    address varchar(255),
    hr_comment text,
    active boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_employees_uq_employees_merchant_user ON employees (merchant_id, user_id);
CREATE UNIQUE INDEX uq_employees_uq_employees_merchant_member ON employees (merchant_id, member_id);
CREATE INDEX idx_employees_idx_employees_merchant_active ON employees (merchant_id, active);
CREATE INDEX idx_employees_idx_employees_merchant ON employees (merchant_id);
CREATE INDEX idx_employees_idx_employees_contract_type ON employees (contract_type_code);
CREATE INDEX idx_employees_idx_employees_position_id ON employees (position_id);
CREATE INDEX idx_employees_idx_employees_member_id ON employees (member_id);

-- ---------------------------------------------------------------------
-- employee_documents
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE employee_documents (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    employee_id varchar(64) NOT NULL,
    document_type varchar(32) NOT NULL,
    name varchar(255) NOT NULL,
    file_key varchar(512) NOT NULL,
    content_type varchar(120) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN employee_documents.document_type IS 'contract id medical other';
CREATE INDEX idx_employee_documents_idx_empdocs_merchant ON employee_documents (merchant_id);
CREATE INDEX idx_employee_documents_idx_empdocs_employee ON employee_documents (employee_id);

-- ---------------------------------------------------------------------
-- employment_agreement
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE employment_agreement (
    merchant_id varchar(64) NOT NULL,
    weekly_limit integer NOT NULL DEFAULT 2100,
    monthly_limit integer NOT NULL DEFAULT 9100,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN employment_agreement.weekly_limit IS 'in minuts';
COMMENT ON COLUMN employment_agreement.monthly_limit IS 'in minuts';

-- ---------------------------------------------------------------------
-- employment_contract
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE employment_contract (
    contract_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    user_id varchar(64) NOT NULL,
    hourly_rate real NOT NULL,
    schedule integer NOT NULL,
    creation_date integer NOT NULL,
    PRIMARY KEY (contract_id)
);
COMMENT ON COLUMN employment_contract.schedule IS 'nombre d''heures à travailler, par mois par défaut mais le système peut être accepté pour accepter par mois et par semaine plus tard';

-- ---------------------------------------------------------------------
-- expiration_dates
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : component_id -> components.component_id
-- ---------------------------------------------------------------------
CREATE TABLE expiration_dates (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    component_id integer NOT NULL,
    comment varchar(150),
    purchased_component_id integer,
    expiration_date date NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN expiration_dates.enabled IS '0 when deleted';

-- ---------------------------------------------------------------------
-- external_tokens
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE external_tokens (
    token_type varchar(30) NOT NULL,
    access_token text NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (token_type)
);

-- ---------------------------------------------------------------------
-- extra
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : component_id -> components.component_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE extra (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    order_item_id integer,
    order_id integer NOT NULL,
    component_id integer NOT NULL,
    product_id integer NOT NULL,
    quantity integer NOT NULL DEFAULT 1,
    price integer NOT NULL,
    merchant_id varchar(64) NOT NULL,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN extra.price IS 'in cents';

-- ---------------------------------------------------------------------
-- firebase_fcm_access_token
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE firebase_fcm_access_token (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    access_token text NOT NULL,
    expiration_date timestamptz NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- floors
--   creation_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE floors (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(50) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- floor_areas
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE floor_areas (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    floor_id integer NOT NULL,
    name varchar(50) NOT NULL,
    stroke_color varchar(11) NOT NULL,
    color varchar(11) NOT NULL,
    x real NOT NULL,
    y real NOT NULL,
    points text NOT NULL,
    angle real NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- floor_obstacles
--   collation table utf8mb4_uca1400_ai_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE floor_obstacles (
    id varchar(64) NOT NULL,
    floor_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    type floor_obstacles_type_enum NOT NULL,
    x real NOT NULL DEFAULT 0,
    y real NOT NULL DEFAULT 0,
    width real NOT NULL DEFAULT 60,
    height real NOT NULL DEFAULT 20,
    angle real NOT NULL DEFAULT 0,
    direction real,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN floor_obstacles.direction IS 'Portes uniquement : angle d ouverture de l arc (degrés)';

-- ---------------------------------------------------------------------
-- goods_receipts
--   non_conformities: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE goods_receipts (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    supplier varchar(255) NOT NULL,
    product_type varchar(50) NOT NULL,
    batch_number varchar(120) NOT NULL,
    product_temp numeric(5,2) NOT NULL,
    control_sample varchar(255),
    quantities_verified boolean NOT NULL DEFAULT false,
    non_conformities jsonb,
    comment text,
    invoice_url varchar(512),
    created_by varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- haccp_corrective_actions
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE haccp_corrective_actions (
    id varchar(64) NOT NULL,
    code varchar(64) NOT NULL,
    label varchar(120) NOT NULL,
    description text,
    severity_scope varchar(32),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_haccp_corrective_actions_uq_haccp_corrective_actions_code ON haccp_corrective_actions (code);
CREATE INDEX idx_haccp_corrective_actions_idx_haccp_corrective_actions_activ ON haccp_corrective_actions (active);

-- ---------------------------------------------------------------------
-- haccp_settings
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE haccp_settings (
    merchant_id varchar(64) NOT NULL,
    temp_entry_required boolean NOT NULL DEFAULT false,
    temp_corrective_actions boolean NOT NULL DEFAULT false,
    temp_block_past_dates boolean NOT NULL DEFAULT false,
    temp_failure_photo_required boolean NOT NULL DEFAULT false,
    traceability_product_name boolean NOT NULL DEFAULT false,
    traceability_block_past_dates boolean NOT NULL DEFAULT false,
    cleaning_photo boolean NOT NULL DEFAULT false,
    cleaning_block_past_dates boolean NOT NULL DEFAULT false,
    reception_other_products boolean NOT NULL DEFAULT false,
    reception_control_sample boolean NOT NULL DEFAULT false,
    reception_block_past_dates boolean NOT NULL DEFAULT false,
    reception_photo boolean NOT NULL DEFAULT false,
    reception_non_conformities boolean NOT NULL DEFAULT false,
    oils_block_past_dates boolean NOT NULL DEFAULT false,
    oils_polar_compound_rate boolean NOT NULL DEFAULT false,
    oils_photo boolean NOT NULL DEFAULT false,
    production_block_past_dates boolean NOT NULL DEFAULT false,
    production_traceability boolean NOT NULL DEFAULT false,
    cooling_block_past_dates boolean NOT NULL DEFAULT false,
    freezing_block_past_dates boolean NOT NULL DEFAULT false,
    reheating_block_past_dates boolean NOT NULL DEFAULT false,
    holding_block_past_dates boolean NOT NULL DEFAULT false,
    holding_corrective_actions boolean NOT NULL DEFAULT false,
    notif_authorization boolean NOT NULL DEFAULT false,
    notif_security boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merchant_id)
);
CREATE UNIQUE INDEX uq_haccp_settings_uq_haccpsettings_merchant ON haccp_settings (merchant_id);

-- ---------------------------------------------------------------------
-- haccp_traceability_records
--   nouvelle table (migrations/done/067_haccp_traceability.up.sql, deja executee en MySQL ; non presente dans le dump wello-resto-mysql-ddl.md audite, meme situation que planning_day_comments/rapport 26) ; module internal/modules/haccp (CreateTraceabilityRecord/ListTraceabilityRecords/GetTraceabilityRecord/HasTraceabilityRecords)
--   id: helpers.GeneratePrefixedID(helpers.HACCPTraceabilityRecordIDPrefix) = "haccp-trace-<uuid>" = 48 caracteres -> varchar(64) deja suffisant cote MySQL source, pas d'elargissement necessaire (verifie rapport 55, confirme rapport 56)
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE haccp_traceability_records (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    comment text,
    created_by varchar(64) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_haccp_traceability_records_idx_haccp_traceability_records_m ON haccp_traceability_records (merchant_id);
CREATE INDEX idx_haccp_traceability_records_idx_haccp_traceability_records_2 ON haccp_traceability_records (merchant_id, created_at);

-- ---------------------------------------------------------------------
-- haccp_traceability_photos
--   nouvelle table (migrations/done/067_haccp_traceability.up.sql, deja executee en MySQL ; meme origine que haccp_traceability_records ci-dessus) ; module internal/modules/haccp
--   id: helpers.GeneratePrefixedID(helpers.HACCPTraceabilityPhotoIDPrefix) = "haccp-trace-photo-<uuid>" = 54 caracteres -> varchar(64) deja suffisant, pas d'elargissement necessaire (verifie rapport 55, confirme rapport 56)
--   position: tinyint MySQL (signe, non UNSIGNED) -> smallint, jamais converti en boolean (04-schema-mapping-notes.md)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK reelle conservee (deja presente cote MySQL, pas une candidate) : record_id -> haccp_traceability_records.id ON DELETE CASCADE ; index explicite ajoute sur record_id (MySQL/InnoDB indexe automatiquement les colonnes de FK referencantes, PG non - meme motif que product_ratings.order_rating_id)
--   placee apres haccp_traceability_records : deviation deliberee de l'ordre alphabetique strict du fichier (photos < records) car la FK ci-dessus exige que la table referencee existe deja au moment de la creation de la contrainte (fichier execute comme une seule transaction BEGIN;...COMMIT;)
-- ---------------------------------------------------------------------
CREATE TABLE haccp_traceability_photos (
    id varchar(64) NOT NULL,
    record_id varchar(64) NOT NULL,
    photo_key varchar(512) NOT NULL,
    position smallint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT fk_haccp_traceability_photos_record FOREIGN KEY (record_id) REFERENCES haccp_traceability_records (id) ON DELETE CASCADE
);
CREATE INDEX idx_haccp_traceability_photos_record_id ON haccp_traceability_photos (record_id);

-- ---------------------------------------------------------------------
-- holiday_calendar
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE holiday_calendar (
    id varchar(64) NOT NULL,
    country_code char(2) NOT NULL,
    holiday_date date NOT NULL,
    label varchar(150) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_holiday_calendar_uq_holiday_calendar_country_date ON holiday_calendar (country_code, holiday_date);
CREATE INDEX idx_holiday_calendar_idx_holiday_calendar_country ON holiday_calendar (country_code);

-- ---------------------------------------------------------------------
-- hours_amendments
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE hours_amendments (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    employee_id varchar(64) NOT NULL,
    type hours_amendments_type_enum NOT NULL DEFAULT 'permanent',
    start_date date NOT NULL,
    end_date date,
    new_hours_volume numeric(5,2) NOT NULL,
    created_by varchar(255),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_hours_amendments_idx_hours_amendments_merchant ON hours_amendments (merchant_id);
CREATE INDEX idx_hours_amendments_idx_hours_amendments_employee ON hours_amendments (employee_id);

-- ---------------------------------------------------------------------
-- hours_of_operation
--   valid_from: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE hours_of_operation (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    day_of_week_from integer NOT NULL,
    hour_from time NOT NULL,
    day_of_week_to integer NOT NULL,
    hour_to time NOT NULL,
    first_booking_time time,
    last_booking_time time,
    booking_capacity integer NOT NULL DEFAULT 0,
    valid_from timestamptz,
    valid_to timestamptz,
    creation_date timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN hours_of_operation.day_of_week_from IS '1 => Monday, 7 => Sunday';

-- ---------------------------------------------------------------------
-- integration_deliveroo
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : location_id -> locations.location_id
--   FK candidate (non creee) : brand_id -> brands.brand_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_deliveroo (
    merchant_id varchar(64) NOT NULL,
    location_id varchar(20) NOT NULL,
    brand_id varchar(150) NOT NULL,
    auto_accept_orders boolean NOT NULL DEFAULT false,
    commission_rate integer NOT NULL DEFAULT 0,
    preparation_time_minutes integer NOT NULL DEFAULT 60,
    last_sync timestamptz,
    synced_items integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN integration_deliveroo.commission_rate IS 'Commission rate in percent';
COMMENT ON COLUMN integration_deliveroo.last_sync IS 'Last successful menu upload timestamp (UTC)';
COMMENT ON COLUMN integration_deliveroo.synced_items IS 'Number of products in last published menu';

-- ---------------------------------------------------------------------
-- integration_deliveroo_attributes_mapping
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_deliveroo_attributes_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    configurable_attribute_id integer NOT NULL,
    modifier_group_pos_id varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_integration_deliveroo_attributes_mapping_unique_mapping ON integration_deliveroo_attributes_mapping (merchant_id, modifier_group_pos_id);

-- ---------------------------------------------------------------------
-- integration_deliveroo_components_mapping
--   !! PAS DE PRIMARY KEY dans le DDL source (deja le cas en MySQL) — a traiter avant migration logique/replication
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : item_id -> shift_templates_items.item_id
--   FK candidate (non creee) : component_id -> components.component_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_deliveroo_components_mapping (
    id integer NOT NULL,
    item_id varchar(50) NOT NULL,
    component_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true
);

-- ---------------------------------------------------------------------
-- integration_deliveroo_options_mapping
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : item_id -> shift_templates_items.item_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_deliveroo_options_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    configurable_attribute_option_id integer NOT NULL,
    item_id varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_integration_deliveroo_options_mapping_unique_mapping ON integration_deliveroo_options_mapping (merchant_id, item_id);

-- ---------------------------------------------------------------------
-- integration_deliveroo_products_mapping
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : item_id -> shift_templates_items.item_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_deliveroo_products_mapping (
    item_id varchar(50) NOT NULL,
    item_name varchar(50) NOT NULL,
    product_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (item_id, product_id, merchant_id)
);

-- ---------------------------------------------------------------------
-- integration_uber_direct
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : customer_id -> customer.customer_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_direct (
    merchant_id varchar(64) NOT NULL,
    bearer_token text,
    customer_id text NOT NULL,
    client_id text NOT NULL,
    client_secret text NOT NULL,
    external_store_id varchar(50),
    PRIMARY KEY (merchant_id)
);

-- ---------------------------------------------------------------------
-- integration_uber_eats
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_eats (
    merchant_id varchar(64) NOT NULL,
    store_id varchar(150) NOT NULL,
    pos_provisioning_token text,
    pos_provisionning_refresh_token text NOT NULL,
    pos_provisionning_token_expiration_date timestamptz NOT NULL,
    estimated_preparation_time varchar(10) NOT NULL DEFAULT '30',
    last_estimated_preparation_time varchar(10) NOT NULL DEFAULT '30',
    delay_until timestamptz,
    delay_duration integer NOT NULL,
    closed_until timestamptz,
    auto_accept_orders boolean NOT NULL,
    bearer_token text,
    refresh_token text,
    bearer_token_expiration_date timestamptz,
    expires_at timestamptz,
    enabled boolean NOT NULL DEFAULT false,
    unlink_date timestamptz,
    commission_rate integer NOT NULL DEFAULT 0,
    last_sync timestamptz,
    synced_items integer NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN integration_uber_eats.delay_until IS 'UTC';
COMMENT ON COLUMN integration_uber_eats.commission_rate IS 'Commission rate in percent';
COMMENT ON COLUMN integration_uber_eats.last_sync IS 'Last successful menu/product sync timestamp (UTC)';
COMMENT ON COLUMN integration_uber_eats.synced_items IS 'Number of products currently mapped to Uber Eats';

-- ---------------------------------------------------------------------
-- integration_uber_eats_attributes_mapping
--   collation table utf8mb4_general_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_eats_attributes_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    configurable_attribute_id integer NOT NULL,
    modifier_group_id varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_integration_uber_eats_attributes_mapping_unique_mapping ON integration_uber_eats_attributes_mapping (merchant_id, modifier_group_id);

-- ---------------------------------------------------------------------
-- integration_uber_eats_components_mapping
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : item_id -> shift_templates_items.item_id
--   FK candidate (non creee) : component_id -> components.component_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_eats_components_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    item_id varchar(50) NOT NULL,
    component_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- integration_uber_eats_options_mapping
--   collation table utf8mb4_general_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : item_id -> shift_templates_items.item_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_eats_options_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    configurable_attribute_option_id integer NOT NULL,
    item_id varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_integration_uber_eats_options_mapping_unique_mapping ON integration_uber_eats_options_mapping (merchant_id, item_id);

-- ---------------------------------------------------------------------
-- integration_uber_eats_products_mapping
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : item_id -> shift_templates_items.item_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_eats_products_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    item_id varchar(50) NOT NULL,
    product_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- integration_uber_eats_reports
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE integration_uber_eats_reports (
    workflow_id varchar(255) NOT NULL,
    report_type varchar(60) NOT NULL,
    store_id varchar(150) NOT NULL,
    start_date date NOT NULL,
    end_date date NOT NULL,
    download_url text,
    creation_date timestamptz NOT NULL,
    PRIMARY KEY (workflow_id)
);
COMMENT ON COLUMN integration_uber_eats_reports.workflow_id IS 'job_id in webhook';

-- ---------------------------------------------------------------------
-- invoices
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE invoices (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    invoice_id varchar(50),
    merchant_id varchar(64) NOT NULL,
    order_id integer NOT NULL,
    customer_email varchar(100) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- kiosks
--   admin_pin_encrypted: varbinary(255) -> bytea
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : location_id -> locations.location_id
-- ---------------------------------------------------------------------
CREATE TABLE kiosks (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(100) NOT NULL,
    location_id varchar(64),
    status kiosks_status_enum NOT NULL DEFAULT 'pending',
    app_version varchar(20),
    hardware_model varchar(100),
    admin_pin_encrypted bytea,
    os_version varchar(50),
    last_heartbeat_at timestamptz,
    last_ip varchar(45),
    last_error text,
    last_error_at timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- kiosk_device_tokens
--   !! PAS DE PRIMARY KEY dans le DDL source (deja le cas en MySQL) — a traiter avant migration logique/replication
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE kiosk_device_tokens (
    id varchar(64) NOT NULL,
    new_id varchar(64),
    kiosk_id varchar(64) NOT NULL,
    new_kiosk_id varchar(64),
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_kiosk_device_tokens_idx_device_token_hash ON kiosk_device_tokens (token_hash);

-- ---------------------------------------------------------------------
-- kiosk_enrollment_codes
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE kiosk_enrollment_codes (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    code_hash varchar(64) NOT NULL,
    kiosk_id varchar(64),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_by_user_id varchar(64),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- kiosk_settings
--   pager_number_required: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   pay_at_counter_enabled: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE kiosk_settings (
    merchant_id varchar(64) NOT NULL,
    fulfillment_dine_in boolean NOT NULL DEFAULT true,
    fulfillment_take_away boolean NOT NULL DEFAULT true,
    force_fulfillment_type varchar(20),
    pager_number_required boolean NOT NULL DEFAULT false,
    show_allergens boolean NOT NULL DEFAULT true,
    inactivity_timeout_sec integer NOT NULL DEFAULT 90,
    upsell_enabled boolean NOT NULL DEFAULT true,
    pay_at_counter_enabled boolean NOT NULL DEFAULT true,
    variable_fees numeric(10,4) NOT NULL DEFAULT 0.0070,
    fixed_fees integer NOT NULL DEFAULT 15,
    card_payment_enabled boolean NOT NULL DEFAULT false,
    logo_url varchar(500),
    idle_image_url varchar(500),
    idle_video_url varchar(500),
    primary_color varchar(7),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN kiosk_settings.variable_fees IS 'Frais variables plateforme (ex: 0.007 = 0.7%)';
COMMENT ON COLUMN kiosk_settings.fixed_fees IS 'Frais fixes plateforme en centimes (ex: 15 = 0.15€)';

-- ---------------------------------------------------------------------
-- labels
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE labels (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    label_value varchar(20) NOT NULL,
    label_type varchar(20) NOT NULL,
    lang varchar(5) NOT NULL,
    label varchar(150) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- labor_rules
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE labor_rules (
    country_code char(2) NOT NULL,
    label varchar(120) NOT NULL,
    min_daily_rest_hours numeric(4,2) NOT NULL DEFAULT 11.00,
    min_break_minutes integer NOT NULL DEFAULT 45,
    night_shift_start time NOT NULL DEFAULT '22:00:00',
    night_shift_end time NOT NULL DEFAULT '06:00:00',
    night_shift_multiplier numeric(4,2) NOT NULL DEFAULT 1.25,
    holiday_multiplier numeric(4,2) NOT NULL DEFAULT 2.00,
    max_weekly_hours numeric(5,2) NOT NULL DEFAULT 48.00,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (country_code)
);

-- ---------------------------------------------------------------------
-- locations
--   attributes: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE locations (
    location_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    location_name varchar(20) NOT NULL,
    location_desc varchar(20),
    location_order integer NOT NULL DEFAULT 0,
    seats integer NOT NULL,
    floor_id integer,
    shape varchar(20),
    x real,
    current_x real,
    y real,
    current_y real,
    width real,
    current_width real,
    height real,
    current_height real,
    angle real,
    enabled boolean NOT NULL DEFAULT true,
    attributes jsonb,
    PRIMARY KEY (location_id)
);
COMMENT ON COLUMN locations.location_name IS 'Nom de la table';
COMMENT ON COLUMN locations.location_desc IS 'Description (nb de eprsonnes, handicapés, debout..)';
COMMENT ON COLUMN locations.attributes IS 'Attributs booléens : pmr, terrace, vip, window';

-- ---------------------------------------------------------------------
-- marketing_categories
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE marketing_categories (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(191) NOT NULL,
    display_order integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    available boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_marketing_categories_uq_marketing_categories_merchant_name ON marketing_categories (merchant_id, name);
CREATE INDEX idx_marketing_categories_idx_marketing_categories_merchant_enab ON marketing_categories (merchant_id, enabled, display_order);

-- ---------------------------------------------------------------------
-- merchant
--   fullName: identifiant mixed-case 'fullName' : PG replie en 'fullname' (sans impact pour les requetes Go non quotees)
--   SIRET: identifiant mixed-case 'SIRET' : PG replie en 'siret' (sans impact pour les requetes Go non quotees)
--   merchantTel: identifiant mixed-case 'merchantTel' : PG replie en 'merchanttel' (sans impact pour les requetes Go non quotees)
--   FK candidate (non creee) : brand_id -> brands.brand_id
--   ATTENTION type : merchant.id reste INTEGER (identity, non concerne par l'unification 13-merchant-id-schema-update.md).
--   Toutes les colonnes merchant_id listees en commentaire "FK candidate" a travers ce fichier sont
--   desormais varchar(64) : aucune de ces FK candidates non creees n'est donc type-compatible avec
--   merchant.id sans cast explicite (merchant.id::varchar) si une vraie contrainte est ajoutee un jour.
-- ---------------------------------------------------------------------
CREATE TABLE merchant (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    brand_id varchar(35),
    fullName varchar(50) NOT NULL,
    address text NOT NULL,
    street_number varchar(25) NOT NULL,
    street varchar(255) NOT NULL,
    zip_code varchar(6) NOT NULL,
    city varchar(255) NOT NULL,
    country varchar(255) NOT NULL DEFAULT 'France',
    lat double precision DEFAULT 0,
    lng double precision DEFAULT 0,
    timezone varchar(50) NOT NULL DEFAULT 'Europe/Paris',
    logo text,
    logo_url text,
    handicap_access boolean NOT NULL DEFAULT false,
    SIRET varchar(50) NOT NULL,
    vat_number varchar(50),
    web_site varchar(100) NOT NULL,
    email varchar(100),
    merchantTel varchar(15) NOT NULL,
    token varchar(20) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    is_active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- merchant_code
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE merchant_code (
    code_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    code varchar(6) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (code_id)
);

-- ---------------------------------------------------------------------
-- merchant_google_maps_monthly
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE merchant_google_maps_monthly (
    merchant_id varchar(64) NOT NULL,
    month date NOT NULL,
    call_count integer NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, month)
);

-- ---------------------------------------------------------------------
-- merchant_marketing_settings
--   id: UNSIGNED perdu (bigint UNSIGNED -> bigint)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE merchant_marketing_settings (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL CHECK (id >= 0),
    merchant_id varchar(64) NOT NULL,
    sms_enabled boolean DEFAULT true,
    sms_unit_price integer NOT NULL DEFAULT 7,
    email_enabled boolean DEFAULT true,
    sms_sender_name varchar(20),
    email_sender_name varchar(100),
    sms_template text,
    email_template text,
    tracking_template text DEFAULT 'Votre commande #{order_id} est en cours de livraison. Suivez-la ici : {tracking_url}',
    messaggio_login varchar(255) NOT NULL DEFAULT 'd46j1e3un6tc738rv3tg',
    messaggio_from varchar(255) NOT NULL DEFAULT 'd46j39jun6tc738rv3vg',
    created_at timestamptz DEFAULT now(),
    updated_at timestamptz DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- merchant_parameters
--   pager_number_required: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   customer_form_requirements: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   enabled_rating: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE merchant_parameters (
    merchant_id varchar(64) NOT NULL,
    manage_on_site boolean NOT NULL DEFAULT true,
    manage_take_away boolean NOT NULL DEFAULT true,
    manage_delivery boolean NOT NULL DEFAULT true,
    last_menu_update timestamptz NOT NULL,
    concurrent_preparation_capacity integer NOT NULL DEFAULT 1,
    delivery_fees integer NOT NULL DEFAULT 0,
    delivery_fees_limit integer NOT NULL DEFAULT 0,
    delivery_distance_limit integer NOT NULL DEFAULT 5000,
    minimum_cart_for_delivery_order integer NOT NULL DEFAULT 1000,
    kitchen_show_only_paid boolean NOT NULL DEFAULT false,
    kitchen_show_pending_approval boolean NOT NULL DEFAULT false,
    kitchen_distribution_mode varchar(30) NOT NULL DEFAULT 'READY_FOR_DISTRIBUTION',
    production_display_mode varchar(20) NOT NULL DEFAULT 'CLASSIC',
    preparation_time_mode varchar(20) NOT NULL DEFAULT 'AUTO',
    preparation_time integer NOT NULL DEFAULT 15,
    minimum_preparation_time integer NOT NULL DEFAULT 300,
    maximum_preparation_time integer NOT NULL DEFAULT 3600,
    disable_components_under_safety_stock boolean NOT NULL DEFAULT false,
    service_required_for_ordering boolean NOT NULL DEFAULT false,
    cash_register_required_for_ordering boolean NOT NULL DEFAULT true,
    waiter_app_can_cash_in boolean NOT NULL DEFAULT true,
    waiter_app_can_clock_in boolean NOT NULL DEFAULT false,
    auto_complete_orders boolean NOT NULL DEFAULT false,
    auto_complete_orders_delay integer NOT NULL DEFAULT 10,
    auto_accept_sno_delivery_orders boolean NOT NULL DEFAULT false,
    auto_accept_sno_take_away_orders boolean NOT NULL DEFAULT false,
    automatically_add_customer_rewards boolean NOT NULL DEFAULT true,
    warning_new_order_not_paid boolean NOT NULL DEFAULT true,
    enable_advance_orders boolean NOT NULL DEFAULT false,
    advance_order_days integer NOT NULL DEFAULT 3,
    pager_number_required boolean NOT NULL DEFAULT false,
    pos_auto_lock_enabled boolean NOT NULL DEFAULT false,
    pos_auto_lock_delay_minutes integer NOT NULL DEFAULT 5,
    pos_upsell_enabled boolean NOT NULL DEFAULT false,
    customer_form_requirements jsonb,
    enabled_rating boolean NOT NULL DEFAULT false,
    currency varchar(5) NOT NULL DEFAULT 'EUR',
    is_open boolean NOT NULL DEFAULT false,
    primary_color varchar(10) NOT NULL DEFAULT '#212529',
    text_color_on_primary_color varchar(10) NOT NULL DEFAULT '#ffffff',
    zoning_type varchar(20),
    radial_cone_count integer NOT NULL DEFAULT 8,
    radial_zone_ranges varchar(20) NOT NULL DEFAULT '0-3,3-5,5-999',
    grid_cell_size_km integer NOT NULL DEFAULT 2,
    grid_origin_lat double precision,
    grid_origin_lng double precision,
    cardinal_cone_count integer NOT NULL DEFAULT 4,
    cardinal_zone_ranges varchar(30) NOT NULL DEFAULT '0-1,1-3,3-999',
    enable_upsell boolean NOT NULL DEFAULT false,
    upsell_max_items integer NOT NULL DEFAULT 3,
    enable_translation boolean NOT NULL DEFAULT false,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN merchant_parameters.concurrent_preparation_capacity IS 'Exemples par type d''établissement / Le goulot d''étranglement varie énormément d''un type de cuisine à l''autre. / / 1. Pizzeria ? / Goulot d''étranglement : La taille du four. / / Exemple : Un restaurant a un four à convoyeur qui peut cuire 4 pizzas côte à côte. Peu importe s''il y a 1 ou 3 cuisiniers, le débit est limité par le four. / / concurrent_preparation_capacity = 4 / / 2. Sandwicherie / Tacos / Kebab ? / Goulot d''étranglement : Le nombre d''employés au poste d''assemblage. / / Exemple : Deux employés préparent les sandwichs en parallèle. Un troisième employé à la caisse ne compte pas dans la capacité de production. / / concurrent_preparation_capacity = 2 / / 3. Restaurant traditionnel / Grill ? / Goulot d''étranglement : La surface de cuisson principale (plancha, grill). / / Exemple : Une plancha peut accueillir 6 steaks et 4 accompagnements en même temps. On peut considérer que chaque plat principal ou accompagnement est un "article". / / concurrent_preparation_capacity = 10 (environ) / / 4. Bar / Café ☕ / Goulot d''étran';
COMMENT ON COLUMN merchant_parameters.kitchen_distribution_mode IS 'READY_FOR_DISTRIBUTION / DISTRIBUTE';
COMMENT ON COLUMN merchant_parameters.production_display_mode IS 'CLASSIC, PRODUCT_FOCUS';
COMMENT ON COLUMN merchant_parameters.preparation_time_mode IS 'AUTO | MANUAL';
COMMENT ON COLUMN merchant_parameters.preparation_time IS 'for MANUAL, in minuts';
COMMENT ON COLUMN merchant_parameters.minimum_preparation_time IS 'in seconds';
COMMENT ON COLUMN merchant_parameters.maximum_preparation_time IS 'in seconds';
COMMENT ON COLUMN merchant_parameters.warning_new_order_not_paid IS 'popup that warn that the new order is not paid when creating new order on WR Reception';
COMMENT ON COLUMN merchant_parameters.pager_number_required IS 'Demande un numéro de bipeur';

-- ---------------------------------------------------------------------
-- merchant_sms_monthly
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE merchant_sms_monthly (
    merchant_id varchar(64) NOT NULL,
    month date NOT NULL,
    sms_count integer NOT NULL DEFAULT 0,
    total_cost integer NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, month)
);

-- ---------------------------------------------------------------------
-- merchant_translation_languages
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE merchant_translation_languages (
    merchant_id varchar(64) NOT NULL,
    lang_code varchar(5) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merchant_id, lang_code),
    CONSTRAINT merchant_translation_languages_ibfk_1 FOREIGN KEY (lang_code) REFERENCES available_languages (code)
);
CREATE INDEX idx_merchant_translation_languages_lang_code ON merchant_translation_languages (lang_code);

-- ---------------------------------------------------------------------
-- migration_users
--   totalPoint: identifiant mixed-case 'totalPoint' : PG replie en 'totalpoint' (sans impact pour les requetes Go non quotees)
--   totalGift: identifiant mixed-case 'totalGift' : PG replie en 'totalgift' (sans impact pour les requetes Go non quotees)
--   userIdNotif: identifiant mixed-case 'userIdNotif' : PG replie en 'useridnotif' (sans impact pour les requetes Go non quotees)
--   history__createdAt: identifiant mixed-case 'history__createdAt' : PG replie en 'history__createdat' (sans impact pour les requetes Go non quotees)
--   history__updatedAt: identifiant mixed-case 'history__updatedAt' : PG replie en 'history__updatedat' (sans impact pour les requetes Go non quotees)
--   createdAt: identifiant mixed-case 'createdAt' : PG replie en 'createdat' (sans impact pour les requetes Go non quotees)
--   updatedAt: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   updatedAt: identifiant mixed-case 'updatedAt' : PG replie en 'updatedat' (sans impact pour les requetes Go non quotees)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE migration_users (
    _id varchar(50) NOT NULL,
    totalPoint integer DEFAULT 0,
    totalGift integer DEFAULT 0,
    archived boolean DEFAULT false,
    email varchar(100) NOT NULL,
    password varchar(255),
    phone varchar(20),
    first_name varchar(100),
    last_name varchar(100),
    role varchar(50),
    userIdNotif varchar(255),
    code varchar(100),
    history___id varchar(24),
    history__title varchar(255),
    history__createdAt timestamptz,
    history__updatedAt timestamptz,
    createdAt timestamptz DEFAULT now(),
    updatedAt timestamptz DEFAULT now(),
    __v integer DEFAULT 0,
    PRIMARY KEY (_id)
);

-- ---------------------------------------------------------------------
-- notifications
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE notifications (
    notification_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    notification_user_id varchar(64) NOT NULL,
    notification_title varchar(60) NOT NULL,
    notification_desc varchar(150) NOT NULL,
    done boolean NOT NULL DEFAULT false,
    notification_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (notification_id)
);
COMMENT ON COLUMN notifications.notification_date IS 'UTC';

-- ---------------------------------------------------------------------
-- orderitems
--   isPaid: identifiant mixed-case 'isPaid' : PG replie en 'ispaid' (sans impact pour les requetes Go non quotees)
--   isDistributed: identifiant mixed-case 'isDistributed' : PG replie en 'isdistributed' (sans impact pour les requetes Go non quotees)
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : discount_id -> discounts.discount_id
-- ---------------------------------------------------------------------
CREATE TABLE orderitems (
    order_item_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    brand_order_item_id varchar(100),
    order_id integer NOT NULL,
    product_id integer NOT NULL,
    merchant_id varchar(64) NOT NULL,
    quantity integer NOT NULL,
    paid_quantity integer NOT NULL DEFAULT 0,
    distributed_quantity integer NOT NULL DEFAULT 0,
    ready_for_distribution_quantity integer NOT NULL DEFAULT 0,
    discount_id integer,
    price integer NOT NULL,
    base_price integer,
    isPaid boolean NOT NULL DEFAULT false,
    isDistributed boolean NOT NULL DEFAULT false,
    production_status varchar(20) NOT NULL DEFAULT 'TODO',
    production_status_done_quantity integer NOT NULL DEFAULT 0,
    delay_id integer DEFAULT 0,
    is_upsell boolean NOT NULL DEFAULT false,
    distributed_on timestamptz,
    ordered_on timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (order_item_id, order_id, product_id)
);
COMMENT ON COLUMN orderitems.isPaid IS 'Paid article';
COMMENT ON COLUMN orderitems.isDistributed IS 'Distributed article';
CREATE INDEX idx_orderitems_idx_orderitems_product_id ON orderitems (product_id);

-- ---------------------------------------------------------------------
-- orders
--   dateCall: identifiant mixed-case 'dateCall' : PG replie en 'datecall' (sans impact pour les requetes Go non quotees)
--   TVA: identifiant mixed-case 'TVA' : PG replie en 'tva' (sans impact pour les requetes Go non quotees)
--   HT: identifiant mixed-case 'HT' : PG replie en 'ht' (sans impact pour les requetes Go non quotees)
--   isDelivery: colonne retiree (voir 35-dead-columns-removal.md) - logique morte cote Go, non consommee
--   par wello_resto_flutter/wello-kiosk/ScanNOrder/wello-back-office ; reste en base MySQL source telle quelle
--   isPaid: identifiant mixed-case 'isPaid' : PG replie en 'ispaid' (sans impact pour les requetes Go non quotees)
--   isDistributed: identifiant mixed-case 'isDistributed' : PG replie en 'isdistributed' (sans impact pour les requetes Go non quotees)
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : customer_id -> customer.customer_id
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
--   FK candidate (non creee) : deletion_reason_id -> deletion_reasons.deletion_reason_id
--   cart_discount_id/cart_discount_code/cart_discount_amount : colonnes ajoutees par migrations/done/041_cart_discounts.up.sql, posterieures au dump du 2026-07-13 audite (meme situation que discount_scope sur discounts ci-dessus, rapport 57) ; aucun module Go ne les lit/ecrit a ce jour
--   FK candidate (non creee) : cart_discount_id -> discounts.discount_id
-- ---------------------------------------------------------------------
CREATE TABLE orders (
    order_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    public_id varchar(64),
    merchant_id varchar(64) NOT NULL,
    customer_id integer,
    use_customer_temporary_address boolean DEFAULT false,
    cash_register_id varchar(11),
    kiosk_id varchar(64),
    order_num integer NOT NULL,
    brand varchar(20) NOT NULL DEFAULT 'WELLO_RESTO',
    brand_order_id varchar(50),
    parent_order_id varchar(50),
    brand_order_num varchar(10),
    brand_status varchar(30) NOT NULL,
    scheduled boolean NOT NULL DEFAULT false,
    order_type varchar(30),
    state varchar(30) NOT NULL DEFAULT 'OPEN',
    location text,
    places_settings integer NOT NULL DEFAULT 0,
    pager_number varchar(5),
    price integer NOT NULL,
    dateCall timestamptz NOT NULL DEFAULT now(),
    delivery_start timestamptz,
    delivered_on timestamptz,
    TVA integer NOT NULL,
    HT integer NOT NULL,
    cart_discount_id varchar(64),
    cart_discount_code varchar(64),
    cart_discount_amount integer NOT NULL DEFAULT 0,
    delivery_fees integer NOT NULL DEFAULT 0,
    comment text,
    cutlery_notes boolean DEFAULT false,
    merchant_approval varchar(30) NOT NULL DEFAULT 'ACCEPTED',
    status integer DEFAULT 2,
    creation_date timestamptz NOT NULL DEFAULT now(),
    created_by varchar(40) NOT NULL,
    means_of_payement text,
    monnaie real NOT NULL DEFAULT 0,
    isPaid boolean NOT NULL DEFAULT false,
    isDistributed boolean NOT NULL DEFAULT false,
    responsible integer,
    fulfillment_type varchar(40) NOT NULL DEFAULT 'DELIVERY_BY_RESTAURANT',
    estimated_ready timestamptz,
    deletion_reason_id varchar(11),
    deletion_comment varchar(255),
    last_update timestamptz NOT NULL DEFAULT now(),
    hash varchar(64),
    signature text,
    previous_hash varchar(64),
    PRIMARY KEY (order_id)
);
COMMENT ON COLUMN orders.order_num IS 'Numéro de la commande affiché au client et au marchand';
COMMENT ON COLUMN orders.parent_order_id IS 'Deliveroo : Previous brand_order_id before remake';
COMMENT ON COLUMN orders.delivery_fees IS 'Cents';
COMMENT ON COLUMN orders.merchant_approval IS 'validation des commandes en livraison par SNO';
COMMENT ON COLUMN orders.status IS '-1 deleted / 0 finished / 1 done and paid / 2 pending / 3 stripe payment pending';
COMMENT ON COLUMN orders.created_by IS 'User who created the order';
COMMENT ON COLUMN orders.isPaid IS 'Order is paied';
COMMENT ON COLUMN orders.isDistributed IS 'All plates are distributed';
CREATE INDEX idx_orders_idx_orders_state ON orders (state);
CREATE INDEX idx_orders_idx_orders_brand_status ON orders (brand_status);
CREATE INDEX idx_orders_idx_orders_merchant_id ON orders (merchant_id);

-- ---------------------------------------------------------------------
-- order_changes_log
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE order_changes_log (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    order_id varchar(25) NOT NULL,
    changed_by_user_id varchar(25) NOT NULL,
    change_type varchar(50) NOT NULL,
    change_date timestamptz NOT NULL,
    field_changed varchar(50) NOT NULL,
    old_value varchar(50),
    new_value varchar(50) NOT NULL,
    change_reason varchar(255),
    origin varchar(255),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- order_comments
--   creation_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE order_comments (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    order_id integer NOT NULL,
    order_item_id integer,
    user_id varchar(20),
    content text NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- order_item_configuration
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE order_item_configuration (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    order_item_id integer NOT NULL,
    configuration_attribute_id integer NOT NULL,
    configuration_attribute_option_id integer NOT NULL,
    quantity integer NOT NULL DEFAULT 1,
    PRIMARY KEY (id)
);
CREATE INDEX idx_order_item_configuration_idx_order_item_configuration_order ON order_item_configuration (order_item_id);
CREATE INDEX idx_order_item_configuration_idx_order_item_configuration_confi ON order_item_configuration (configuration_attribute_option_id);

-- ---------------------------------------------------------------------
-- order_location
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : location_id -> locations.location_id
-- ---------------------------------------------------------------------
CREATE TABLE order_location (
    order_id integer NOT NULL,
    location_id integer NOT NULL,
    PRIMARY KEY (order_id, location_id)
);

-- ---------------------------------------------------------------------
-- order_ratings
--   id: UNSIGNED perdu (bigint UNSIGNED -> bigint)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE order_ratings (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL CHECK (id >= 0),
    order_id varchar(255) NOT NULL,
    delivery_rating smallint NOT NULL CHECK (delivery_rating >= 0),
    comment text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN order_ratings.delivery_rating IS 'Note de 1 à 5 pour la livraison';
COMMENT ON COLUMN order_ratings.comment IS 'Commentaire textuel de l''utilisateur';
CREATE UNIQUE INDEX uq_order_ratings_uniq_order_id ON order_ratings (order_id);

-- ---------------------------------------------------------------------
-- packages
--   allow_waiter_account: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   allow_delivery_account: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE packages (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    package_name varchar(50) NOT NULL,
    stripe_price_id varchar(200) NOT NULL,
    trial_period_days integer NOT NULL DEFAULT 0,
    allow_waiter_account boolean NOT NULL DEFAULT false,
    allow_delivery_account boolean NOT NULL DEFAULT false,
    scannorder_ready boolean NOT NULL DEFAULT true,
    stock_management integer NOT NULL DEFAULT 0,
    hr_management boolean NOT NULL DEFAULT false,
    planning_enabled boolean NOT NULL DEFAULT false,
    haccp_enabled boolean NOT NULL DEFAULT false,
    stock_enabled boolean NOT NULL DEFAULT false,
    scannorder_enabled boolean NOT NULL DEFAULT true,
    bookings_enabled boolean NOT NULL DEFAULT false,
    kiosks_enabled boolean NOT NULL DEFAULT false,
    PRIMARY KEY (id)
);
COMMENT ON COLUMN packages.scannorder_ready IS 'Allow access SNO options in Quick Management (Reception App) and order via SNO';
COMMENT ON COLUMN packages.kiosks_enabled IS 'Added to MySQL source after the 07/13 DDL dump this migration was audited against — not present in wello-resto-mysql-ddl.md, confirmed by the user as a recent production addition (see 25-tier2-conversion-log.md).';

-- ---------------------------------------------------------------------
-- payments
--   payment_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE payments (
    payment_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    cash_register_id varchar(20),
    merchant_id varchar(64) NOT NULL,
    user_id varchar(64) NOT NULL,
    order_id integer NOT NULL,
    amount integer NOT NULL,
    mop varchar(20) NOT NULL,
    fee integer NOT NULL DEFAULT 0,
    net_amount integer NOT NULL DEFAULT 0,
    comment varchar(250),
    status_check varchar(2),
    hash varchar(64),
    signature text,
    previous_hash varchar(64),
    operation_type varchar(20) NOT NULL DEFAULT 'SALE',
    payment_date timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (payment_id)
);
COMMENT ON COLUMN payments.mop IS 'Means of payment | CURRENCY or PERCENTAGE for discounts';
COMMENT ON COLUMN payments.status_check IS 'check status for TR payment';
COMMENT ON COLUMN payments.enabled IS '1 enabled, 0 disabled';

-- ---------------------------------------------------------------------
-- pictures
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE pictures (
    picture_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    img text NOT NULL,
    PRIMARY KEY (picture_id)
);

-- ---------------------------------------------------------------------
-- planned_shifts
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE planned_shifts (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    created_by integer NOT NULL,
    planning_role_id integer NOT NULL,
    user_id varchar(64) NOT NULL,
    start_date timestamptz NOT NULL,
    end_date timestamptz NOT NULL,
    department_id integer,
    comment varchar(50),
    enabled integer NOT NULL DEFAULT 1,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- planning_day_comments
--   nouvelle table (migration 065, non presente dans le dump wello-resto-mysql-ddl.md audite) ; module internal/modules/planning/daycomments
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes) ; CONFIRME cependant, voir docs/migration-postgres/05-on-update-timestamp-audit.md #41 (Upsert du module set explicitement updated_at)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_day_comments (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    comment_date date NOT NULL,
    comment text NOT NULL,
    created_by varchar(64),
    updated_by varchar(64),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_planning_day_comments_uq_planning_day_comments_merchant_date ON planning_day_comments (merchant_id, comment_date);
CREATE INDEX idx_planning_day_comments_idx_planning_day_comments_merchant_ra ON planning_day_comments (merchant_id, comment_date);

-- ---------------------------------------------------------------------
-- planning_holiday_overrides
--   count_as_holiday: tinyint(1) signale par l'heuristique compteur, confirme BOOLEAN apres revue du code Go
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_holiday_overrides (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    holiday_date date NOT NULL,
    label varchar(150),
    is_open boolean,
    count_as_holiday boolean,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_planning_holiday_overrides_uq_planning_holiday_overrides_mer ON planning_holiday_overrides (merchant_id, holiday_date);
CREATE INDEX idx_planning_holiday_overrides_idx_planning_holiday_overrides_m ON planning_holiday_overrides (merchant_id);
CREATE INDEX idx_planning_holiday_overrides_idx_planning_holiday_overrides_d ON planning_holiday_overrides (holiday_date);

-- ---------------------------------------------------------------------
-- planning_leave_requests
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_leave_requests (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    employee_id varchar(64) NOT NULL,
    leave_type planning_leave_requests_leave_type_enum NOT NULL DEFAULT 'paid',
    start_date date NOT NULL,
    end_date date NOT NULL,
    status planning_leave_requests_status_enum NOT NULL DEFAULT 'pending',
    reason text,
    manager_note text,
    requested_by_user_id varchar(64),
    processed_by_user_id varchar(64),
    processed_at timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_leave_requests_idx_planning_leave_requests_merchan ON planning_leave_requests (merchant_id, employee_id);
CREATE INDEX idx_planning_leave_requests_idx_planning_leave_requests_status ON planning_leave_requests (status);
CREATE INDEX idx_planning_leave_requests_idx_planning_leave_requests_range ON planning_leave_requests (start_date, end_date);
CREATE INDEX idx_planning_leave_requests_idx_planning_leave_requests_request ON planning_leave_requests (requested_by_user_id);
CREATE INDEX idx_planning_leave_requests_idx_planning_leave_requests_process ON planning_leave_requests (processed_by_user_id);

-- ---------------------------------------------------------------------
-- planning_positions
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_positions (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    label varchar(150) NOT NULL,
    color char(7) NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_positions_idx_planning_positions_merchant ON planning_positions (merchant_id);
CREATE INDEX idx_planning_positions_idx_planning_positions_merchant_label ON planning_positions (merchant_id, label);
CREATE INDEX idx_planning_positions_idx_planning_positions_merchant_sort ON planning_positions (merchant_id, sort_order);

-- ---------------------------------------------------------------------
-- planning_revenue_forecasts
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_revenue_forecasts (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    forecast_date date NOT NULL,
    amount_ht_cents bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_planning_revenue_forecasts_uq_planning_revenue_forecasts_mer ON planning_revenue_forecasts (merchant_id, forecast_date);
CREATE INDEX idx_planning_revenue_forecasts_idx_planning_revenue_forecasts_m ON planning_revenue_forecasts (merchant_id);

-- ---------------------------------------------------------------------
-- planning_roles
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_roles (
    role_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    role_name varchar(50) NOT NULL,
    role_color varchar(11) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id)
);

-- ---------------------------------------------------------------------
-- planning_settings
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_settings (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    labor_country_code char(2) NOT NULL DEFAULT 'FR',
    min_daily_rest_hours numeric(4,2) NOT NULL DEFAULT 11.00,
    min_break_minutes integer NOT NULL DEFAULT 45,
    night_shift_start time NOT NULL DEFAULT '22:00:00',
    night_shift_end time NOT NULL DEFAULT '06:00:00',
    night_shift_multiplier numeric(4,2) NOT NULL DEFAULT 1.25,
    holiday_multiplier numeric(4,2) NOT NULL DEFAULT 2.00,
    allow_override_warnings boolean NOT NULL DEFAULT true,
    attendance_source varchar(32) NOT NULL DEFAULT 'pointage',
    shift_swap_approval_mode varchar(25) NOT NULL DEFAULT 'none',
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_planning_settings_uq_planning_settings_merchant ON planning_settings (merchant_id);
CREATE INDEX idx_planning_settings_idx_planning_settings_merchant ON planning_settings (merchant_id);
CREATE INDEX idx_planning_settings_idx_planning_settings_labor_country_code ON planning_settings (labor_country_code);

-- ---------------------------------------------------------------------
-- planning_shifts
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_shifts (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    week_id varchar(64) NOT NULL,
    employee_id varchar(64),
    position_id varchar(64),
    title varchar(150) NOT NULL,
    shift_date date NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    break_minutes integer NOT NULL DEFAULT 0,
    "position" varchar(150),
    location varchar(150),
    notes text,
    status planning_shifts_status_enum NOT NULL DEFAULT 'planned',
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_shifts_idx_planning_shifts_merchant ON planning_shifts (merchant_id);
CREATE INDEX idx_planning_shifts_idx_planning_shifts_week ON planning_shifts (week_id);
CREATE INDEX idx_planning_shifts_idx_planning_shifts_employee_date ON planning_shifts (employee_id, shift_date);
CREATE INDEX idx_planning_shifts_idx_planning_shifts_date ON planning_shifts (shift_date);
CREATE INDEX idx_planning_shifts_idx_planning_shifts_position_id ON planning_shifts (position_id);

-- ---------------------------------------------------------------------
-- planning_shift_swap_requests
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_shift_swap_requests (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    requester_employee_id varchar(64) NOT NULL,
    requester_shift_id varchar(64) NOT NULL,
    target_employee_id varchar(64) NOT NULL,
    target_shift_id varchar(64) NOT NULL,
    status planning_shift_swap_requests_status_enum NOT NULL DEFAULT 'pending',
    reason text,
    manager_note text,
    requested_by_user_id varchar(64),
    processed_by_user_id varchar(64),
    processed_at timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_reques ON planning_shift_swap_requests (merchant_id);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_2 ON planning_shift_swap_requests (status);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_3 ON planning_shift_swap_requests (requester_employee_id);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_4 ON planning_shift_swap_requests (target_employee_id);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_5 ON planning_shift_swap_requests (requester_shift_id);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_6 ON planning_shift_swap_requests (target_shift_id);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_7 ON planning_shift_swap_requests (requested_by_user_id);
CREATE INDEX idx_planning_shift_swap_requests_idx_planning_shift_swap_requ_8 ON planning_shift_swap_requests (processed_by_user_id);

-- ---------------------------------------------------------------------
-- planning_shift_templates
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_shift_templates (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    label varchar(150) NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    break_minutes integer NOT NULL DEFAULT 0,
    position_id varchar(64),
    color char(7) NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_shift_templates_idx_planning_shift_templates_merch ON planning_shift_templates (merchant_id);

-- ---------------------------------------------------------------------
-- planning_time_entries
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_time_entries (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    employee_id varchar(64) NOT NULL,
    shift_id varchar(64),
    attendance_source varchar(32) NOT NULL,
    clock_in_at timestamptz NOT NULL,
    clock_out_at timestamptz,
    clock_in_note text,
    clock_out_note text,
    modified_by varchar(255),
    modified_at timestamptz,
    modification_reason varchar(255),
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_time_entries_idx_planning_time_entries_merchant_em ON planning_time_entries (merchant_id, employee_id);
CREATE INDEX idx_planning_time_entries_idx_planning_time_entries_open ON planning_time_entries (employee_id, clock_out_at);
CREATE INDEX idx_planning_time_entries_idx_planning_time_entries_shift ON planning_time_entries (shift_id);
CREATE INDEX idx_planning_time_entries_idx_planning_time_entries_clock_in ON planning_time_entries (clock_in_at);
CREATE INDEX idx_planning_time_entries_idx_planning_time_entries_attendance_ ON planning_time_entries (attendance_source);

-- ---------------------------------------------------------------------
-- planning_weeks
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_weeks (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    label varchar(150),
    start_date date NOT NULL,
    end_date date NOT NULL,
    status planning_weeks_status_enum NOT NULL DEFAULT 'draft',
    published_at timestamptz,
    notes text,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_weeks_idx_planning_weeks_merchant_start ON planning_weeks (merchant_id, start_date, enabled);
CREATE INDEX idx_planning_weeks_idx_planning_weeks_merchant ON planning_weeks (merchant_id);
CREATE INDEX idx_planning_weeks_idx_planning_weeks_range ON planning_weeks (start_date, end_date);

-- ---------------------------------------------------------------------
-- planning_week_templates
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE planning_week_templates (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    label varchar(120) NOT NULL,
    notes text,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_week_templates_idx_planning_week_templates_merchan ON planning_week_templates (merchant_id);

-- ---------------------------------------------------------------------
-- planning_week_template_shifts
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE planning_week_template_shifts (
    id varchar(64) NOT NULL,
    week_template_id varchar(64) NOT NULL,
    day_of_week smallint NOT NULL,
    employee_id varchar(64),
    position_id varchar(64),
    title varchar(120),
    start_time time NOT NULL,
    end_time time NOT NULL,
    break_minutes integer NOT NULL DEFAULT 0,
    location varchar(120),
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_planning_week_template_shifts_idx_planning_week_template_sh ON planning_week_template_shifts (week_template_id);

-- ---------------------------------------------------------------------
-- printers
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE printers (
    printer_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(255) NOT NULL,
    connection_type varchar(20) NOT NULL,
    ip_address varchar(45),
    port integer NOT NULL DEFAULT 9100,
    bluetooth_address varchar(17),
    language varchar(10) NOT NULL,
    role varchar(30) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    production_product_ids text,
    created_at timestamptz DEFAULT now(),
    updated_at timestamptz DEFAULT now(),
    paper_width_mm integer NOT NULL DEFAULT 80,
    PRIMARY KEY (printer_id)
);

-- ---------------------------------------------------------------------
-- productcateg
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE productcateg (
    categ_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    merchant_categ_id varchar(20) NOT NULL,
    categ_name text NOT NULL,
    categ_order integer NOT NULL,
    bg_color varchar(9) NOT NULL DEFAULT '#ffffff',
    available boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (categ_id)
);

-- ---------------------------------------------------------------------
-- products
--   merchant_Id: identifiant mixed-case 'merchant_Id' : PG replie en 'merchant_id' (sans impact pour les requetes Go non quotees)
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE products (
    product_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    by_product_of integer,
    merchant_Id varchar(64) NOT NULL,
    name varchar(255) NOT NULL,
    product_desc text,
    img text,
    image_url text,
    bg_color varchar(11) NOT NULL DEFAULT '#ffffff',
    production_color varchar(11),
    display_order integer NOT NULL DEFAULT 0,
    price integer NOT NULL,
    price_take_away integer NOT NULL DEFAULT 0,
    price_delivery integer NOT NULL DEFAULT 0,
    price_uber_eats integer NOT NULL DEFAULT 0,
    price_deliveroo integer NOT NULL DEFAULT 0,
    available_in boolean NOT NULL DEFAULT true,
    available_take_away boolean NOT NULL DEFAULT true,
    available_delivery boolean NOT NULL DEFAULT true,
    tva_in_id integer NOT NULL DEFAULT 0,
    tva_delivery_id integer NOT NULL DEFAULT 0,
    tva_take_away_id integer NOT NULL DEFAULT 0,
    category varchar(30) NOT NULL,
    status varchar(20) NOT NULL DEFAULT '1',
    is_product_group boolean NOT NULL DEFAULT false,
    is_available_on_sno boolean NOT NULL DEFAULT true,
    is_available_on_kiosk boolean NOT NULL DEFAULT true,
    sync_deliveroo boolean NOT NULL DEFAULT true,
    sync_uber_eats boolean NOT NULL DEFAULT true,
    available boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    is_popular boolean DEFAULT false,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id, merchant_Id)
);
COMMENT ON COLUMN products.product_desc IS 'Short description of the product. Talink about components is not mandatory';
COMMENT ON COLUMN products.img IS 'picture base64';
COMMENT ON COLUMN products.tva_in_id IS 'TVA rate ID';
COMMENT ON COLUMN products.tva_delivery_id IS 'TVA rate ID';
COMMENT ON COLUMN products.tva_take_away_id IS 'TVA rate ID';
COMMENT ON COLUMN products.available IS 'Product is available on the menu';
COMMENT ON COLUMN products.enabled IS 'Product is not deleted definitely';

-- ---------------------------------------------------------------------
-- product_allergens
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : allergen_id -> allergens.allergen_id
-- ---------------------------------------------------------------------
CREATE TABLE product_allergens (
    product_id varchar(255) NOT NULL,
    allergen_id varchar(255) NOT NULL,
    PRIMARY KEY (product_id, allergen_id)
);

-- ---------------------------------------------------------------------
-- product_configurable_attribute
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE product_configurable_attribute (
    product_id varchar(64) NOT NULL,
    configurable_attribute_id varchar(64) NOT NULL,
    num_order integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (configurable_attribute_id, product_id)
);

-- ---------------------------------------------------------------------
-- product_marketing_categories
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE product_marketing_categories (
    product_id varchar(64) NOT NULL,
    marketing_category_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id)
);
CREATE INDEX idx_product_marketing_categories_idx_product_marketing_categori ON product_marketing_categories (merchant_id, marketing_category_id);

-- ---------------------------------------------------------------------
-- product_ratings
--   id: UNSIGNED perdu (bigint UNSIGNED -> bigint)
--   order_rating_id: UNSIGNED perdu (bigint UNSIGNED -> bigint)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE product_ratings (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL CHECK (id >= 0),
    order_rating_id bigint NOT NULL CHECK (order_rating_id >= 0),
    product_id varchar(255) NOT NULL,
    rating smallint NOT NULL CHECK (rating >= 0),
    PRIMARY KEY (id),
    CONSTRAINT fk_product_ratings_order_rating FOREIGN KEY (order_rating_id) REFERENCES order_ratings (id) ON DELETE CASCADE
);
COMMENT ON COLUMN product_ratings.order_rating_id IS 'Clé étrangère vers order_ratings';
COMMENT ON COLUMN product_ratings.product_id IS 'ID unique du produit';
COMMENT ON COLUMN product_ratings.rating IS 'Note de 1 à 5 pour le produit';
CREATE INDEX idx_product_ratings_idx_order_rating_id ON product_ratings (order_rating_id);

-- ---------------------------------------------------------------------
-- product_tags
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : tag_id -> tags.tag_id
-- ---------------------------------------------------------------------
CREATE TABLE product_tags (
    product_id varchar(255) NOT NULL,
    tag_id varchar(255) NOT NULL,
    PRIMARY KEY (product_id, tag_id)
);

-- ---------------------------------------------------------------------
-- purchased_components
--   registration_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : component_id -> components.component_id
-- ---------------------------------------------------------------------
CREATE TABLE purchased_components (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    component_id integer NOT NULL,
    barcode varchar(25) NOT NULL,
    price real NOT NULL,
    quantity integer NOT NULL,
    uom integer NOT NULL,
    remaining_quantity real NOT NULL DEFAULT 0,
    bought_quantity integer NOT NULL,
    registration_date timestamptz NOT NULL DEFAULT now(),
    empty_date timestamptz NOT NULL DEFAULT now(),
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- qrcodes
--   QR_id: identifiant mixed-case 'QR_id' : PG replie en 'qr_id' (sans impact pour les requetes Go non quotees)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : location_id -> locations.location_id
-- ---------------------------------------------------------------------
CREATE TABLE qrcodes (
    QR_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    description varchar(70),
    user_id varchar(64),
    location_id integer,
    menu_only boolean NOT NULL DEFAULT false,
    delivery smallint NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    mywelloresto_flag boolean NOT NULL DEFAULT false,
    code text NOT NULL,
    last_waiter_call timestamptz,
    creation_date timestamptz DEFAULT now(),
    deleted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (QR_id)
);
COMMENT ON COLUMN qrcodes.mywelloresto_flag IS 'MyWelloResto test page';
COMMENT ON COLUMN qrcodes.last_waiter_call IS 'SERVER DATE of last call to waiter';

-- ---------------------------------------------------------------------
-- receipts
--   tax_details: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   items_snapshot: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   payments_snapshot: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE receipts (
    receipt_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    order_id integer NOT NULL,
    receipt_number varchar(50) NOT NULL,
    total_ttc integer NOT NULL,
    total_ht integer NOT NULL,
    tax_details jsonb NOT NULL,
    items_snapshot jsonb NOT NULL,
    payments_snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    prev_hash varchar(64),
    hash varchar(64) NOT NULL,
    signature text NOT NULL,
    PRIMARY KEY (receipt_id)
);
COMMENT ON COLUMN receipts.receipt_id IS 'UUID technique';
COMMENT ON COLUMN receipts.receipt_number IS 'Numéro fiscal séquentiel ex: F-2026-00012';
COMMENT ON COLUMN receipts.total_ttc IS 'En cents';
COMMENT ON COLUMN receipts.total_ht IS 'En cents';
COMMENT ON COLUMN receipts.tax_details IS 'Ventilation par taux ex: {"1000": 150, "2000": 300}';
COMMENT ON COLUMN receipts.items_snapshot IS 'Copie des produits vendus';
COMMENT ON COLUMN receipts.payments_snapshot IS 'Copie des paiements';

-- ---------------------------------------------------------------------
-- recipes
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
-- ---------------------------------------------------------------------
CREATE TABLE recipes (
    recipe_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    product_id integer NOT NULL,
    preparation_time integer NOT NULL DEFAULT 0,
    PRIMARY KEY (recipe_id)
);
COMMENT ON COLUMN recipes.preparation_time IS 'in seconds';

-- ---------------------------------------------------------------------
-- requires
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : recipe_id -> recipes.recipe_id
--   FK candidate (non creee) : component_id -> components.component_id
--   FK candidate (non creee) : consumable_id -> consumables.consumable_id
-- ---------------------------------------------------------------------
CREATE TABLE requires (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    recipe_id integer NOT NULL,
    component_id integer,
    consumable_id integer,
    quantity double precision NOT NULL DEFAULT 0,
    unit_of_measure integer NOT NULL,
    in_orders boolean NOT NULL DEFAULT true,
    take_away_orders boolean NOT NULL DEFAULT true,
    delivery_orders boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    last_update timestamptz,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN requires.last_update IS 'deactivation date (server time)';

-- ---------------------------------------------------------------------
-- restaurant_ticket
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : payment_id -> payments.payment_id
-- ---------------------------------------------------------------------
CREATE TABLE restaurant_ticket (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    payment_id integer,
    barcode varchar(20) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- scannorder_session
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE scannorder_session (
    user_code varchar(255) NOT NULL,
    user_name varchar(40) NOT NULL,
    PRIMARY KEY (user_code)
);

-- ---------------------------------------------------------------------
-- scannorder_settings
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE scannorder_settings (
    merchant_id varchar(64) NOT NULL,
    activated boolean NOT NULL DEFAULT false,
    show_address boolean NOT NULL DEFAULT false,
    header_background text,
    header_background_url varchar(255),
    home_page boolean NOT NULL DEFAULT false,
    home_page_title varchar(50),
    home_page_desc text,
    info_popup_enabled boolean NOT NULL DEFAULT false,
    info_popup_title text NOT NULL DEFAULT 'Wow, Attends une seconde !',
    info_popup_content text NOT NULL DEFAULT '''La vente d\''alcool est réservée à un public majeur. L\''abus d\''alcool est dangereux pour la santé, consommez avec modération !''',
    info_popup_button_content text NOT NULL DEFAULT 'J''ai compris !',
    product_bg_color varchar(9) NOT NULL DEFAULT '#ffffff',
    nav_bg_color varchar(9) NOT NULL DEFAULT '#ffffff',
    bg_color varchar(9) NOT NULL DEFAULT '#f5f5f5',
    btn_color varchar(9) NOT NULL DEFAULT '#0259b6',
    btn_text_color varchar(9) NOT NULL DEFAULT '#ffffff',
    product_categ_bg_color varchar(9) NOT NULL DEFAULT '#ffffff',
    product_categ_text_color varchar(9) NOT NULL DEFAULT '#927c0c',
    popup_bg_color varchar(9) NOT NULL DEFAULT '#f2f2f2',
    popup_text_color varchar(9) NOT NULL DEFAULT '#000000',
    ad_text_color varchar(9) NOT NULL DEFAULT '#b3b3b3',
    home_text_color varchar(9) NOT NULL DEFAULT '#5e5e5e',
    product_text_color varchar(9) NOT NULL DEFAULT '#000000',
    discount_color varchar(11) NOT NULL DEFAULT '#227e00',
    discount_text_color varchar(11) NOT NULL DEFAULT '#ffffff',
    border_radius varchar(8) NOT NULL DEFAULT '21',
    shadow_style varchar(8) NOT NULL DEFAULT '0',
    delivery_type integer NOT NULL DEFAULT 1,
    enable_payments boolean NOT NULL DEFAULT false,
    variable_fees double precision NOT NULL DEFAULT 0.007,
    fixed_fees integer NOT NULL DEFAULT 15,
    users_default_name varchar(40) NOT NULL DEFAULT 'Utilisateur',
    take_away_enabled boolean NOT NULL DEFAULT true,
    take_away_available boolean NOT NULL DEFAULT true,
    delivery_enabled boolean NOT NULL DEFAULT true,
    delivery_available boolean NOT NULL DEFAULT true,
    in_enabled boolean NOT NULL DEFAULT false,
    in_available boolean NOT NULL DEFAULT false,
    seo_title varchar(255) NOT NULL,
    seo_description varchar(512) NOT NULL,
    seo_keywords varchar(512) NOT NULL,
    seo_cuisine_type varchar(255) NOT NULL,
    commission_rate integer NOT NULL DEFAULT 0,
    last_sync timestamptz,
    synced_items integer NOT NULL DEFAULT 0,
    logo_url varchar(512),
    banner_url varchar(512),
    header_title varchar(255),
    header_text varchar(512),
    cgv_link varchar(512),
    return_policy_link varchar(512),
    legal_notices_link varchar(512),
    takeaway_auto_accept boolean NOT NULL DEFAULT false,
    delivery_auto_accept boolean NOT NULL DEFAULT false,
    closed_until timestamptz,
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN scannorder_settings.activated IS 'Is ScanNOrder activated ? updatable with merchant app';
COMMENT ON COLUMN scannorder_settings.home_page IS 'Show Home Page ?';
COMMENT ON COLUMN scannorder_settings.delivery_type IS '1 => prep., pay, SNO / 2 => pay, prep, take, SNO';
COMMENT ON COLUMN scannorder_settings.commission_rate IS 'Commission rate in percent (0 for internal tool)';
COMMENT ON COLUMN scannorder_settings.last_sync IS 'Last settings sync timestamp (UTC)';
COMMENT ON COLUMN scannorder_settings.synced_items IS 'Number of products currently available on ScanNOrder';
COMMENT ON COLUMN scannorder_settings.logo_url IS 'Merchant logo URL';
COMMENT ON COLUMN scannorder_settings.banner_url IS 'Merchant banner/cover URL';
COMMENT ON COLUMN scannorder_settings.header_title IS 'Hero section title shown on the ordering page';
COMMENT ON COLUMN scannorder_settings.header_text IS 'Hero section subtitle/body text';
COMMENT ON COLUMN scannorder_settings.cgv_link IS 'URL to general terms and conditions';
COMMENT ON COLUMN scannorder_settings.return_policy_link IS 'URL to return / refund policy';
COMMENT ON COLUMN scannorder_settings.legal_notices_link IS 'URL to legal notices';
COMMENT ON COLUMN scannorder_settings.takeaway_auto_accept IS '1 = takeaway orders are auto-accepted';
COMMENT ON COLUMN scannorder_settings.delivery_auto_accept IS '1 = delivery orders are auto-accepted';
COMMENT ON COLUMN scannorder_settings.closed_until IS 'If set in the future (UTC), ScanNOrder is temporarily closed until this timestamp';

-- ---------------------------------------------------------------------
-- services_performed
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : cash_desk_id -> cash_desks.cash_desk_id
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
-- ---------------------------------------------------------------------
CREATE TABLE services_performed (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    cash_desk_id integer NOT NULL,
    cash_register_id integer,
    planned_shift_id integer,
    start_date timestamptz NOT NULL DEFAULT now(),
    clock_in_photo_url text,
    end_date timestamptz,
    clock_out_photo_url text,
    shift_offset integer,
    shift_duration integer,
    extra_hours integer,
    confirmed boolean,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- session_orderitem
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE session_orderitem (
    user_code varchar(255) NOT NULL,
    order_item_id integer NOT NULL,
    quantity integer NOT NULL,
    paid_quantity integer NOT NULL DEFAULT 0,
    payment_intent_quantity integer NOT NULL DEFAULT 0,
    PRIMARY KEY (user_code, order_item_id)
);

-- ---------------------------------------------------------------------
-- shift_templates
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE shift_templates (
    shift_template_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    created_by integer NOT NULL,
    template_name varchar(50) NOT NULL,
    planning_role_id integer NOT NULL,
    start_hour time NOT NULL,
    end_hour time NOT NULL,
    creation_date timestamptz NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (shift_template_id)
);

-- ---------------------------------------------------------------------
-- shift_templates_items
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : shift_template_id -> shift_templates.shift_template_id
-- ---------------------------------------------------------------------
CREATE TABLE shift_templates_items (
    item_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    shift_template_id integer NOT NULL,
    week_day integer NOT NULL,
    PRIMARY KEY (item_id)
);

-- ---------------------------------------------------------------------
-- stock_evolution_records
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : component_id -> components.component_id
-- ---------------------------------------------------------------------
CREATE TABLE stock_evolution_records (
    record_date date NOT NULL,
    component_id integer NOT NULL,
    stock real NOT NULL,
    PRIMARY KEY (record_date, component_id)
);

-- ---------------------------------------------------------------------
-- stock_movements
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : component_id -> components.component_id
--   FK candidate (non creee) : consumable_id -> consumables.consumable_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE stock_movements (
    id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    user_id varchar(64) NOT NULL,
    component_id varchar(50),
    consumable_id varchar(50),
    product_id varchar(50),
    order_item_id varchar(50),
    order_id integer,
    source varchar(20) NOT NULL,
    movement varchar(20) NOT NULL,
    quantity real NOT NULL,
    unit_of_measure varchar(50) NOT NULL,
    comment varchar(255),
    component_cost integer,
    movement_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN stock_movements.source IS 'Source of the movement';
COMMENT ON COLUMN stock_movements.movement IS 'Add, remove, consume';
COMMENT ON COLUMN stock_movements.component_cost IS 'in cents';
CREATE INDEX idx_stock_movements_idx_stock_movements_order_id ON stock_movements (order_id);

-- ---------------------------------------------------------------------
-- stock_movements_desc
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE stock_movements_desc (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    lang varchar(2) NOT NULL,
    movement_desc varchar(40) NOT NULL,
    multiplier integer NOT NULL,
    PRIMARY KEY (id, lang)
);

-- ---------------------------------------------------------------------
-- stock_movements_source
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE stock_movements_source (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    lang varchar(2) NOT NULL,
    movement_source varchar(40) NOT NULL,
    PRIMARY KEY (id, lang)
);

-- ---------------------------------------------------------------------
-- stripe_accounts
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : customer_id -> customer.customer_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE stripe_accounts (
    account_id varchar(255) NOT NULL,
    customer_id varchar(50),
    merchant_id varchar(64) NOT NULL,
    verification_status varchar(50) NOT NULL DEFAULT 'action_required',
    terminal_location_id varchar(255),
    PRIMARY KEY (merchant_id)
);
COMMENT ON COLUMN stripe_accounts.verification_status IS '"verified" | "action_required" — mirrored from Stripe account.charges_enabled + payouts_enabled';

-- ---------------------------------------------------------------------
-- stripe_payments
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : payment_id -> payments.payment_id
-- ---------------------------------------------------------------------
CREATE TABLE stripe_payments (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    link_key varchar(100),
    order_id integer NOT NULL,
    payment_id integer,
    payment_intent_id varchar(200),
    payment_intent_status varchar(30) NOT NULL DEFAULT 'REQUIRES_CONFIRMATION',
    checkout_session_id text,
    success_key varchar(100) NOT NULL,
    customer_email text,
    stripe_session_date timestamptz NOT NULL DEFAULT now(),
    wello_resto_total_fees integer NOT NULL DEFAULT 0,
    stripe_total_fees integer NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_stripe_payments_stripe_order_link ON stripe_payments (link_key);

-- ---------------------------------------------------------------------
-- subscriptions
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE subscriptions (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    stripe_subscription_id varchar(150) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    package_id integer NOT NULL,
    planning_enabled boolean NOT NULL DEFAULT false,
    haccp_enabled boolean NOT NULL DEFAULT false,
    stock_enabled boolean NOT NULL DEFAULT false,
    scannorder_enabled boolean NOT NULL DEFAULT true,
    bookings_enabled boolean NOT NULL DEFAULT false,
    kiosks_enabled boolean NOT NULL DEFAULT false,
    max_kiosks integer NOT NULL DEFAULT 0,
    PRIMARY KEY (id, merchant_id, package_id)
);
COMMENT ON COLUMN subscriptions.max_kiosks IS 'Nombre max de bornes actives (0 = module non inclus)';

-- ---------------------------------------------------------------------
-- subscription_invoices
--   invoice_date: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE subscription_invoices (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    invoice_id varchar(50) NOT NULL,
    status integer NOT NULL DEFAULT 0,
    invoice_date timestamptz NOT NULL DEFAULT now(),
    amount integer NOT NULL,
    payment_date timestamptz,
    comment varchar(150),
    PRIMARY KEY (id)
);
COMMENT ON COLUMN subscription_invoices.status IS '0 => open / 1 => paid / -1 => error';
COMMENT ON COLUMN subscription_invoices.amount IS 'in cents';

-- ---------------------------------------------------------------------
-- sub_cash_registers
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : cash_register_id -> cash_registers.cash_register_id
--   FK candidate (non creee) : device_id -> device_link.device_id | users_devices.device_id
--   [tier1] cash_register_id aligne varchar(20) -> integer (source MySQL incoherente :
--   varchar referencant un PK int ; jointure user_services impossible en PG sans cast).
--   Migration de donnees : CAST(cash_register_id AS UNSIGNED) a la copie.
-- ---------------------------------------------------------------------
CREATE TABLE sub_cash_registers (
    cash_register_id integer NOT NULL,
    sub_cash_register_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    device_id varchar(50) NOT NULL,
    cash_fund integer NOT NULL,
    start_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sub_cash_register_id, cash_register_id)
);
COMMENT ON COLUMN sub_cash_registers.cash_fund IS 'in cents';

-- ---------------------------------------------------------------------
-- sys_attendance_sources
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE sys_attendance_sources (
    code varchar(32) NOT NULL,
    label varchar(80) NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (code)
);

-- ---------------------------------------------------------------------
-- sys_contract_types
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE sys_contract_types (
    code varchar(32) NOT NULL,
    label varchar(80) NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (code)
);

-- ---------------------------------------------------------------------
-- sys_planning_event_types
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE sys_planning_event_types (
    code varchar(32) NOT NULL,
    label varchar(80) NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (code)
);

-- ---------------------------------------------------------------------
-- tags
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE tags (
    tag_id varchar(42) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(50) NOT NULL,
    color varchar(9) NOT NULL DEFAULT '#ffffff',
    display_order integer NOT NULL DEFAULT 0,
    PRIMARY KEY (tag_id)
);

-- ---------------------------------------------------------------------
-- temperature_readings
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE temperature_readings (
    id varchar(64) NOT NULL,
    session_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    zone_id varchar(64) NOT NULL,
    value numeric(5,2) NOT NULL,
    status temperature_readings_status_enum NOT NULL DEFAULT 'ok',
    photo_url varchar(512),
    signature text,
    comment text,
    created_by varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- temperature_reading_corrective_actions
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE temperature_reading_corrective_actions (
    id varchar(64) NOT NULL,
    reading_id varchar(64) NOT NULL,
    action_id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    note text,
    photo_url varchar(512),
    follow_up_value numeric(5,2),
    created_by varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_temperature_reading_corrective_actions_idx_trca_reading ON temperature_reading_corrective_actions (reading_id);
CREATE INDEX idx_temperature_reading_corrective_actions_idx_trca_action ON temperature_reading_corrective_actions (action_id);
CREATE INDEX idx_temperature_reading_corrective_actions_idx_trca_merchant ON temperature_reading_corrective_actions (merchant_id);

-- ---------------------------------------------------------------------
-- temperature_sessions
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE temperature_sessions (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    created_by varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- temperature_zones
--   updated_at: ON UPDATE current_timestamp() sans equivalent declaratif en PG -> necessite un trigger (voir notes)
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE temperature_zones (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(150) NOT NULL,
    target_temp_min numeric(5,2) NOT NULL,
    target_temp_max numeric(5,2) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- timezone_info
--   !! PAS DE PRIMARY KEY dans le DDL source (deja le cas en MySQL) — a traiter avant migration logique/replication
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE timezone_info (
    timezone varchar(30) NOT NULL,
    "offset" varchar(6) NOT NULL
);

-- ---------------------------------------------------------------------
-- tva_categories
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE tva_categories (
    tva_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    delivery_type varchar(20) NOT NULL,
    tva_title varchar(30) NOT NULL,
    tva_desc varchar(150) NOT NULL,
    tva_rate real NOT NULL,
    show_in_report boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (tva_id)
);
COMMENT ON COLUMN tva_categories.delivery_type IS '0 => in, 1 => delivery, 3=> take away (2 not used because 2 is SNO is "isDelivery" field or orders)';
COMMENT ON COLUMN tva_categories.tva_rate IS 'in percent (5 => 5%)';

-- ---------------------------------------------------------------------
-- unit_of_measure
--   UOM: identifiant mixed-case 'UOM' : PG replie en 'uom' (sans impact pour les requetes Go non quotees)
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE unit_of_measure (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    UOM varchar(5) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- unit_of_measure_convert
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE unit_of_measure_convert (
    id_from integer NOT NULL,
    id_to integer NOT NULL,
    ratio real NOT NULL,
    PRIMARY KEY (id_from, id_to)
);

-- ---------------------------------------------------------------------
-- unit_of_measure_desc
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE unit_of_measure_desc (
    id integer NOT NULL,
    lang varchar(3) NOT NULL,
    uom_desc text NOT NULL,
    uom_short_desc varchar(20),
    PRIMARY KEY (id, lang)
);

-- ---------------------------------------------------------------------
-- upsell_suggestions
--   suggested_items: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   accepted_items: longtext + CHECK json_valid -> JSONB (le CHECK MySQL devient implicite)
--   collation table utf8mb4_uca1400_ai_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : order_id -> orders.order_id
-- ---------------------------------------------------------------------
CREATE TABLE upsell_suggestions (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    order_id integer,
    cart_signature varchar(64) NOT NULL,
    suggested_items jsonb NOT NULL,
    source varchar(32) NOT NULL,
    accepted_items jsonb,
    revenue_impact numeric(10,2),
    llm_provider varchar(32),
    llm_model varchar(64),
    tokens_in integer,
    tokens_out integer,
    latency_ms integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    staff_member_id varchar(64),
    channel upsell_suggestions_channel_enum NOT NULL DEFAULT 'POS',
    PRIMARY KEY (id)
);
CREATE INDEX idx_upsell_suggestions_idx_upsell_merchant_created ON upsell_suggestions (merchant_id, created_at);
CREATE INDEX idx_upsell_suggestions_idx_upsell_cart_merchant ON upsell_suggestions (cart_signature, merchant_id);
-- NB : index MySQL avec prefixe (accepted_items(1)) — les index prefixes n'existent pas en PG ;
--      colonne(s) prefixee(s) retiree(s) de l'index, a revoir si la selectivite comptait.
CREATE INDEX idx_upsell_suggestions_idx_upsell_acceptance ON upsell_suggestions (merchant_id);

-- ---------------------------------------------------------------------
-- users
--   userName: identifiant mixed-case 'userName' : PG replie en 'username' (sans impact pour les requetes Go non quotees)
--   isReception: identifiant mixed-case 'isReception' : PG replie en 'isreception' (sans impact pour les requetes Go non quotees)
--   isWaiter: identifiant mixed-case 'isWaiter' : PG replie en 'iswaiter' (sans impact pour les requetes Go non quotees)
--   isDelivery: identifiant mixed-case 'isDelivery' : PG replie en 'isdelivery' (sans impact pour les requetes Go non quotees)
--   creationDate: identifiant mixed-case 'creationDate' : PG replie en 'creationdate' (sans impact pour les requetes Go non quotees)
--   lastAccess: identifiant mixed-case 'lastAccess' : PG replie en 'lastaccess' (sans impact pour les requetes Go non quotees)
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE users (
    user_id varchar(64) NOT NULL,
    merchant_id varchar(64),
    name varchar(255) NOT NULL,
    first_name varchar(40) NOT NULL,
    last_name varchar(40) NOT NULL,
    password varchar(255) NOT NULL,
    pin_code varchar(6),
    mfa_type varchar(25),
    mfa_status varchar(25),
    mfa_verified_at timestamptz,
    mfa_otp_sent_at timestamptz,
    mfa_secret varchar(50),
    userName varchar(20),
    email varchar(255) NOT NULL,
    email_verified_at timestamptz,
    dob date,
    tel varchar(20),
    tel_verified_at timestamptz,
    address varchar(255),
    street_number varchar(20),
    street varchar(255),
    city varchar(255),
    country varchar(255),
    zip_code varchar(9),
    lat text,
    lng text,
    heading integer NOT NULL DEFAULT 0,
    profile_picture text,
    planning_color varchar(11) NOT NULL DEFAULT '#28B2FC',
    isReception boolean NOT NULL DEFAULT false,
    isWaiter boolean NOT NULL DEFAULT false,
    isDelivery integer NOT NULL DEFAULT 0,
    admin boolean NOT NULL DEFAULT false,
    access_id integer,
    waiter_device_token varchar(255),
    reception_device_token varchar(255),
    delivery_device_token varchar(255),
    token varchar(64) NOT NULL,
    terms_of_use_accepted boolean NOT NULL DEFAULT false,
    creationDate timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz DEFAULT now(),
    lastAccess timestamptz,
    last_activity timestamptz NOT NULL DEFAULT now(),
    enabled boolean NOT NULL DEFAULT true,
    last_login_at timestamptz,
    last_position_at timestamptz,
    PRIMARY KEY (user_id)
);
COMMENT ON COLUMN users.first_name IS 'Prénom';
COMMENT ON COLUMN users.last_name IS 'Nom';
COMMENT ON COLUMN users.dob IS 'date of birth';
COMMENT ON COLUMN users.waiter_device_token IS 'Device token of WR Waitrer';
COMMENT ON COLUMN users.reception_device_token IS 'Device token of WR Reception';
COMMENT ON COLUMN users.delivery_device_token IS 'Device token of WR Delivery';
COMMENT ON COLUMN users.lastAccess IS 'can be deleted (29/05/2026)';
CREATE UNIQUE INDEX uq_users_name ON users (name);

-- ---------------------------------------------------------------------
-- users_devices
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : device_id -> device_link.device_id
-- ---------------------------------------------------------------------
CREATE TABLE users_devices (
    user_id varchar(64) NOT NULL,
    merchant_id varchar(64),
    app varchar(20) NOT NULL,
    device_id varchar(255) NOT NULL,
    fcm_token text NOT NULL,
    last_used timestamptz NOT NULL,
    PRIMARY KEY (device_id)
);

-- ---------------------------------------------------------------------
-- users_nfc_tags
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : tag_id -> tags.tag_id
-- ---------------------------------------------------------------------
CREATE TABLE users_nfc_tags (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    user_id varchar(64),
    tag_id integer NOT NULL,
    tag_token varchar(50) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_users_nfc_tags_tag_token ON users_nfc_tags (tag_token);

-- ---------------------------------------------------------------------
-- users_rights
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE users_rights (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id varchar(64),
    merchant_id varchar(64) NOT NULL,
    token varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    access_wrwaiter boolean NOT NULL DEFAULT true,
    access_wrreception boolean NOT NULL DEFAULT true,
    access_wrdelivery boolean NOT NULL DEFAULT true,
    position_id varchar(64),
    position_note text,
    job_title varchar(150),
    role varchar(32) NOT NULL DEFAULT 'employee',
    contract_type_code varchar(32),
    contract_start_date date,
    contract_end_date date,
    probation_end_date date,
    last_medical_checkup_date date,
    contract_hours numeric(5,2) NOT NULL DEFAULT 35.00,
    max_weekly_hours numeric(5,2) NOT NULL DEFAULT 35.00,
    required_rest_days integer NOT NULL DEFAULT 2,
    sunday_premium boolean NOT NULL DEFAULT false,
    night_premium boolean NOT NULL DEFAULT false,
    hourly_rate bigint NOT NULL DEFAULT 0,
    gross_monthly_salary bigint NOT NULL DEFAULT 0,
    employer_charges_pct numeric(5,2) NOT NULL DEFAULT 45.00,
    transport_cost bigint NOT NULL DEFAULT 0,
    hr_comment text,
    manage_menu boolean NOT NULL DEFAULT false,
    manage_plannings boolean NOT NULL DEFAULT false,
    manage_users boolean NOT NULL DEFAULT false,
    manage_settings boolean NOT NULL DEFAULT false,
    manage_haccp boolean NOT NULL DEFAULT false,
    view_reports boolean NOT NULL DEFAULT false,
    export_reports boolean NOT NULL DEFAULT false,
    view_financials boolean NOT NULL DEFAULT false,
    export_financials boolean NOT NULL DEFAULT false,
    manage_customers boolean NOT NULL DEFAULT false,
    export_customers boolean NOT NULL DEFAULT false,
    admin boolean NOT NULL DEFAULT false,
    print_merchant_cash_report boolean NOT NULL DEFAULT false,
    open_cash_drawer boolean NOT NULL DEFAULT false,
    last_login_at timestamptz NOT NULL DEFAULT now(),
    login_enabled boolean NOT NULL DEFAULT true,
    pin_hash varchar(64),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- user_vacations
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : user_id -> users.user_id
-- ---------------------------------------------------------------------
CREATE TABLE user_vacations (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    user_id varchar(64) NOT NULL,
    start_date timestamptz NOT NULL,
    end_date timestamptz NOT NULL,
    reason varchar(255),
    created_at timestamptz DEFAULT now(),
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- welloresto_stripe_customers
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE welloresto_stripe_customers (
    merchant_id varchar(64) NOT NULL,
    creator_user_id varchar(64),
    stripe_customer_id varchar(255) NOT NULL,
    PRIMARY KEY (merchant_id)
);

-- ---------------------------------------------------------------------
-- without
--   collation table utf8mb3_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
--   FK candidate (non creee) : order_id -> orders.order_id
--   FK candidate (non creee) : component_id -> components.component_id
--   FK candidate (non creee) : product_id -> product_marketing_categories.product_id
--   FK candidate (non creee) : merchant_id -> average_distribution_time.merchant_id | average_distribution_time_by_category.merchant_id | employment_agreement.merchant_id | haccp_settings.merchant_id | integration_deliveroo.merchant_id | integration_uber_direct.merchant_id | integration_uber_eats.merchant_id | kiosk_settings.merchant_id | merchant_parameters.merchant_id | scannorder_settings.merchant_id | stripe_accounts.merchant_id | welloresto_stripe_customers.merchant_id
-- ---------------------------------------------------------------------
CREATE TABLE without (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    order_id integer NOT NULL,
    order_item_id integer NOT NULL DEFAULT 0,
    component_id integer NOT NULL,
    product_id integer NOT NULL,
    merchant_id varchar(64) NOT NULL,
    PRIMARY KEY (id)
);

-- ---------------------------------------------------------------------
-- z_platform_daily_activity_recording
--   !! PAS DE PRIMARY KEY dans le DDL source (deja le cas en MySQL) — a traiter avant migration logique/replication
--   collation table utf8mb4_unicode_ci (insensible casse/accents) -> collation PG par defaut sensible a la casse ; colonnes candidates CITEXT/LOWER listees dans les notes
-- ---------------------------------------------------------------------
CREATE TABLE z_platform_daily_activity_recording (
    date date NOT NULL,
    email_sent integer NOT NULL DEFAULT 0,
    direction_api_calls integer NOT NULL DEFAULT 0,
    matrix_api_calls integer NOT NULL DEFAULT 0
);

-- =====================================================================
-- VUE user_status_view (traduction de la vue MySQL, DEFINER/ALGORITHM retires)
-- =====================================================================
CREATE VIEW user_status_view AS
SELECT
    u.user_id,
    u.first_name,
    u.last_name,
    u.lat,
    u.lng,
    u.heading,
    CASE
        WHEN u.enabled = false THEN 'DISABLED'
        WHEN ds.id IS NOT NULL AND ds.status IN ('1', 'PENDING', 'active') THEN 'IN_DELIVERY_SESSION'
        WHEN EXISTS (
            SELECT 1 FROM user_vacations v
            WHERE v.user_id = u.user_id
              AND now() BETWEEN v.start_date AND v.end_date
        ) THEN 'VACATIONS'
        ELSE 'AVAILABLE'
    END AS status
FROM users u
LEFT JOIN delivery_session ds
       ON ds.user_id = u.user_id
      AND ds.status IN ('1', 'PENDING', 'active');

-- =====================================================================
-- COLONNES MySQL 'ON UPDATE current_timestamp()' : comportement NON reproduit.
-- A decider : trigger BEFORE UPDATE generique, ou mise a jour explicite cote Go
-- (la plupart des repositories ecrivent deja update_date explicitement).
-- =====================================================================
--   bookings.creation_date
--   broadcast_list.create_date
--   calendar.date
--   discounts.valid_from
--   employees.updated_at
--   employee_documents.updated_at
--   floors.creation_date
--   goods_receipts.updated_at
--   haccp_corrective_actions.updated_at
--   haccp_settings.updated_at
--   holiday_calendar.updated_at
--   hours_amendments.updated_at
--   hours_of_operation.valid_from
--   kiosks.updated_at
--   kiosk_settings.updated_at
--   labor_rules.updated_at
--   marketing_categories.updated_at
--   migration_users.updatedAt
--   order_comments.creation_date
--   payments.payment_date
--   planning_holiday_overrides.updated_at
--   planning_leave_requests.updated_at
--   planning_positions.updated_at
--   planning_revenue_forecasts.updated_at
--   planning_settings.updated_at
--   planning_shifts.updated_at
--   planning_shift_swap_requests.updated_at
--   planning_shift_templates.updated_at
--   planning_time_entries.updated_at
--   planning_weeks.updated_at
--   planning_week_templates.updated_at
--   planning_week_template_shifts.updated_at
--   printers.updated_at
--   product_marketing_categories.updated_at
--   purchased_components.registration_date
--   subscription_invoices.invoice_date
--   temperature_readings.updated_at
--   temperature_reading_corrective_actions.updated_at
--   temperature_sessions.updated_at
--   temperature_zones.updated_at

COMMIT;

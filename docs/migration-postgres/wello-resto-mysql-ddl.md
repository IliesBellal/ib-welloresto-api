-- phpMyAdmin SQL Dump
-- version 5.2.2
-- https://www.phpmyadmin.net/
--
-- Hôte : 127.0.0.1:3306
-- Généré le : lun. 13 juil. 2026 à 11:05
-- Version du serveur : 11.8.8-MariaDB-log
-- Version de PHP : 7.2.34

SET SQL_MODE = "NO_AUTO_VALUE_ON_ZERO";
START TRANSACTION;
SET time_zone = "+00:00";


/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!40101 SET NAMES utf8mb4 */;

--
-- Base de données : `u231520952_welloresto`
--

-- --------------------------------------------------------

--
-- Structure de la table `allergens`
--

CREATE TABLE `allergens` (
  `allergen_id` varchar(35) NOT NULL,
  `name` varchar(50) NOT NULL,
  `code` varchar(12) NOT NULL,
  `icon` varchar(12) NOT NULL,
  `color` varchar(12) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `api_calls`
--

CREATE TABLE `api_calls` (
  `id` int(255) NOT NULL,
  `user_id` int(11) NOT NULL,
  `query` varchar(50) NOT NULL,
  `uri` longtext NOT NULL,
  `date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `api_request_logs`
--

CREATE TABLE `api_request_logs` (
  `id` bigint(20) NOT NULL,
  `user_id` bigint(20) DEFAULT NULL,
  `merchant_id` bigint(20) DEFAULT NULL,
  `method` varchar(10) DEFAULT NULL,
  `url` text DEFAULT NULL,
  `payload` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`payload`)),
  `status_code` int(11) DEFAULT NULL,
  `ip` varchar(45) DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `app_version`
--

CREATE TABLE `app_version` (
  `id` int(11) NOT NULL,
  `app_id` varchar(25) NOT NULL COMMENT '0 => merchant\r\n1 => delivery\r\n2 => waiter',
  `version_code` int(11) NOT NULL,
  `last_functional_version_code` int(11) NOT NULL,
  `download_url` varchar(255) NOT NULL,
  `release_date` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `app_version_merchant`
--

CREATE TABLE `app_version_merchant` (
  `id` int(11) NOT NULL,
  `version_code` int(11) NOT NULL,
  `merchant_id` varchar(25) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `audit_logs`
--

CREATE TABLE `audit_logs` (
  `id` varchar(36) NOT NULL,
  `user_id` varchar(36) DEFAULT NULL,
  `merchant_id` varchar(36) DEFAULT NULL,
  `action` varchar(50) DEFAULT NULL,
  `resource_type` varchar(50) DEFAULT NULL,
  `resource_id` varchar(36) DEFAULT NULL,
  `old_values` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`old_values`)),
  `new_values` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`new_values`)),
  `previous_hash` varchar(64) DEFAULT NULL,
  `hash` varchar(64) NOT NULL,
  `created_at` timestamp NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `availabilities`
--

CREATE TABLE `availabilities` (
  `availability_id` varchar(50) NOT NULL,
  `merchant_id` varchar(50) NOT NULL,
  `availability_name` varchar(50) NOT NULL,
  `unavailable_message` varchar(50) NOT NULL,
  `available` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 when availability is deleted',
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `update_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `availabilities_products`
--

CREATE TABLE `availabilities_products` (
  `availability_product_id` varchar(50) NOT NULL,
  `availability_id` varchar(50) NOT NULL,
  `product_id` int(11) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 when deleted',
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `update_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `availabilities_schedules`
--

CREATE TABLE `availabilities_schedules` (
  `schedule_id` varchar(50) NOT NULL,
  `availability_id` varchar(50) NOT NULL,
  `day_of_week` int(11) NOT NULL,
  `available_from` time NOT NULL,
  `available_to` time NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 if deleted',
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `update_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `available_languages`
--

CREATE TABLE `available_languages` (
  `code` varchar(5) NOT NULL,
  `name` text NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `average_distribution_time`
--

CREATE TABLE `average_distribution_time` (
  `merchant_id` int(11) NOT NULL,
  `distribution_time` int(11) NOT NULL COMMENT 'In seconds'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `average_distribution_time_by_category`
--

CREATE TABLE `average_distribution_time_by_category` (
  `merchant_id` int(11) NOT NULL,
  `category` varchar(11) NOT NULL,
  `distribution_time` int(11) NOT NULL COMMENT 'In seconds'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `average_distribution_time_history`
--

CREATE TABLE `average_distribution_time_history` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `category` varchar(30) NOT NULL,
  `distribution_time` int(11) NOT NULL,
  `calculation_date` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `barcodes`
--

CREATE TABLE `barcodes` (
  `barcode` varchar(25) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `component_id` int(11) NOT NULL,
  `quantity` int(11) NOT NULL DEFAULT 0,
  `uom` int(11) NOT NULL DEFAULT 0,
  `last_scan` timestamp NOT NULL DEFAULT current_timestamp(),
  `price` float NOT NULL DEFAULT 0,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `booked_location`
--

CREATE TABLE `booked_location` (
  `id` int(11) NOT NULL,
  `booking_id` int(11) NOT NULL,
  `location_id` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `bookings`
--

CREATE TABLE `bookings` (
  `booking_id` int(11) NOT NULL,
  `booking_number` varchar(6) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `order_id` int(11) DEFAULT NULL,
  `party_size` int(11) NOT NULL,
  `customer_id` int(11) NOT NULL,
  `comment` varchar(255) DEFAULT NULL,
  `booking_date_from` timestamp NOT NULL DEFAULT '0000-00-00 00:00:00',
  `booking_date_to` timestamp NOT NULL DEFAULT '0000-00-00 00:00:00',
  `booking_duration` int(11) NOT NULL,
  `created_by` varchar(20) NOT NULL COMMENT 'user id',
  `updated_by` varchar(30) DEFAULT NULL,
  `status` varchar(20) NOT NULL COMMENT '-1 => deleted\r\n0 => finished\r\n1 => order opened\r\n2 => validated\r\n3 => pending validation',
  `source` varchar(16) NOT NULL DEFAULT 'staff',
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `last_update_date` timestamp NULL DEFAULT NULL,
  `sequence_number` int(11) NOT NULL DEFAULT 0,
  `deletion_date` timestamp NULL DEFAULT NULL,
  `reminder_sent_at` timestamp NULL DEFAULT NULL,
  `deletion_reason_id` int(11) DEFAULT NULL,
  `deletion_reason_desc` text DEFAULT NULL,
  `cancelled_by` varchar(64) DEFAULT NULL COMMENT 'SYSTEM | CUSTOMER | user_id staff'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `bookings_settings`
--

CREATE TABLE `bookings_settings` (
  `id` int(11) NOT NULL,
  `merchant_id` varchar(20) NOT NULL,
  `code` varchar(30) NOT NULL,
  `default_booking_duration` int(11) NOT NULL DEFAULT 90,
  `auto_accept_reserve_bookings` tinyint(1) NOT NULL DEFAULT 1,
  `reserve_maximum_party_size` int(11) NOT NULL DEFAULT 8,
  `reserve_minimum_party_size` int(11) DEFAULT NULL,
  `first_booking_offset_minutes` int(11) NOT NULL DEFAULT 0,
  `last_booking_offset_minutes` int(11) NOT NULL DEFAULT 60,
  `overbooking_percent` int(11) DEFAULT NULL,
  `max_booking_horizon_days` int(11) DEFAULT NULL,
  `min_booking_notice_minutes` int(11) DEFAULT NULL,
  `cancel_booking_limit_offset_hours` int(11) NOT NULL DEFAULT 48,
  `sms_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `waitlist_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `waitlist_max_size` int(11) NOT NULL DEFAULT 0,
  `waitlist_slot_expiry_minutes` int(11) NOT NULL DEFAULT 15,
  `pending_expiration_hours` int(11) NOT NULL DEFAULT 24,
  `slot_interval_minutes` int(11) NOT NULL DEFAULT 15,
  `cancelable_by_customer` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `creation_date` timestamp NULL DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `booking_duration_rules`
--

CREATE TABLE `booking_duration_rules` (
  `rule_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `min_party_size` int(11) NOT NULL,
  `max_party_size` int(11) NOT NULL,
  `duration_minutes` int(11) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

-- --------------------------------------------------------

--
-- Structure de la table `booking_events`
--

CREATE TABLE `booking_events` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `booking_id` int(11) DEFAULT NULL,
  `waitlist_id` varchar(64) DEFAULT NULL,
  `event_type` varchar(64) NOT NULL,
  `source` varchar(64) DEFAULT NULL,
  `actor` varchar(64) DEFAULT NULL,
  `metadata` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`metadata`)),
  `created_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

-- --------------------------------------------------------

--
-- Structure de la table `booking_waitlist`
--

CREATE TABLE `booking_waitlist` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `customer_id` varchar(64) DEFAULT NULL,
  `party_size` int(11) NOT NULL DEFAULT 1,
  `customer_name` varchar(255) NOT NULL,
  `customer_phone` varchar(50) NOT NULL,
  `notes` text DEFAULT NULL,
  `status` enum('waiting','notified','seated','expired','cancelled') NOT NULL DEFAULT 'waiting',
  `notified_at` datetime DEFAULT NULL,
  `expires_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

-- --------------------------------------------------------

--
-- Structure de la table `brands`
--

CREATE TABLE `brands` (
  `brand_id` varchar(35) NOT NULL,
  `name` varchar(50) NOT NULL,
  `slug` varchar(50) NOT NULL,
  `logo_url` varchar(255) NOT NULL,
  `banner_url` varchar(255) NOT NULL,
  `description` varchar(255) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `broadcast_list`
--

CREATE TABLE `broadcast_list` (
  `id` int(11) NOT NULL,
  `contact` varchar(255) NOT NULL,
  `create_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `calendar`
--

CREATE TABLE `calendar` (
  `date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cash_desks`
--

CREATE TABLE `cash_desks` (
  `cash_desk_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `name` varchar(50) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cash_funds`
--

CREATE TABLE `cash_funds` (
  `cash_fund_id` int(11) NOT NULL,
  `cash_register_id` int(11) NOT NULL,
  `sub_cash_register_id` int(11) DEFAULT NULL,
  `user_id` int(11) NOT NULL,
  `initial_amount` int(11) NOT NULL DEFAULT 0,
  `expected_amount` int(11) NOT NULL DEFAULT 0,
  `actual_amount` int(11) DEFAULT NULL,
  `opened` tinyint(1) NOT NULL DEFAULT 1,
  `closed` tinyint(1) NOT NULL DEFAULT 0,
  `start_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `end_date` timestamp NULL DEFAULT NULL,
  `closed_by` int(11) DEFAULT NULL,
  `closure_comment` varchar(255) DEFAULT NULL,
  `last_assignment_reason` varchar(50) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cash_registers`
--

CREATE TABLE `cash_registers` (
  `cash_register_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `cash_desk_id` int(11) NOT NULL,
  `device_id` varchar(50) NOT NULL,
  `user_id` varchar(64) NOT NULL,
  `cash_fund` int(11) NOT NULL COMMENT 'in cents',
  `final_cash_fund` int(11) DEFAULT 0,
  `start_date` timestamp NOT NULL,
  `end_date` timestamp NULL DEFAULT NULL,
  `closed` tinyint(1) NOT NULL DEFAULT 0,
  `enclosed` tinyint(1) NOT NULL DEFAULT 0,
  `closure_comment` varchar(255) NOT NULL,
  `closed_by` varchar(25) DEFAULT NULL,
  `hash` varchar(64) DEFAULT NULL,
  `signature` text DEFAULT NULL,
  `previous_hash` varchar(64) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cash_registers_custom_items`
--

CREATE TABLE `cash_registers_custom_items` (
  `id` int(11) NOT NULL,
  `label` varchar(25) NOT NULL,
  `amount` int(11) NOT NULL COMMENT 'In cents',
  `merchant_id` varchar(35) DEFAULT NULL,
  `created_by` varchar(35) DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `cash_register_id` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cash_registers_items`
--

CREATE TABLE `cash_registers_items` (
  `id` int(11) NOT NULL,
  `cash_register_id` int(11) NOT NULL,
  `mop` varchar(10) NOT NULL,
  `amount` int(11) NOT NULL COMMENT 'in cents'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cash_reports`
--

CREATE TABLE `cash_reports` (
  `id` int(11) NOT NULL,
  `user_id` int(11) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `cash_desk_id` int(11) NOT NULL,
  `period_from` datetime DEFAULT NULL,
  `period_to` datetime NOT NULL,
  `creation_date` datetime NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `category_discount`
--

CREATE TABLE `category_discount` (
  `id` int(11) NOT NULL,
  `categ_id` int(11) NOT NULL COMMENT 'merchantcategid',
  `merchant_id` int(11) NOT NULL,
  `merchant_discount_id` int(11) NOT NULL,
  `discount_desc` text NOT NULL COMMENT 'Description',
  `discount_order_type` int(11) DEFAULT NULL COMMENT '	0 = IN, 1 = DELIVERY, NULL = all',
  `discount_value` double NOT NULL,
  `discount_unit` varchar(20) NOT NULL COMMENT 'PERCENTAGE | CURRENCY | NEWPRICE',
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `valid_from` timestamp NULL DEFAULT NULL COMMENT 'UTC',
  `valid_to` timestamp NULL DEFAULT NULL COMMENT 'UTC',
  `available_from` time DEFAULT NULL COMMENT 'Merchant TimeZone',
  `available_to` time DEFAULT NULL COMMENT 'Merchant TimeZone',
  `coupon_code` text DEFAULT NULL,
  `min_order_value` int(11) DEFAULT NULL,
  `min_order_unit` varchar(20) DEFAULT NULL COMMENT 'CURRENCY | QUANTITY',
  `max_discount_value` int(11) DEFAULT NULL,
  `max_discount_unit` varchar(20) DEFAULT NULL,
  `discounted_qty` int(11) DEFAULT NULL,
  `is_cumulative` tinyint(1) NOT NULL DEFAULT 0,
  `active` tinyint(1) NOT NULL COMMENT 'Discount activated or not'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `checkout_orderitems`
--

CREATE TABLE `checkout_orderitems` (
  `link_key` varchar(255) NOT NULL,
  `user_code` varchar(255) NOT NULL,
  `order_item_id` int(11) NOT NULL,
  `quantity` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cleaning_executions`
--

CREATE TABLE `cleaning_executions` (
  `id` varchar(64) NOT NULL,
  `session_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `surface_id` varchar(64) NOT NULL,
  `comment` text DEFAULT NULL,
  `photo_url` text DEFAULT NULL,
  `status` varchar(32) NOT NULL DEFAULT 'done',
  `created_by` varchar(64) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cleaning_sessions`
--

CREATE TABLE `cleaning_sessions` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `status` varchar(32) NOT NULL DEFAULT 'done',
  `created_by` varchar(64) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cleaning_surfaces`
--

CREATE TABLE `cleaning_surfaces` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `zone_id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `frequency_unit` enum('day','week','month') NOT NULL,
  `frequency_count` int(11) NOT NULL DEFAULT 1,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `cleaning_zones`
--

CREATE TABLE `cleaning_zones` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `components`
--

CREATE TABLE `components` (
  `component_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `name` varchar(100) CHARACTER SET utf8mb3 COLLATE utf8mb3_bin NOT NULL,
  `component_price` int(11) NOT NULL DEFAULT 0,
  `category_id` varchar(15) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `stock` double NOT NULL DEFAULT 0,
  `safety_stock` float NOT NULL DEFAULT 0 COMMENT 'Minimum stock value before component automatically get set unavailable',
  `safety_triggered` tinyint(1) NOT NULL DEFAULT 1,
  `unit_of_measure` int(11) NOT NULL,
  `purchase_price` int(11) NOT NULL DEFAULT 0 COMMENT 'in cents',
  `purchase_price_quantity` float NOT NULL DEFAULT 1,
  `purchase_unit_id` varchar(35) DEFAULT NULL,
  `auto_update_purchase_info` tinyint(1) NOT NULL DEFAULT 1,
  `status` varchar(20) NOT NULL DEFAULT '1',
  `available` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `conservation_days` int(11) DEFAULT NULL COMMENT 'Durée de conservation après ouverture/déconditionnement, en jours',
  `conservation_type` varchar(20) DEFAULT 'froid' COMMENT 'Type de stockage : froid, congele, sec, ambiant',
  `storage_temp_min` float DEFAULT NULL COMMENT 'Température min de stockage en °C',
  `storage_temp_max` float DEFAULT NULL COMMENT 'Température max de stockage en °C'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `component_category`
--

CREATE TABLE `component_category` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `merchant_categ_id` varchar(11) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` text NOT NULL,
  `categ_order` int(11) NOT NULL,
  `available` tinyint(1) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `configurable_attributes`
--

CREATE TABLE `configurable_attributes` (
  `id` varchar(64) NOT NULL,
  `product_id` int(11) NOT NULL,
  `merchant_id` varchar(20) NOT NULL,
  `brand` varchar(20) NOT NULL DEFAULT 'WELLO_RESTO' COMMENT 'Origine de la création de l''attribut (WELLO_RESTO, UBER_EATS)',
  `attribute_type` varchar(20) NOT NULL DEFAULT 'CHECK' COMMENT 'CHECK | QUANTITY',
  `name` varchar(50) NOT NULL,
  `title` varchar(80) NOT NULL,
  `max_options` int(11) NOT NULL,
  `is_required` tinyint(1) NOT NULL DEFAULT 1,
  `min_options` int(11) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `configurable_attribute_options`
--

CREATE TABLE `configurable_attribute_options` (
  `id` int(11) NOT NULL,
  `configurable_attribute_id` varchar(64) NOT NULL,
  `title` varchar(25) NOT NULL,
  `max_quantity` int(11) NOT NULL DEFAULT 1,
  `extra_price` int(11) NOT NULL DEFAULT 0,
  `image_url` varchar(500) DEFAULT NULL,
  `enabled` int(11) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `consumables`
--

CREATE TABLE `consumables` (
  `consumable_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `name` varchar(50) NOT NULL,
  `unit_of_measure` int(11) NOT NULL,
  `purchase_price` int(11) DEFAULT NULL COMMENT 'in cents',
  `stock` float NOT NULL DEFAULT 0,
  `status` tinyint(1) NOT NULL DEFAULT 1,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `purchase_price_quantity` int(11) DEFAULT NULL COMMENT 'in uom of consumable',
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer`
--

CREATE TABLE `customer` (
  `customer_id` int(11) NOT NULL,
  `customer_brand` varchar(20) NOT NULL DEFAULT 'WELLO_RESTO',
  `customer_brand_id` varchar(50) DEFAULT NULL,
  `merchant_id` int(11) DEFAULT NULL,
  `customer_name` varchar(50) DEFAULT NULL,
  `customer_first_name` varchar(50) DEFAULT NULL,
  `customer_last_name` varchar(50) DEFAULT NULL,
  `customer_code` varchar(4) DEFAULT NULL,
  `customer_tel` varchar(20) DEFAULT NULL,
  `is_migrated` tinyint(1) DEFAULT 0,
  `customer_temporary_phone` varchar(20) DEFAULT NULL,
  `customer_temporary_phone_code` varchar(20) DEFAULT NULL,
  `customer_email` varchar(255) DEFAULT NULL,
  `customer_address` varchar(255) DEFAULT NULL,
  `customer_floor_number` varchar(11) DEFAULT NULL,
  `customer_door_number` varchar(25) DEFAULT NULL,
  `customer_additional_address` varchar(255) DEFAULT NULL,
  `customer_business_name` varchar(50) DEFAULT NULL,
  `customer_birthdate` date DEFAULT NULL,
  `customer_additional_info` varchar(255) DEFAULT NULL,
  `customer_temporary_address` varchar(255) DEFAULT NULL,
  `customer_temporary_lat` double DEFAULT NULL,
  `customer_temporary_lng` double DEFAULT NULL,
  `customer_temporary_floor_number` int(11) DEFAULT NULL,
  `customer_temporary_door_number` varchar(25) DEFAULT NULL,
  `customer_temporary_additional_address` varchar(255) DEFAULT NULL,
  `customer_total_spent` int(11) NOT NULL DEFAULT 0,
  `customer_google_place_id` varchar(255) DEFAULT NULL,
  `customer_lat` double DEFAULT NULL,
  `customer_lng` double DEFAULT NULL,
  `customer_nb_orders` int(11) NOT NULL DEFAULT 0,
  `customer_nb_bookings` int(11) NOT NULL DEFAULT 0,
  `customer_zone_code` varchar(4) DEFAULT NULL,
  `customer_zone_updated_at` timestamp NULL DEFAULT NULL,
  `last_order_date` timestamp NULL DEFAULT NULL,
  `last_advertisement_date` timestamp NULL DEFAULT NULL,
  `loyalty_reminder_count` int(11) NOT NULL DEFAULT 0,
  `advertising_consent` tinyint(1) NOT NULL DEFAULT 1,
  `creation_date` datetime NOT NULL DEFAULT current_timestamp(),
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `delivery_notes` text DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `customer_advertisement_emails`
--

CREATE TABLE `customer_advertisement_emails` (
  `id` int(11) NOT NULL,
  `customer_id` varchar(30) NOT NULL,
  `marketing_campaing_id` varchar(20) DEFAULT NULL,
  `reason` varchar(100) NOT NULL,
  `communication_date` timestamp NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer_loyalty_programs`
--

CREATE TABLE `customer_loyalty_programs` (
  `id` varchar(50) NOT NULL,
  `merchant_id` varchar(30) NOT NULL,
  `name` varchar(50) NOT NULL,
  `description` varchar(120) NOT NULL,
  `type` varchar(30) NOT NULL COMMENT 'enum("orders_count", "total_spent", "products_count")',
  `target_value` int(11) NOT NULL,
  `target_order_type` varchar(100) NOT NULL DEFAULT 'IN TAKE_AWAY DELIVERY',
  `reward_type` varchar(30) NOT NULL COMMENT '	enum("fixed_discount", "percent_discount", "free_product")',
  `reward_value` int(11) NOT NULL,
  `rewards_order_type` varchar(100) NOT NULL DEFAULT 'IN TAKE_AWAY DELIVERY',
  `min_order_value` int(11) NOT NULL DEFAULT 500 COMMENT 'in cents',
  `max_discount_value` int(11) DEFAULT NULL COMMENT 'in cents',
  `max_rewards_per_order` int(11) NOT NULL DEFAULT 1,
  `available` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer_loyalty_program_reward_products`
--

CREATE TABLE `customer_loyalty_program_reward_products` (
  `id` varchar(50) NOT NULL,
  `product_id` varchar(50) NOT NULL,
  `loyalty_program_id` varchar(50) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer_loyalty_program_target_products`
--

CREATE TABLE `customer_loyalty_program_target_products` (
  `id` varchar(50) NOT NULL,
  `product_id` varchar(50) NOT NULL,
  `loyalty_program_id` varchar(50) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer_loyalty_progress`
--

CREATE TABLE `customer_loyalty_progress` (
  `id` varchar(50) NOT NULL,
  `customer_id` varchar(30) NOT NULL,
  `loyalty_program_id` varchar(30) NOT NULL,
  `current_value` int(11) NOT NULL,
  `last_update` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer_loyalty_progress_order`
--

CREATE TABLE `customer_loyalty_progress_order` (
  `id` int(11) NOT NULL,
  `loyalty_program_id` varchar(30) NOT NULL,
  `progress_id` varchar(30) NOT NULL,
  `order_id` varchar(30) NOT NULL,
  `increment_value` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `customer_rewards`
--

CREATE TABLE `customer_rewards` (
  `reward_id` int(11) NOT NULL,
  `customer_id` varchar(30) NOT NULL,
  `loyalty_program_id` varchar(30) NOT NULL,
  `reward_type` varchar(30) NOT NULL,
  `reward_order_type` varchar(100) NOT NULL DEFAULT 'IN TAKE_AWAY DELIVERY',
  `reward_value` int(11) NOT NULL DEFAULT 0,
  `is_used` tinyint(1) NOT NULL DEFAULT 0,
  `issue_date` timestamp NULL DEFAULT NULL,
  `usage_date` timestamp NULL DEFAULT NULL,
  `used_on_order_id` varchar(20) DEFAULT NULL,
  `creation_date` timestamp NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `delays`
--

CREATE TABLE `delays` (
  `id` int(11) NOT NULL COMMENT 'Do not delete records, place then as disabled instead',
  `description` varchar(15) NOT NULL,
  `short_description` varchar(10) NOT NULL,
  `duration` int(11) NOT NULL COMMENT 'in seconds',
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '	Do not delete records, place then as disabled instead'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `deletion_reasons`
--

CREATE TABLE `deletion_reasons` (
  `deletion_reason_id` int(11) NOT NULL,
  `deletion_reason_type` varchar(30) DEFAULT NULL,
  `deletion_reason_object` varchar(30) NOT NULL,
  `deletion_reason_desc` varchar(255) NOT NULL,
  `requires_comment` tinyint(1) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `delivery_position`
--

CREATE TABLE `delivery_position` (
  `id` bigint(20) UNSIGNED NOT NULL,
  `user_id` varchar(64) NOT NULL,
  `delivery_session_id` int(10) UNSIGNED NOT NULL,
  `lat` decimal(10,7) NOT NULL,
  `lng` decimal(10,7) NOT NULL,
  `heading` float DEFAULT NULL,
  `accuracy` float DEFAULT NULL,
  `speed` float DEFAULT NULL,
  `recorded_at` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `delivery_session`
--

CREATE TABLE `delivery_session` (
  `id` int(11) NOT NULL,
  `user_id` varchar(64) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `start_date` datetime NOT NULL COMMENT 'UTC',
  `end_date` timestamp NULL DEFAULT NULL,
  `distance` int(11) NOT NULL DEFAULT 0 COMMENT 'in meters',
  `duration` int(11) NOT NULL DEFAULT 0 COMMENT 'in seconds',
  `status` varchar(25) NOT NULL DEFAULT '1',
  `current_order_id` int(10) UNSIGNED DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `delivery_session_order`
--

CREATE TABLE `delivery_session_order` (
  `delivery_session_id` int(11) NOT NULL,
  `order_id` int(11) NOT NULL,
  `priority` int(11) NOT NULL,
  `status` varchar(20) NOT NULL DEFAULT 'pending',
  `arrived_at` datetime DEFAULT NULL,
  `delivered_at` datetime DEFAULT NULL,
  `failed_at` datetime DEFAULT NULL,
  `canceled_at` datetime DEFAULT NULL,
  `fail_reason` varchar(255) DEFAULT NULL,
  `deletion_reason_id` varchar(20) DEFAULT NULL,
  `deletion_comment` varchar(255) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `device_link`
--

CREATE TABLE `device_link` (
  `device_id` varchar(50) NOT NULL,
  `user_id` varchar(20) NOT NULL,
  `on_behalf_of` varchar(50) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `discounts`
--

CREATE TABLE `discounts` (
  `discount_id` varchar(50) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `discount_name` varchar(50) NOT NULL,
  `discount_desc` varchar(100) NOT NULL,
  `prefered_order` int(11) NOT NULL DEFAULT 0,
  `discount_code` varchar(20) DEFAULT NULL,
  `discount_order_type` varchar(40) DEFAULT NULL COMMENT '0 = IN, 1 = DELIVERY, NULL = all',
  `discount_value` int(11) NOT NULL DEFAULT 0,
  `discount_unit` varchar(20) NOT NULL COMMENT '	PERCENTAGE | CURRENCY | NEWPRICE	',
  `valid_from` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp() COMMENT 'UTC',
  `valid_to` timestamp NULL DEFAULT NULL COMMENT 'UTC',
  `min_order_value` double NOT NULL DEFAULT 0,
  `min_order_unit` varchar(20) DEFAULT NULL COMMENT '	CURRENCY | QUANTITY',
  `max_discount_value` double DEFAULT NULL,
  `max_discount_unit` varchar(20) DEFAULT NULL COMMENT '	CURRENCY | QUANTITY',
  `discounted_quantity` int(11) NOT NULL,
  `is_cumulative` tinyint(1) NOT NULL,
  `is_time_limited` tinyint(1) NOT NULL COMMENT 'Requires discount_shedules ?',
  `available` tinyint(1) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 when deleted',
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `discounts_products`
--

CREATE TABLE `discounts_products` (
  `id` int(11) NOT NULL,
  `discount_id` varchar(50) NOT NULL,
  `product_id` int(11) NOT NULL,
  `new_price` int(11) DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 when deleted'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `discounts_products_options`
--

CREATE TABLE `discounts_products_options` (
  `discount_id` varchar(20) NOT NULL,
  `product_id` varchar(20) NOT NULL,
  `option_id` varchar(20) NOT NULL,
  `new_price` int(11) DEFAULT NULL,
  `is_option_mandatory` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `discounts_schedules`
--

CREATE TABLE `discounts_schedules` (
  `schedule_id` int(11) NOT NULL,
  `discount_id` varchar(50) NOT NULL,
  `day_of_week` int(11) NOT NULL COMMENT 'lundi = 1',
  `available_from` time NOT NULL COMMENT 'UTC time',
  `available_to` time NOT NULL COMMENT 'UTC time',
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 when deleted'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `employees`
--

CREATE TABLE `employees` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `user_id` varchar(64) DEFAULT NULL,
  `member_id` bigint(20) DEFAULT NULL,
  `first_name` varchar(150) NOT NULL,
  `last_name` varchar(150) NOT NULL,
  `position_id` varchar(64) NOT NULL,
  `position_note` text DEFAULT NULL,
  `job_title` varchar(150) DEFAULT NULL,
  `email` varchar(255) DEFAULT NULL,
  `phone` varchar(64) DEFAULT NULL,
  `role` enum('employee','manager','admin') NOT NULL DEFAULT 'employee',
  `contract_type_code` varchar(32) NOT NULL,
  `contract_start_date` date DEFAULT NULL,
  `contract_end_date` date DEFAULT NULL,
  `probation_end_date` date DEFAULT NULL,
  `last_medical_checkup_date` date DEFAULT NULL,
  `contract_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `max_weekly_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `required_rest_days` int(11) NOT NULL DEFAULT 2,
  `sunday_premium` tinyint(1) NOT NULL DEFAULT 0,
  `night_premium` tinyint(1) NOT NULL DEFAULT 0,
  `hourly_rate` bigint(20) NOT NULL DEFAULT 0,
  `gross_monthly_salary` bigint(20) NOT NULL DEFAULT 0,
  `employer_charges_pct` decimal(5,2) NOT NULL DEFAULT 45.00,
  `transport_cost` bigint(20) NOT NULL DEFAULT 0,
  `birth_date` date DEFAULT NULL,
  `gender` varchar(32) DEFAULT NULL,
  `nationality` varchar(80) DEFAULT NULL,
  `address` varchar(255) DEFAULT NULL,
  `hr_comment` text DEFAULT NULL,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `employee_documents`
--

CREATE TABLE `employee_documents` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `employee_id` varchar(64) NOT NULL,
  `document_type` varchar(32) NOT NULL COMMENT 'contract id medical other',
  `name` varchar(255) NOT NULL,
  `file_key` varchar(512) NOT NULL,
  `content_type` varchar(120) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `employment_agreement`
--

CREATE TABLE `employment_agreement` (
  `merchant_id` int(11) NOT NULL,
  `weekly_limit` int(11) NOT NULL DEFAULT 2100 COMMENT 'in minuts',
  `monthly_limit` int(11) NOT NULL DEFAULT 9100 COMMENT 'in minuts'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `employment_contract`
--

CREATE TABLE `employment_contract` (
  `contract_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `user_id` int(11) NOT NULL,
  `hourly_rate` float NOT NULL,
  `schedule` int(11) NOT NULL COMMENT 'nombre d''heures à travailler, par mois par défaut mais le système peut être accepté pour accepter par mois et par semaine plus tard',
  `creation_date` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `expiration_dates`
--

CREATE TABLE `expiration_dates` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `component_id` int(11) NOT NULL,
  `comment` varchar(150) DEFAULT NULL,
  `purchased_component_id` int(11) DEFAULT NULL,
  `expiration_date` date NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '0 when deleted'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `external_tokens`
--

CREATE TABLE `external_tokens` (
  `token_type` varchar(30) NOT NULL,
  `access_token` longtext NOT NULL,
  `expires_at` timestamp NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `extra`
--

CREATE TABLE `extra` (
  `id` int(11) NOT NULL,
  `order_item_id` int(11) DEFAULT NULL,
  `order_id` int(11) NOT NULL,
  `component_id` int(11) NOT NULL,
  `product_id` int(11) NOT NULL,
  `quantity` int(11) NOT NULL DEFAULT 1,
  `price` int(11) NOT NULL COMMENT 'in cents',
  `merchant_id` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `firebase_fcm_access_token`
--

CREATE TABLE `firebase_fcm_access_token` (
  `id` int(11) NOT NULL,
  `access_token` longtext NOT NULL,
  `expiration_date` datetime NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `floors`
--

CREATE TABLE `floors` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `name` varchar(50) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `floor_areas`
--

CREATE TABLE `floor_areas` (
  `id` int(11) NOT NULL,
  `floor_id` int(11) NOT NULL,
  `name` varchar(50) NOT NULL,
  `stroke_color` varchar(11) NOT NULL,
  `color` varchar(11) NOT NULL,
  `x` float NOT NULL,
  `y` float NOT NULL,
  `points` longtext NOT NULL,
  `angle` float NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `floor_obstacles`
--

CREATE TABLE `floor_obstacles` (
  `id` varchar(64) NOT NULL,
  `floor_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `type` enum('wall','bar','stairs','door') NOT NULL,
  `x` float NOT NULL DEFAULT 0,
  `y` float NOT NULL DEFAULT 0,
  `width` float NOT NULL DEFAULT 60,
  `height` float NOT NULL DEFAULT 20,
  `angle` float NOT NULL DEFAULT 0,
  `direction` float DEFAULT NULL COMMENT 'Portes uniquement : angle d ouverture de l arc (degrés)',
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

-- --------------------------------------------------------

--
-- Structure de la table `goods_receipts`
--

CREATE TABLE `goods_receipts` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `supplier` varchar(255) NOT NULL,
  `product_type` varchar(50) NOT NULL,
  `batch_number` varchar(120) NOT NULL,
  `product_temp` decimal(5,2) NOT NULL,
  `control_sample` varchar(255) DEFAULT NULL,
  `quantities_verified` tinyint(1) NOT NULL DEFAULT 0,
  `non_conformities` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`non_conformities`)),
  `comment` text DEFAULT NULL,
  `invoice_url` varchar(512) DEFAULT NULL,
  `created_by` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `haccp_corrective_actions`
--

CREATE TABLE `haccp_corrective_actions` (
  `id` varchar(64) NOT NULL,
  `code` varchar(64) NOT NULL,
  `label` varchar(120) NOT NULL,
  `description` text DEFAULT NULL,
  `severity_scope` varchar(32) DEFAULT NULL,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `haccp_settings`
--

CREATE TABLE `haccp_settings` (
  `merchant_id` varchar(64) NOT NULL,
  `temp_entry_required` tinyint(1) NOT NULL DEFAULT 0,
  `temp_corrective_actions` tinyint(1) NOT NULL DEFAULT 0,
  `temp_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `temp_failure_photo_required` tinyint(1) NOT NULL DEFAULT 0,
  `traceability_product_name` tinyint(1) NOT NULL DEFAULT 0,
  `traceability_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `cleaning_photo` tinyint(1) NOT NULL DEFAULT 0,
  `cleaning_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `reception_other_products` tinyint(1) NOT NULL DEFAULT 0,
  `reception_control_sample` tinyint(1) NOT NULL DEFAULT 0,
  `reception_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `reception_photo` tinyint(1) NOT NULL DEFAULT 0,
  `reception_non_conformities` tinyint(1) NOT NULL DEFAULT 0,
  `oils_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `oils_polar_compound_rate` tinyint(1) NOT NULL DEFAULT 0,
  `oils_photo` tinyint(1) NOT NULL DEFAULT 0,
  `production_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `production_traceability` tinyint(1) NOT NULL DEFAULT 0,
  `cooling_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `freezing_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `reheating_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `holding_block_past_dates` tinyint(1) NOT NULL DEFAULT 0,
  `holding_corrective_actions` tinyint(1) NOT NULL DEFAULT 0,
  `notif_authorization` tinyint(1) NOT NULL DEFAULT 0,
  `notif_security` tinyint(1) NOT NULL DEFAULT 0,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `holiday_calendar`
--

CREATE TABLE `holiday_calendar` (
  `id` varchar(64) NOT NULL,
  `country_code` char(2) NOT NULL,
  `holiday_date` date NOT NULL,
  `label` varchar(150) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `hours_amendments`
--

CREATE TABLE `hours_amendments` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `employee_id` varchar(64) NOT NULL,
  `type` enum('permanent','temporary') NOT NULL DEFAULT 'permanent',
  `start_date` date NOT NULL,
  `end_date` date DEFAULT NULL,
  `new_hours_volume` decimal(5,2) NOT NULL,
  `created_by` varchar(255) DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `hours_of_operation`
--

CREATE TABLE `hours_of_operation` (
  `id` varchar(64) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `day_of_week_from` int(11) NOT NULL COMMENT '	1 => Monday, 7 => Sunday	',
  `hour_from` time(6) NOT NULL,
  `day_of_week_to` int(11) NOT NULL,
  `hour_to` time(6) NOT NULL,
  `first_booking_time` time DEFAULT NULL,
  `last_booking_time` time DEFAULT NULL,
  `booking_capacity` int(11) NOT NULL DEFAULT 0,
  `valid_from` datetime(6) DEFAULT NULL ON UPDATE current_timestamp(6),
  `valid_to` datetime(6) DEFAULT NULL,
  `creation_date` datetime(6) NOT NULL DEFAULT current_timestamp(6),
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_deliveroo`
--

CREATE TABLE `integration_deliveroo` (
  `merchant_id` int(11) NOT NULL,
  `location_id` varchar(20) NOT NULL,
  `brand_id` varchar(150) NOT NULL,
  `auto_accept_orders` tinyint(1) NOT NULL DEFAULT 0,
  `commission_rate` int(11) NOT NULL DEFAULT 0 COMMENT 'Commission rate in percent',
  `preparation_time_minutes` int(11) NOT NULL DEFAULT 60,
  `last_sync` datetime DEFAULT NULL COMMENT 'Last successful menu upload timestamp (UTC)',
  `synced_items` int(11) NOT NULL DEFAULT 0 COMMENT 'Number of products in last published menu',
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_deliveroo_attributes_mapping`
--

CREATE TABLE `integration_deliveroo_attributes_mapping` (
  `id` int(11) NOT NULL,
  `merchant_id` varchar(20) NOT NULL,
  `configurable_attribute_id` int(11) NOT NULL,
  `modifier_group_pos_id` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_deliveroo_components_mapping`
--

CREATE TABLE `integration_deliveroo_components_mapping` (
  `id` int(11) NOT NULL,
  `item_id` varchar(50) NOT NULL,
  `component_id` varchar(50) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `deletion_date` timestamp NULL DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_deliveroo_options_mapping`
--

CREATE TABLE `integration_deliveroo_options_mapping` (
  `id` int(11) NOT NULL,
  `merchant_id` varchar(20) NOT NULL,
  `configurable_attribute_option_id` int(11) NOT NULL,
  `item_id` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_deliveroo_products_mapping`
--

CREATE TABLE `integration_deliveroo_products_mapping` (
  `item_id` varchar(50) NOT NULL,
  `item_name` varchar(50) NOT NULL,
  `product_id` varchar(50) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `deletion_date` timestamp NULL DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_direct`
--

CREATE TABLE `integration_uber_direct` (
  `merchant_id` int(11) NOT NULL,
  `bearer_token` longtext DEFAULT NULL,
  `customer_id` longtext NOT NULL,
  `client_id` longtext NOT NULL,
  `client_secret` longtext NOT NULL,
  `external_store_id` varchar(50) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_eats`
--

CREATE TABLE `integration_uber_eats` (
  `merchant_id` int(11) NOT NULL,
  `store_id` varchar(150) NOT NULL,
  `pos_provisioning_token` longtext DEFAULT NULL,
  `pos_provisionning_refresh_token` longtext NOT NULL,
  `pos_provisionning_token_expiration_date` datetime NOT NULL,
  `estimated_preparation_time` varchar(10) NOT NULL DEFAULT '30',
  `last_estimated_preparation_time` varchar(10) NOT NULL DEFAULT '30',
  `delay_until` timestamp NULL DEFAULT NULL COMMENT 'UTC',
  `delay_duration` int(11) NOT NULL,
  `closed_until` timestamp NULL DEFAULT NULL,
  `auto_accept_orders` tinyint(1) NOT NULL,
  `bearer_token` longtext DEFAULT NULL,
  `refresh_token` longtext DEFAULT NULL,
  `bearer_token_expiration_date` timestamp NULL DEFAULT NULL,
  `expires_at` timestamp NULL DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 0,
  `unlink_date` timestamp NULL DEFAULT NULL,
  `commission_rate` int(11) NOT NULL DEFAULT 0 COMMENT 'Commission rate in percent',
  `last_sync` datetime DEFAULT NULL COMMENT 'Last successful menu/product sync timestamp (UTC)',
  `synced_items` int(11) NOT NULL DEFAULT 0 COMMENT 'Number of products currently mapped to Uber Eats'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_eats_attributes_mapping`
--

CREATE TABLE `integration_uber_eats_attributes_mapping` (
  `id` int(11) NOT NULL,
  `merchant_id` varchar(20) NOT NULL,
  `configurable_attribute_id` int(11) NOT NULL,
  `modifier_group_id` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_eats_components_mapping`
--

CREATE TABLE `integration_uber_eats_components_mapping` (
  `id` int(11) NOT NULL,
  `item_id` varchar(50) NOT NULL,
  `component_id` varchar(50) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `deletion_date` timestamp NULL DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_eats_options_mapping`
--

CREATE TABLE `integration_uber_eats_options_mapping` (
  `id` int(11) NOT NULL,
  `merchant_id` varchar(20) NOT NULL,
  `configurable_attribute_option_id` int(11) NOT NULL,
  `item_id` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_eats_products_mapping`
--

CREATE TABLE `integration_uber_eats_products_mapping` (
  `id` int(11) NOT NULL,
  `item_id` varchar(50) NOT NULL,
  `product_id` varchar(50) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `deletion_date` timestamp NULL DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `integration_uber_eats_reports`
--

CREATE TABLE `integration_uber_eats_reports` (
  `workflow_id` varchar(255) NOT NULL COMMENT 'job_id in webhook',
  `report_type` varchar(60) NOT NULL,
  `store_id` varchar(150) NOT NULL,
  `start_date` date NOT NULL,
  `end_date` date NOT NULL,
  `download_url` longtext DEFAULT NULL,
  `creation_date` timestamp NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `invoices`
--

CREATE TABLE `invoices` (
  `id` int(11) NOT NULL,
  `invoice_id` varchar(50) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `order_id` int(11) NOT NULL,
  `customer_email` varchar(100) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `kiosks`
--

CREATE TABLE `kiosks` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `name` varchar(100) NOT NULL,
  `location_id` varchar(64) DEFAULT NULL,
  `status` enum('pending','active','inactive','revoked') NOT NULL DEFAULT 'pending',
  `app_version` varchar(20) DEFAULT NULL,
  `hardware_model` varchar(100) DEFAULT NULL,
  `admin_pin_encrypted` varbinary(255) DEFAULT NULL,
  `os_version` varchar(50) DEFAULT NULL,
  `last_heartbeat_at` datetime DEFAULT NULL,
  `last_ip` varchar(45) DEFAULT NULL,
  `last_error` text DEFAULT NULL,
  `last_error_at` datetime DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime DEFAULT NULL ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `kiosk_device_tokens`
--

CREATE TABLE `kiosk_device_tokens` (
  `id` varchar(64) NOT NULL,
  `new_id` varchar(64) DEFAULT NULL,
  `kiosk_id` varchar(64) NOT NULL,
  `new_kiosk_id` varchar(64) DEFAULT NULL,
  `token_hash` varchar(64) NOT NULL,
  `expires_at` datetime NOT NULL,
  `revoked_at` datetime DEFAULT NULL,
  `last_used_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `kiosk_enrollment_codes`
--

CREATE TABLE `kiosk_enrollment_codes` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `code_hash` varchar(64) NOT NULL,
  `kiosk_id` varchar(64) DEFAULT NULL,
  `expires_at` datetime NOT NULL,
  `used_at` datetime DEFAULT NULL,
  `created_by_user_id` varchar(64) DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `kiosk_settings`
--

CREATE TABLE `kiosk_settings` (
  `merchant_id` varchar(64) NOT NULL,
  `fulfillment_dine_in` tinyint(1) NOT NULL DEFAULT 1,
  `fulfillment_take_away` tinyint(1) NOT NULL DEFAULT 1,
  `force_fulfillment_type` varchar(20) DEFAULT NULL,
  `pager_number_required` tinyint(1) NOT NULL DEFAULT 0,
  `show_allergens` tinyint(1) NOT NULL DEFAULT 1,
  `inactivity_timeout_sec` int(11) NOT NULL DEFAULT 90,
  `upsell_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `pay_at_counter_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `variable_fees` decimal(10,4) NOT NULL DEFAULT 0.0070 COMMENT 'Frais variables plateforme (ex: 0.007 = 0.7%)',
  `fixed_fees` int(11) NOT NULL DEFAULT 15 COMMENT 'Frais fixes plateforme en centimes (ex: 15 = 0.15€)',
  `card_payment_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `logo_url` varchar(500) DEFAULT NULL,
  `idle_image_url` varchar(500) DEFAULT NULL,
  `idle_video_url` varchar(500) DEFAULT NULL,
  `primary_color` varchar(7) DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime DEFAULT NULL ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `labels`
--

CREATE TABLE `labels` (
  `id` int(11) NOT NULL,
  `label_value` varchar(20) NOT NULL,
  `label_type` varchar(20) NOT NULL,
  `lang` varchar(5) NOT NULL,
  `label` varchar(150) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `labor_rules`
--

CREATE TABLE `labor_rules` (
  `country_code` char(2) NOT NULL,
  `label` varchar(120) NOT NULL,
  `min_daily_rest_hours` decimal(4,2) NOT NULL DEFAULT 11.00,
  `min_break_minutes` int(11) NOT NULL DEFAULT 45,
  `night_shift_start` time NOT NULL DEFAULT '22:00:00',
  `night_shift_end` time NOT NULL DEFAULT '06:00:00',
  `night_shift_multiplier` decimal(4,2) NOT NULL DEFAULT 1.25,
  `holiday_multiplier` decimal(4,2) NOT NULL DEFAULT 2.00,
  `max_weekly_hours` decimal(5,2) NOT NULL DEFAULT 48.00,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `locations`
--

CREATE TABLE `locations` (
  `location_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `location_name` varchar(20) NOT NULL COMMENT 'Nom de la table',
  `location_desc` varchar(20) DEFAULT NULL COMMENT 'Description (nb de eprsonnes, handicapés, debout..)',
  `location_order` int(11) NOT NULL DEFAULT 0,
  `seats` int(11) NOT NULL,
  `floor_id` int(11) DEFAULT NULL,
  `shape` varchar(20) DEFAULT NULL,
  `x` float DEFAULT NULL,
  `current_x` float DEFAULT NULL,
  `y` float DEFAULT NULL,
  `current_y` float DEFAULT NULL,
  `width` float DEFAULT NULL,
  `current_width` float DEFAULT NULL,
  `height` float DEFAULT NULL,
  `current_height` float DEFAULT NULL,
  `angle` float DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `attributes` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL COMMENT 'Attributs booléens : pmr, terrace, vip, window' CHECK (json_valid(`attributes`))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `marketing_categories`
--

CREATE TABLE `marketing_categories` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `name` varchar(191) NOT NULL,
  `display_order` int(11) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `available` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` timestamp NOT NULL DEFAULT current_timestamp(),
  `updated_at` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `merchant`
--

CREATE TABLE `merchant` (
  `id` int(11) NOT NULL,
  `brand_id` varchar(35) DEFAULT NULL,
  `fullName` varchar(50) NOT NULL,
  `address` text NOT NULL,
  `street_number` varchar(25) NOT NULL,
  `street` varchar(255) NOT NULL,
  `zip_code` varchar(6) NOT NULL,
  `city` varchar(255) NOT NULL,
  `country` varchar(255) NOT NULL DEFAULT 'France',
  `lat` double DEFAULT 0,
  `lng` double DEFAULT 0,
  `timezone` varchar(50) NOT NULL DEFAULT 'Europe/Paris',
  `logo` longtext DEFAULT NULL,
  `logo_url` longtext DEFAULT NULL,
  `handicap_access` tinyint(1) NOT NULL DEFAULT 0,
  `SIRET` varchar(50) NOT NULL,
  `vat_number` varchar(50) DEFAULT NULL,
  `web_site` varchar(100) NOT NULL,
  `email` varchar(100) DEFAULT NULL,
  `merchantTel` varchar(15) NOT NULL,
  `token` varchar(20) NOT NULL,
  `creation_date` datetime NOT NULL DEFAULT current_timestamp(),
  `is_active` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `merchant_code`
--

CREATE TABLE `merchant_code` (
  `code_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `code` varchar(6) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `merchant_google_maps_monthly`
--

CREATE TABLE `merchant_google_maps_monthly` (
  `merchant_id` varchar(64) NOT NULL,
  `month` date NOT NULL,
  `call_count` int(11) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `merchant_marketing_settings`
--

CREATE TABLE `merchant_marketing_settings` (
  `id` bigint(20) UNSIGNED NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `sms_enabled` tinyint(1) DEFAULT 1,
  `sms_unit_price` int(11) NOT NULL DEFAULT 7,
  `email_enabled` tinyint(1) DEFAULT 1,
  `sms_sender_name` varchar(20) DEFAULT NULL,
  `email_sender_name` varchar(100) DEFAULT NULL,
  `sms_template` text DEFAULT NULL,
  `email_template` text DEFAULT NULL,
  `tracking_template` text DEFAULT 'Votre commande #{order_id} est en cours de livraison. Suivez-la ici : {tracking_url}',
  `messaggio_login` varchar(255) NOT NULL DEFAULT 'd46j1e3un6tc738rv3tg',
  `messaggio_from` varchar(255) NOT NULL DEFAULT 'd46j39jun6tc738rv3vg',
  `created_at` timestamp NULL DEFAULT utc_timestamp(),
  `updated_at` timestamp NULL DEFAULT utc_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `merchant_parameters`
--

CREATE TABLE `merchant_parameters` (
  `merchant_id` int(11) NOT NULL,
  `manage_on_site` tinyint(1) NOT NULL DEFAULT 1,
  `manage_take_away` tinyint(1) NOT NULL DEFAULT 1,
  `manage_delivery` tinyint(1) NOT NULL DEFAULT 1,
  `last_menu_update` timestamp NOT NULL,
  `concurrent_preparation_capacity` int(11) NOT NULL DEFAULT 1 COMMENT 'Exemples par type d''établissement\r\nLe goulot d''étranglement varie énormément d''un type de cuisine à l''autre.\r\n\r\n1. Pizzeria ?\r\nGoulot d''étranglement : La taille du four.\r\n\r\nExemple : Un restaurant a un four à convoyeur qui peut cuire 4 pizzas côte à côte. Peu importe s''il y a 1 ou 3 cuisiniers, le débit est limité par le four.\r\n\r\nconcurrent_preparation_capacity = 4\r\n\r\n2. Sandwicherie / Tacos / Kebab ?\r\nGoulot d''étranglement : Le nombre d''employés au poste d''assemblage.\r\n\r\nExemple : Deux employés préparent les sandwichs en parallèle. Un troisième employé à la caisse ne compte pas dans la capacité de production.\r\n\r\nconcurrent_preparation_capacity = 2\r\n\r\n3. Restaurant traditionnel / Grill ?\r\nGoulot d''étranglement : La surface de cuisson principale (plancha, grill).\r\n\r\nExemple : Une plancha peut accueillir 6 steaks et 4 accompagnements en même temps. On peut considérer que chaque plat principal ou accompagnement est un "article".\r\n\r\nconcurrent_preparation_capacity = 10 (environ)\r\n\r\n4. Bar / Café ☕\r\nGoulot d''étran',
  `delivery_fees` int(11) NOT NULL DEFAULT 0,
  `delivery_fees_limit` int(11) NOT NULL DEFAULT 0,
  `delivery_distance_limit` int(11) NOT NULL DEFAULT 5000,
  `minimum_cart_for_delivery_order` int(11) NOT NULL DEFAULT 1000,
  `kitchen_show_only_paid` tinyint(1) NOT NULL DEFAULT 0,
  `kitchen_show_pending_approval` tinyint(1) NOT NULL DEFAULT 0,
  `kitchen_distribution_mode` varchar(30) NOT NULL DEFAULT 'READY_FOR_DISTRIBUTION' COMMENT 'READY_FOR_DISTRIBUTION\r\nDISTRIBUTE',
  `production_display_mode` varchar(20) NOT NULL DEFAULT 'CLASSIC' COMMENT 'CLASSIC, PRODUCT_FOCUS',
  `preparation_time_mode` varchar(20) NOT NULL DEFAULT 'AUTO' COMMENT 'AUTO | MANUAL',
  `preparation_time` int(11) NOT NULL DEFAULT 15 COMMENT 'for MANUAL, in minuts',
  `minimum_preparation_time` int(11) NOT NULL DEFAULT 300 COMMENT 'in seconds',
  `maximum_preparation_time` int(11) NOT NULL DEFAULT 3600 COMMENT 'in seconds',
  `disable_components_under_safety_stock` tinyint(1) NOT NULL DEFAULT 0,
  `service_required_for_ordering` tinyint(1) NOT NULL DEFAULT 0,
  `cash_register_required_for_ordering` tinyint(1) NOT NULL DEFAULT 1,
  `waiter_app_can_cash_in` tinyint(1) NOT NULL DEFAULT 1,
  `waiter_app_can_clock_in` tinyint(1) NOT NULL DEFAULT 0,
  `auto_complete_orders` tinyint(1) NOT NULL DEFAULT 0,
  `auto_complete_orders_delay` int(11) NOT NULL DEFAULT 10,
  `auto_accept_sno_delivery_orders` tinyint(1) NOT NULL DEFAULT 0,
  `auto_accept_sno_take_away_orders` tinyint(1) NOT NULL DEFAULT 0,
  `automatically_add_customer_rewards` tinyint(1) NOT NULL DEFAULT 1,
  `warning_new_order_not_paid` tinyint(1) NOT NULL DEFAULT 1 COMMENT 'popup that warn that the new order is not paid when creating new order on WR Reception',
  `enable_advance_orders` tinyint(1) NOT NULL DEFAULT 0,
  `advance_order_days` int(11) NOT NULL DEFAULT 3,
  `pager_number_required` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Demande un numéro de bipeur',
  `pos_auto_lock_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `pos_auto_lock_delay_minutes` int(11) NOT NULL DEFAULT 5,
  `pos_upsell_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `customer_form_requirements` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`customer_form_requirements`)),
  `enabled_rating` tinyint(1) NOT NULL DEFAULT 0,
  `currency` varchar(5) NOT NULL DEFAULT 'EUR',
  `is_open` tinyint(1) NOT NULL DEFAULT 0,
  `primary_color` varchar(10) NOT NULL DEFAULT '#212529',
  `text_color_on_primary_color` varchar(10) NOT NULL DEFAULT '#ffffff',
  `zoning_type` varchar(20) DEFAULT NULL,
  `radial_cone_count` int(11) NOT NULL DEFAULT 8,
  `radial_zone_ranges` varchar(20) NOT NULL DEFAULT '0-3,3-5,5-999',
  `grid_cell_size_km` int(11) NOT NULL DEFAULT 2,
  `grid_origin_lat` double DEFAULT NULL,
  `grid_origin_lng` double DEFAULT NULL,
  `cardinal_cone_count` int(11) NOT NULL DEFAULT 4,
  `cardinal_zone_ranges` varchar(30) NOT NULL DEFAULT '0-1,1-3,3-999',
  `enable_upsell` tinyint(1) NOT NULL DEFAULT 0,
  `upsell_max_items` int(11) NOT NULL DEFAULT 3,
  `enable_translation` tinyint(1) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `merchant_sms_monthly`
--

CREATE TABLE `merchant_sms_monthly` (
  `merchant_id` varchar(50) NOT NULL,
  `month` date NOT NULL,
  `sms_count` int(11) NOT NULL DEFAULT 0,
  `total_cost` int(11) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `merchant_translation_languages`
--

CREATE TABLE `merchant_translation_languages` (
  `merchant_id` varchar(64) NOT NULL,
  `lang_code` varchar(5) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `migration_users`
--

CREATE TABLE `migration_users` (
  `_id` varchar(50) NOT NULL,
  `totalPoint` int(11) DEFAULT 0,
  `totalGift` int(11) DEFAULT 0,
  `archived` tinyint(1) DEFAULT 0,
  `email` varchar(100) NOT NULL,
  `password` varchar(255) DEFAULT NULL,
  `phone` varchar(20) DEFAULT NULL,
  `first_name` varchar(100) DEFAULT NULL,
  `last_name` varchar(100) DEFAULT NULL,
  `role` varchar(50) DEFAULT NULL,
  `userIdNotif` varchar(255) DEFAULT NULL,
  `code` varchar(100) DEFAULT NULL,
  `history___id` varchar(24) DEFAULT NULL,
  `history__title` varchar(255) DEFAULT NULL,
  `history__createdAt` datetime DEFAULT NULL,
  `history__updatedAt` datetime DEFAULT NULL,
  `createdAt` datetime DEFAULT current_timestamp(),
  `updatedAt` datetime DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `__v` int(11) DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `notifications`
--

CREATE TABLE `notifications` (
  `notification_id` int(11) NOT NULL,
  `notification_user_id` int(11) NOT NULL,
  `notification_title` varchar(60) NOT NULL,
  `notification_desc` varchar(150) NOT NULL,
  `done` tinyint(1) NOT NULL DEFAULT 0,
  `notification_date` timestamp NOT NULL DEFAULT current_timestamp() COMMENT 'UTC'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `orderitems`
--

CREATE TABLE `orderitems` (
  `order_item_id` int(11) NOT NULL,
  `brand_order_item_id` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL,
  `order_id` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `product_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `quantity` int(11) NOT NULL,
  `paid_quantity` int(11) NOT NULL DEFAULT 0,
  `distributed_quantity` int(11) NOT NULL DEFAULT 0,
  `ready_for_distribution_quantity` int(11) NOT NULL DEFAULT 0,
  `discount_id` int(11) DEFAULT NULL,
  `price` int(11) NOT NULL,
  `base_price` int(11) DEFAULT NULL,
  `isPaid` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Paid article',
  `isDistributed` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Distributed article',
  `production_status` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'TODO',
  `production_status_done_quantity` int(11) NOT NULL DEFAULT 0,
  `delay_id` int(11) DEFAULT 0,
  `is_upsell` tinyint(1) NOT NULL DEFAULT 0,
  `distributed_on` timestamp NULL DEFAULT NULL,
  `ordered_on` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `orders`
--

CREATE TABLE `orders` (
  `order_id` int(11) NOT NULL,
  `public_id` varchar(64) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `customer_id` int(11) DEFAULT NULL,
  `use_customer_temporary_address` tinyint(1) DEFAULT 0,
  `cash_register_id` varchar(11) DEFAULT NULL,
  `kiosk_id` varchar(64) DEFAULT NULL,
  `order_num` int(11) NOT NULL COMMENT 'Numéro de la commande affiché au client et au marchand',
  `brand` varchar(20) NOT NULL DEFAULT 'WELLO_RESTO',
  `brand_order_id` varchar(50) DEFAULT NULL,
  `parent_order_id` varchar(50) DEFAULT NULL COMMENT 'Deliveroo : Previous brand_order_id before remake',
  `brand_order_num` varchar(10) DEFAULT NULL,
  `brand_status` varchar(30) NOT NULL,
  `scheduled` tinyint(1) NOT NULL DEFAULT 0,
  `order_type` varchar(30) DEFAULT NULL,
  `state` varchar(30) NOT NULL DEFAULT 'OPEN',
  `location` text DEFAULT NULL,
  `places_settings` int(11) NOT NULL DEFAULT 0,
  `pager_number` varchar(5) DEFAULT NULL,
  `price` int(11) NOT NULL,
  `dateCall` datetime NOT NULL DEFAULT current_timestamp(),
  `delivery_start` datetime DEFAULT NULL,
  `delivered_on` datetime DEFAULT NULL,
  `TVA` int(11) NOT NULL,
  `HT` int(11) NOT NULL,
  `delivery_fees` int(11) NOT NULL DEFAULT 0 COMMENT 'Cents',
  `comment` text DEFAULT NULL,
  `cutlery_notes` tinyint(1) DEFAULT 0,
  `isDelivery` tinyint(1) DEFAULT 1,
  `merchant_approval` varchar(30) NOT NULL DEFAULT 'ACCEPTED' COMMENT 'validation des commandes en livraison par SNO',
  `status` int(11) DEFAULT 2 COMMENT '-1 deleted\r\n0 finished\r\n1 done and paid\r\n2 pending\r\n3 stripe payment pending',
  `creation_date` datetime NOT NULL DEFAULT current_timestamp(),
  `created_by` varchar(40) NOT NULL COMMENT 'User who created the order',
  `means_of_payement` text DEFAULT NULL,
  `monnaie` float NOT NULL DEFAULT 0,
  `isPaid` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Order is paied',
  `isDistributed` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'All plates are distributed',
  `responsible` int(11) DEFAULT NULL,
  `fulfillment_type` varchar(40) NOT NULL DEFAULT 'DELIVERY_BY_RESTAURANT',
  `estimated_ready` timestamp NULL DEFAULT NULL,
  `deletion_reason_id` varchar(11) DEFAULT NULL,
  `deletion_comment` varchar(255) DEFAULT NULL,
  `last_update` timestamp NOT NULL DEFAULT current_timestamp(),
  `hash` varchar(64) DEFAULT NULL,
  `signature` text DEFAULT NULL,
  `previous_hash` varchar(64) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `order_changes_log`
--

CREATE TABLE `order_changes_log` (
  `id` int(11) NOT NULL,
  `order_id` varchar(25) NOT NULL,
  `changed_by_user_id` varchar(25) NOT NULL,
  `change_type` varchar(50) NOT NULL,
  `change_date` timestamp NOT NULL,
  `field_changed` varchar(50) NOT NULL,
  `old_value` varchar(50) DEFAULT NULL,
  `new_value` varchar(50) NOT NULL,
  `change_reason` varchar(255) DEFAULT NULL,
  `origin` varchar(255) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `order_comments`
--

CREATE TABLE `order_comments` (
  `id` int(11) NOT NULL,
  `order_id` int(11) NOT NULL,
  `order_item_id` int(11) DEFAULT NULL,
  `user_id` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `content` text NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `order_item_configuration`
--

CREATE TABLE `order_item_configuration` (
  `id` int(11) NOT NULL,
  `order_item_id` int(11) NOT NULL,
  `configuration_attribute_id` int(11) NOT NULL,
  `configuration_attribute_option_id` int(11) NOT NULL,
  `quantity` int(11) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `order_location`
--

CREATE TABLE `order_location` (
  `order_id` int(11) NOT NULL,
  `location_id` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `order_ratings`
--

CREATE TABLE `order_ratings` (
  `id` bigint(20) UNSIGNED NOT NULL,
  `order_id` varchar(255) NOT NULL,
  `delivery_rating` tinyint(3) UNSIGNED NOT NULL COMMENT 'Note de 1 à 5 pour la livraison',
  `comment` text DEFAULT NULL COMMENT 'Commentaire textuel de l''utilisateur',
  `created_at` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `packages`
--

CREATE TABLE `packages` (
  `id` int(11) NOT NULL,
  `package_name` varchar(50) NOT NULL,
  `stripe_price_id` varchar(200) NOT NULL,
  `trial_period_days` int(11) NOT NULL DEFAULT 0,
  `stock_management` int(11) NOT NULL DEFAULT 0,
  `hr_management` tinyint(1) NOT NULL DEFAULT 0,
  `planning_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `haccp_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `stock_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `scannorder_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `bookings_enabled` tinyint(1) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `payments`
--

CREATE TABLE `payments` (
  `payment_id` int(11) NOT NULL,
  `cash_register_id` varchar(20) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `user_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `order_id` int(11) NOT NULL,
  `amount` int(11) NOT NULL,
  `mop` varchar(20) NOT NULL COMMENT 'Means of payment | CURRENCY or PERCENTAGE for discounts',
  `fee` int(11) NOT NULL DEFAULT 0,
  `net_amount` int(11) NOT NULL DEFAULT 0,
  `comment` varchar(250) DEFAULT NULL,
  `status_check` varchar(2) DEFAULT NULL COMMENT 'check status for TR payment',
  `hash` varchar(64) DEFAULT NULL,
  `signature` text DEFAULT NULL,
  `previous_hash` varchar(64) DEFAULT NULL,
  `operation_type` varchar(20) NOT NULL DEFAULT 'SALE',
  `payment_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT '1 enabled, 0 disabled'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `pictures`
--

CREATE TABLE `pictures` (
  `picture_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `img` longtext NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planned_shifts`
--

CREATE TABLE `planned_shifts` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `created_by` int(11) NOT NULL,
  `planning_role_id` int(11) NOT NULL,
  `user_id` int(11) NOT NULL,
  `start_date` datetime NOT NULL,
  `end_date` datetime NOT NULL,
  `department_id` int(11) DEFAULT NULL,
  `comment` varchar(50) DEFAULT NULL,
  `enabled` int(11) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_holiday_overrides`
--

CREATE TABLE `planning_holiday_overrides` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `holiday_date` date NOT NULL,
  `label` varchar(150) DEFAULT NULL,
  `is_open` tinyint(1) DEFAULT NULL,
  `count_as_holiday` tinyint(1) DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_leave_requests`
--

CREATE TABLE `planning_leave_requests` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `employee_id` varchar(64) NOT NULL,
  `leave_type` enum('paid','unpaid','sick','other') NOT NULL DEFAULT 'paid',
  `start_date` date NOT NULL,
  `end_date` date NOT NULL,
  `status` enum('pending','approved','rejected','cancelled') NOT NULL DEFAULT 'pending',
  `reason` text DEFAULT NULL,
  `manager_note` text DEFAULT NULL,
  `requested_by_user_id` varchar(64) DEFAULT NULL,
  `processed_by_user_id` varchar(64) DEFAULT NULL,
  `processed_at` datetime DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_positions`
--

CREATE TABLE `planning_positions` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `label` varchar(150) NOT NULL,
  `color` char(7) NOT NULL,
  `sort_order` int(11) NOT NULL DEFAULT 0,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_revenue_forecasts`
--

CREATE TABLE `planning_revenue_forecasts` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `forecast_date` date NOT NULL,
  `amount_ht_cents` bigint(20) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_roles`
--

CREATE TABLE `planning_roles` (
  `role_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `role_name` varchar(50) NOT NULL,
  `role_color` varchar(11) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `creation_date` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_settings`
--

CREATE TABLE `planning_settings` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `labor_country_code` char(2) NOT NULL DEFAULT 'FR',
  `min_daily_rest_hours` decimal(4,2) NOT NULL DEFAULT 11.00,
  `min_break_minutes` int(11) NOT NULL DEFAULT 45,
  `night_shift_start` time NOT NULL DEFAULT '22:00:00',
  `night_shift_end` time NOT NULL DEFAULT '06:00:00',
  `night_shift_multiplier` decimal(4,2) NOT NULL DEFAULT 1.25,
  `holiday_multiplier` decimal(4,2) NOT NULL DEFAULT 2.00,
  `allow_override_warnings` tinyint(1) NOT NULL DEFAULT 1,
  `attendance_source` varchar(32) NOT NULL DEFAULT 'pointage',
  `shift_swap_approval_mode` varchar(25) NOT NULL DEFAULT 'none',
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_shifts`
--

CREATE TABLE `planning_shifts` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `week_id` varchar(64) NOT NULL,
  `employee_id` varchar(64) DEFAULT NULL,
  `position_id` varchar(64) DEFAULT NULL,
  `title` varchar(150) NOT NULL,
  `shift_date` date NOT NULL,
  `start_time` time NOT NULL,
  `end_time` time NOT NULL,
  `break_minutes` int(11) NOT NULL DEFAULT 0,
  `position` varchar(150) DEFAULT NULL,
  `location` varchar(150) DEFAULT NULL,
  `notes` text DEFAULT NULL,
  `status` enum('planned','confirmed','done','cancelled') NOT NULL DEFAULT 'planned',
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_shift_swap_requests`
--

CREATE TABLE `planning_shift_swap_requests` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `requester_employee_id` varchar(64) NOT NULL,
  `requester_shift_id` varchar(64) NOT NULL,
  `target_employee_id` varchar(64) NOT NULL,
  `target_shift_id` varchar(64) NOT NULL,
  `status` enum('pending','approved','rejected','cancelled') NOT NULL DEFAULT 'pending',
  `reason` text DEFAULT NULL,
  `manager_note` text DEFAULT NULL,
  `requested_by_user_id` varchar(64) DEFAULT NULL,
  `processed_by_user_id` varchar(64) DEFAULT NULL,
  `processed_at` datetime DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_shift_templates`
--

CREATE TABLE `planning_shift_templates` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `label` varchar(150) NOT NULL,
  `start_time` time NOT NULL,
  `end_time` time NOT NULL,
  `break_minutes` int(11) NOT NULL DEFAULT 0,
  `position_id` varchar(64) DEFAULT NULL,
  `color` char(7) NOT NULL,
  `sort_order` int(11) NOT NULL DEFAULT 0,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_time_entries`
--

CREATE TABLE `planning_time_entries` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `employee_id` varchar(64) NOT NULL,
  `shift_id` varchar(64) DEFAULT NULL,
  `attendance_source` varchar(32) NOT NULL,
  `clock_in_at` datetime NOT NULL,
  `clock_out_at` datetime DEFAULT NULL,
  `clock_in_note` text DEFAULT NULL,
  `clock_out_note` text DEFAULT NULL,
  `modified_by` varchar(255) DEFAULT NULL,
  `modified_at` datetime DEFAULT NULL,
  `modification_reason` varchar(255) DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_weeks`
--

CREATE TABLE `planning_weeks` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `label` varchar(150) DEFAULT NULL,
  `start_date` date NOT NULL,
  `end_date` date NOT NULL,
  `status` enum('draft','published','locked') NOT NULL DEFAULT 'draft',
  `published_at` datetime DEFAULT NULL,
  `notes` text DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_week_templates`
--

CREATE TABLE `planning_week_templates` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `label` varchar(120) NOT NULL,
  `notes` text DEFAULT NULL,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `planning_week_template_shifts`
--

CREATE TABLE `planning_week_template_shifts` (
  `id` varchar(64) NOT NULL,
  `week_template_id` varchar(64) NOT NULL,
  `day_of_week` tinyint(4) NOT NULL,
  `employee_id` varchar(64) DEFAULT NULL,
  `position_id` varchar(64) DEFAULT NULL,
  `title` varchar(120) DEFAULT NULL,
  `start_time` time NOT NULL,
  `end_time` time NOT NULL,
  `break_minutes` int(11) NOT NULL DEFAULT 0,
  `location` varchar(120) DEFAULT NULL,
  `notes` text DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `printers`
--

CREATE TABLE `printers` (
  `printer_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `connection_type` varchar(20) NOT NULL,
  `ip_address` varchar(45) DEFAULT NULL,
  `port` int(11) NOT NULL DEFAULT 9100,
  `bluetooth_address` varchar(17) DEFAULT NULL,
  `language` varchar(10) NOT NULL,
  `role` varchar(30) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `production_product_ids` text DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT current_timestamp(),
  `updated_at` timestamp NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `paper_width_mm` int(11) NOT NULL DEFAULT 80
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `productcateg`
--

CREATE TABLE `productcateg` (
  `categ_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `merchant_categ_id` varchar(20) NOT NULL,
  `categ_name` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `categ_order` int(11) NOT NULL,
  `bg_color` varchar(9) NOT NULL DEFAULT '#ffffff',
  `available` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `products`
--

CREATE TABLE `products` (
  `product_id` int(11) NOT NULL,
  `by_product_of` int(11) DEFAULT NULL,
  `merchant_Id` int(11) NOT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `product_desc` text DEFAULT NULL COMMENT 'Short description of the product. Talink about components is not mandatory',
  `img` longtext DEFAULT NULL COMMENT 'picture base64',
  `image_url` longtext DEFAULT NULL,
  `bg_color` varchar(11) NOT NULL DEFAULT '#ffffff',
  `production_color` varchar(11) DEFAULT NULL,
  `display_order` int(11) NOT NULL DEFAULT 0,
  `price` int(11) NOT NULL,
  `price_take_away` int(11) NOT NULL DEFAULT 0,
  `price_delivery` int(11) NOT NULL DEFAULT 0,
  `price_uber_eats` int(11) NOT NULL DEFAULT 0,
  `price_deliveroo` int(11) NOT NULL DEFAULT 0,
  `available_in` tinyint(1) NOT NULL DEFAULT 1,
  `available_take_away` tinyint(1) NOT NULL DEFAULT 1,
  `available_delivery` tinyint(1) NOT NULL DEFAULT 1,
  `tva_in_id` int(11) NOT NULL DEFAULT 0 COMMENT 'TVA rate ID',
  `tva_delivery_id` int(11) NOT NULL DEFAULT 0 COMMENT 'TVA rate ID',
  `tva_take_away_id` int(11) NOT NULL DEFAULT 0 COMMENT 'TVA rate ID',
  `category` varchar(30) NOT NULL,
  `status` varchar(20) NOT NULL DEFAULT '1',
  `is_product_group` tinyint(1) NOT NULL DEFAULT 0,
  `is_available_on_sno` tinyint(1) NOT NULL DEFAULT 1,
  `is_available_on_kiosk` tinyint(1) NOT NULL DEFAULT 1,
  `sync_deliveroo` tinyint(1) NOT NULL DEFAULT 1,
  `sync_uber_eats` tinyint(1) NOT NULL DEFAULT 1,
  `available` tinyint(1) NOT NULL DEFAULT 1 COMMENT 'Product is available on the menu',
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT 'Product is not deleted definitely',
  `is_popular` tinyint(1) DEFAULT 0,
  `creation_date` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `product_allergens`
--

CREATE TABLE `product_allergens` (
  `product_id` varchar(255) NOT NULL,
  `allergen_id` varchar(255) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `product_configurable_attribute`
--

CREATE TABLE `product_configurable_attribute` (
  `product_id` varchar(64) NOT NULL,
  `configurable_attribute_id` varchar(64) NOT NULL,
  `num_order` int(11) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `product_marketing_categories`
--

CREATE TABLE `product_marketing_categories` (
  `product_id` varchar(64) NOT NULL,
  `marketing_category_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT current_timestamp(),
  `updated_at` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `product_ratings`
--

CREATE TABLE `product_ratings` (
  `id` bigint(20) UNSIGNED NOT NULL,
  `order_rating_id` bigint(20) UNSIGNED NOT NULL COMMENT 'Clé étrangère vers order_ratings',
  `product_id` varchar(255) NOT NULL COMMENT 'ID unique du produit',
  `rating` tinyint(3) UNSIGNED NOT NULL COMMENT 'Note de 1 à 5 pour le produit'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `product_tags`
--

CREATE TABLE `product_tags` (
  `product_id` varchar(255) NOT NULL,
  `tag_id` varchar(255) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `purchased_components`
--

CREATE TABLE `purchased_components` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `component_id` int(11) NOT NULL,
  `barcode` varchar(25) NOT NULL,
  `price` float NOT NULL,
  `quantity` int(11) NOT NULL,
  `uom` int(11) NOT NULL,
  `remaining_quantity` float NOT NULL DEFAULT 0,
  `bought_quantity` int(11) NOT NULL,
  `registration_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `empty_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `qrcodes`
--

CREATE TABLE `qrcodes` (
  `QR_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `description` varchar(70) DEFAULT NULL,
  `user_id` int(11) DEFAULT NULL,
  `location_id` int(11) DEFAULT NULL,
  `menu_only` tinyint(1) NOT NULL DEFAULT 0,
  `delivery` tinyint(4) NOT NULL DEFAULT 0,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `mywelloresto_flag` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'MyWelloResto test page',
  `code` text NOT NULL,
  `last_waiter_call` timestamp NULL DEFAULT NULL COMMENT 'SERVER DATE of last call to waiter',
  `creation_date` timestamp NULL DEFAULT current_timestamp(),
  `deleted` tinyint(1) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `receipts`
--

CREATE TABLE `receipts` (
  `receipt_id` varchar(50) NOT NULL COMMENT 'UUID technique',
  `merchant_id` int(11) NOT NULL,
  `order_id` int(11) NOT NULL,
  `receipt_number` varchar(50) NOT NULL COMMENT 'Numéro fiscal séquentiel ex: F-2026-00012',
  `total_ttc` int(11) NOT NULL COMMENT 'En cents',
  `total_ht` int(11) NOT NULL COMMENT 'En cents',
  `tax_details` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Ventilation par taux ex: {"1000": 150, "2000": 300}' CHECK (json_valid(`tax_details`)),
  `items_snapshot` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Copie des produits vendus' CHECK (json_valid(`items_snapshot`)),
  `payments_snapshot` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Copie des paiements' CHECK (json_valid(`payments_snapshot`)),
  `created_at` timestamp NOT NULL DEFAULT current_timestamp(),
  `prev_hash` varchar(64) DEFAULT NULL,
  `hash` varchar(64) NOT NULL,
  `signature` text NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `recipes`
--

CREATE TABLE `recipes` (
  `recipe_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `product_id` int(11) NOT NULL,
  `preparation_time` int(11) NOT NULL DEFAULT 0 COMMENT 'in seconds'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `requires`
--

CREATE TABLE `requires` (
  `id` int(11) NOT NULL,
  `recipe_id` int(11) NOT NULL,
  `component_id` int(11) DEFAULT NULL,
  `consumable_id` int(11) DEFAULT NULL,
  `quantity` double NOT NULL DEFAULT 0,
  `unit_of_measure` int(11) NOT NULL,
  `in_orders` tinyint(1) NOT NULL DEFAULT 1,
  `take_away_orders` tinyint(1) NOT NULL DEFAULT 1,
  `delivery_orders` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `last_update` datetime DEFAULT NULL COMMENT 'deactivation date (server time)',
  `creation_date` datetime NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `restaurant_ticket`
--

CREATE TABLE `restaurant_ticket` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `payment_id` int(11) DEFAULT NULL,
  `barcode` varchar(20) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `scannorder_session`
--

CREATE TABLE `scannorder_session` (
  `user_code` varchar(255) NOT NULL,
  `user_name` varchar(40) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `scannorder_settings`
--

CREATE TABLE `scannorder_settings` (
  `merchant_id` int(11) NOT NULL,
  `activated` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Is ScanNOrder activated ? updatable with merchant app',
  `show_address` tinyint(1) NOT NULL DEFAULT 0,
  `header_background` longtext DEFAULT NULL,
  `header_background_url` varchar(255) DEFAULT NULL,
  `home_page` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Show Home Page ?',
  `home_page_title` varchar(50) DEFAULT NULL,
  `home_page_desc` text DEFAULT NULL,
  `info_popup_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `info_popup_title` text NOT NULL DEFAULT 'Wow, Attends une seconde !',
  `info_popup_content` text NOT NULL DEFAULT '\'La vente d\\\'alcool est réservée à un public majeur. L\\\'abus d\\\'alcool est dangereux pour la santé, consommez avec modération !\'',
  `info_popup_button_content` text NOT NULL DEFAULT 'J\'ai compris !',
  `product_bg_color` varchar(9) NOT NULL DEFAULT '#ffffff',
  `nav_bg_color` varchar(9) NOT NULL DEFAULT '#ffffff',
  `bg_color` varchar(9) NOT NULL DEFAULT '#f5f5f5',
  `btn_color` varchar(9) NOT NULL DEFAULT '#0259b6',
  `btn_text_color` varchar(9) NOT NULL DEFAULT '#ffffff',
  `product_categ_bg_color` varchar(9) NOT NULL DEFAULT '#ffffff',
  `product_categ_text_color` varchar(9) NOT NULL DEFAULT '#927c0c',
  `popup_bg_color` varchar(9) NOT NULL DEFAULT '#f2f2f2',
  `popup_text_color` varchar(9) NOT NULL DEFAULT '#000000',
  `ad_text_color` varchar(9) NOT NULL DEFAULT '#b3b3b3',
  `home_text_color` varchar(9) NOT NULL DEFAULT '#5e5e5e',
  `product_text_color` varchar(9) NOT NULL DEFAULT '#000000',
  `discount_color` varchar(11) NOT NULL DEFAULT '#227e00',
  `discount_text_color` varchar(11) NOT NULL DEFAULT '#ffffff',
  `border_radius` varchar(8) NOT NULL DEFAULT '21',
  `shadow_style` varchar(8) NOT NULL DEFAULT '0',
  `delivery_type` int(11) NOT NULL DEFAULT 1 COMMENT '1 => prep., pay, SNO\r\n2 => pay, prep, take, SNO',
  `enable_payments` tinyint(1) NOT NULL DEFAULT 0,
  `variable_fees` double NOT NULL DEFAULT 0.007,
  `fixed_fees` int(11) NOT NULL DEFAULT 15,
  `users_default_name` varchar(40) NOT NULL DEFAULT 'Utilisateur',
  `take_away_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `take_away_available` tinyint(1) NOT NULL DEFAULT 1,
  `delivery_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `delivery_available` tinyint(1) NOT NULL DEFAULT 1,
  `in_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `in_available` tinyint(1) NOT NULL DEFAULT 0,
  `seo_title` varchar(255) NOT NULL,
  `seo_description` varchar(512) NOT NULL,
  `seo_keywords` varchar(512) NOT NULL,
  `seo_cuisine_type` varchar(255) NOT NULL,
  `commission_rate` int(11) NOT NULL DEFAULT 0 COMMENT 'Commission rate in percent (0 for internal tool)',
  `last_sync` datetime DEFAULT NULL COMMENT 'Last settings sync timestamp (UTC)',
  `synced_items` int(11) NOT NULL DEFAULT 0 COMMENT 'Number of products currently available on ScanNOrder',
  `logo_url` varchar(512) DEFAULT NULL COMMENT 'Merchant logo URL',
  `banner_url` varchar(512) DEFAULT NULL COMMENT 'Merchant banner/cover URL',
  `header_title` varchar(255) DEFAULT NULL COMMENT 'Hero section title shown on the ordering page',
  `header_text` varchar(512) DEFAULT NULL COMMENT 'Hero section subtitle/body text',
  `cgv_link` varchar(512) DEFAULT NULL COMMENT 'URL to general terms and conditions',
  `return_policy_link` varchar(512) DEFAULT NULL COMMENT 'URL to return / refund policy',
  `legal_notices_link` varchar(512) DEFAULT NULL COMMENT 'URL to legal notices',
  `takeaway_auto_accept` tinyint(1) NOT NULL DEFAULT 0 COMMENT '1 = takeaway orders are auto-accepted',
  `delivery_auto_accept` tinyint(1) NOT NULL DEFAULT 0 COMMENT '1 = delivery orders are auto-accepted',
  `closed_until` datetime DEFAULT NULL COMMENT 'If set in the future (UTC), ScanNOrder is temporarily closed until this timestamp'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `services_performed`
--

CREATE TABLE `services_performed` (
  `id` int(25) NOT NULL,
  `user_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `cash_desk_id` int(11) NOT NULL,
  `cash_register_id` int(11) DEFAULT NULL,
  `planned_shift_id` int(11) DEFAULT NULL,
  `start_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `clock_in_photo_url` longtext DEFAULT NULL,
  `end_date` timestamp NULL DEFAULT NULL,
  `clock_out_photo_url` longtext DEFAULT NULL,
  `shift_offset` int(11) DEFAULT NULL,
  `shift_duration` int(11) DEFAULT NULL,
  `extra_hours` int(11) DEFAULT NULL,
  `confirmed` tinyint(1) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `session_orderitem`
--

CREATE TABLE `session_orderitem` (
  `user_code` varchar(255) NOT NULL,
  `order_item_id` int(11) NOT NULL,
  `quantity` int(11) NOT NULL,
  `paid_quantity` int(11) NOT NULL DEFAULT 0,
  `payment_intent_quantity` int(11) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `shift_templates`
--

CREATE TABLE `shift_templates` (
  `shift_template_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `created_by` int(11) NOT NULL,
  `template_name` varchar(50) NOT NULL,
  `planning_role_id` int(11) NOT NULL,
  `start_hour` time NOT NULL,
  `end_hour` time NOT NULL,
  `creation_date` datetime NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `shift_templates_items`
--

CREATE TABLE `shift_templates_items` (
  `item_id` int(11) NOT NULL,
  `shift_template_id` int(11) NOT NULL,
  `week_day` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `stock_evolution_records`
--

CREATE TABLE `stock_evolution_records` (
  `record_date` date NOT NULL,
  `component_id` int(11) NOT NULL,
  `stock` float NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `stock_movements`
--

CREATE TABLE `stock_movements` (
  `id` varchar(50) NOT NULL,
  `merchant_id` varchar(50) NOT NULL,
  `user_id` varchar(50) NOT NULL,
  `component_id` varchar(50) DEFAULT NULL,
  `consumable_id` varchar(50) DEFAULT NULL,
  `product_id` varchar(50) DEFAULT NULL,
  `order_item_id` varchar(50) DEFAULT NULL,
  `order_id` varchar(50) DEFAULT NULL,
  `source` varchar(20) NOT NULL COMMENT 'Source of the movement',
  `movement` varchar(20) NOT NULL COMMENT 'Add, remove, consume',
  `quantity` float NOT NULL,
  `unit_of_measure` varchar(50) NOT NULL,
  `comment` varchar(255) DEFAULT NULL,
  `component_cost` int(11) DEFAULT NULL COMMENT 'in cents',
  `movement_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `stock_movements_desc`
--

CREATE TABLE `stock_movements_desc` (
  `id` int(11) NOT NULL,
  `lang` varchar(2) NOT NULL,
  `movement_desc` varchar(40) NOT NULL,
  `multiplier` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `stock_movements_source`
--

CREATE TABLE `stock_movements_source` (
  `id` int(11) NOT NULL,
  `lang` varchar(2) NOT NULL,
  `movement_source` varchar(40) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `stripe_accounts`
--

CREATE TABLE `stripe_accounts` (
  `account_id` varchar(255) NOT NULL,
  `customer_id` varchar(50) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `verification_status` varchar(50) NOT NULL DEFAULT 'action_required' COMMENT '"verified" | "action_required" — mirrored from Stripe account.charges_enabled + payouts_enabled',
  `terminal_location_id` varchar(255) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `stripe_payments`
--

CREATE TABLE `stripe_payments` (
  `id` int(11) NOT NULL,
  `link_key` varchar(100) DEFAULT NULL,
  `order_id` int(11) NOT NULL,
  `payment_id` int(11) DEFAULT NULL,
  `payment_intent_id` varchar(200) DEFAULT NULL,
  `payment_intent_status` varchar(30) NOT NULL DEFAULT 'REQUIRES_CONFIRMATION',
  `checkout_session_id` text DEFAULT NULL,
  `success_key` varchar(100) NOT NULL,
  `customer_email` text DEFAULT NULL,
  `stripe_session_date` timestamp NOT NULL DEFAULT current_timestamp(),
  `wello_resto_total_fees` int(11) NOT NULL DEFAULT 0,
  `stripe_total_fees` int(11) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `subscriptions`
--

CREATE TABLE `subscriptions` (
  `id` int(11) NOT NULL,
  `stripe_subscription_id` varchar(150) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `package_id` int(11) NOT NULL,
  `planning_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `haccp_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `stock_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `scannorder_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `bookings_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `kiosks_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `max_kiosks` int(11) NOT NULL DEFAULT 0 COMMENT 'Nombre max de bornes actives (0 = module non inclus)'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `subscription_invoices`
--

CREATE TABLE `subscription_invoices` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `invoice_id` varchar(50) NOT NULL,
  `status` int(11) NOT NULL DEFAULT 0 COMMENT '0 => open\r\n1 => paid\r\n-1 => error',
  `invoice_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `amount` int(11) NOT NULL COMMENT 'in cents',
  `payment_date` timestamp NULL DEFAULT NULL,
  `comment` varchar(150) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `sub_cash_registers`
--

CREATE TABLE `sub_cash_registers` (
  `cash_register_id` varchar(20) NOT NULL,
  `sub_cash_register_id` int(11) NOT NULL,
  `device_id` varchar(50) NOT NULL,
  `cash_fund` int(11) NOT NULL COMMENT 'in cents',
  `start_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `sys_attendance_sources`
--

CREATE TABLE `sys_attendance_sources` (
  `code` varchar(32) NOT NULL,
  `label` varchar(80) NOT NULL,
  `sort_order` int(11) NOT NULL DEFAULT 0,
  `active` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `sys_contract_types`
--

CREATE TABLE `sys_contract_types` (
  `code` varchar(32) NOT NULL,
  `label` varchar(80) NOT NULL,
  `sort_order` int(11) NOT NULL DEFAULT 0,
  `active` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `sys_planning_event_types`
--

CREATE TABLE `sys_planning_event_types` (
  `code` varchar(32) NOT NULL,
  `label` varchar(80) NOT NULL,
  `sort_order` int(11) NOT NULL DEFAULT 0,
  `active` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `tags`
--

CREATE TABLE `tags` (
  `tag_id` varchar(42) NOT NULL,
  `merchant_id` varchar(35) NOT NULL,
  `name` varchar(50) NOT NULL,
  `color` varchar(9) NOT NULL DEFAULT '#ffffff',
  `display_order` int(11) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `temperature_readings`
--

CREATE TABLE `temperature_readings` (
  `id` varchar(64) NOT NULL,
  `session_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `zone_id` varchar(64) NOT NULL,
  `value` decimal(5,2) NOT NULL,
  `status` enum('ok','alert','critical') NOT NULL DEFAULT 'ok',
  `photo_url` varchar(512) DEFAULT NULL,
  `signature` mediumtext DEFAULT NULL,
  `comment` text DEFAULT NULL,
  `created_by` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `temperature_reading_corrective_actions`
--

CREATE TABLE `temperature_reading_corrective_actions` (
  `id` varchar(64) NOT NULL,
  `reading_id` varchar(64) NOT NULL,
  `action_id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `note` text DEFAULT NULL,
  `photo_url` varchar(512) DEFAULT NULL,
  `follow_up_value` decimal(5,2) DEFAULT NULL,
  `created_by` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `temperature_sessions`
--

CREATE TABLE `temperature_sessions` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `created_by` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `temperature_zones`
--

CREATE TABLE `temperature_zones` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `name` varchar(150) NOT NULL,
  `target_temp_min` decimal(5,2) NOT NULL,
  `target_temp_max` decimal(5,2) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `timezone_info`
--

CREATE TABLE `timezone_info` (
  `timezone` varchar(30) NOT NULL,
  `offset` varchar(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `tva_categories`
--

CREATE TABLE `tva_categories` (
  `tva_id` int(11) NOT NULL,
  `delivery_type` varchar(20) NOT NULL COMMENT '0 => in, 1 => delivery, 3=> take away (2 not used because 2 is SNO is "isDelivery" field or orders)',
  `tva_title` varchar(30) NOT NULL,
  `tva_desc` varchar(150) NOT NULL,
  `tva_rate` float NOT NULL COMMENT 'in percent (5 => 5%)',
  `show_in_report` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `unit_of_measure`
--

CREATE TABLE `unit_of_measure` (
  `id` int(11) NOT NULL,
  `UOM` varchar(5) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `unit_of_measure_convert`
--

CREATE TABLE `unit_of_measure_convert` (
  `id_from` int(11) NOT NULL,
  `id_to` int(11) NOT NULL,
  `ratio` float NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `unit_of_measure_desc`
--

CREATE TABLE `unit_of_measure_desc` (
  `id` int(11) NOT NULL,
  `lang` varchar(3) NOT NULL,
  `uom_desc` text NOT NULL,
  `uom_short_desc` varchar(20) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `upsell_suggestions`
--

CREATE TABLE `upsell_suggestions` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `order_id` varchar(64) DEFAULT NULL,
  `cart_signature` varchar(64) NOT NULL,
  `suggested_items` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL CHECK (json_valid(`suggested_items`)),
  `source` varchar(32) NOT NULL,
  `accepted_items` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`accepted_items`)),
  `revenue_impact` decimal(10,2) DEFAULT NULL,
  `llm_provider` varchar(32) DEFAULT NULL,
  `llm_model` varchar(64) DEFAULT NULL,
  `tokens_in` int(11) DEFAULT NULL,
  `tokens_out` int(11) DEFAULT NULL,
  `latency_ms` int(11) DEFAULT NULL,
  `created_at` timestamp NOT NULL DEFAULT current_timestamp(),
  `staff_member_id` varchar(64) DEFAULT NULL,
  `channel` enum('POS','SNO','KIOSK') NOT NULL DEFAULT 'POS'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

-- --------------------------------------------------------

--
-- Structure de la table `users`
--

CREATE TABLE `users` (
  `user_id` varchar(50) NOT NULL,
  `merchant_id` int(11) DEFAULT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `first_name` varchar(40) NOT NULL COMMENT 'Prénom',
  `last_name` varchar(40) NOT NULL COMMENT 'Nom',
  `password` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `pin_code` varchar(6) DEFAULT NULL,
  `mfa_type` varchar(25) DEFAULT NULL,
  `mfa_status` varchar(25) DEFAULT NULL,
  `mfa_verified_at` timestamp NULL DEFAULT NULL,
  `mfa_otp_sent_at` timestamp NULL DEFAULT NULL,
  `mfa_secret` varchar(50) DEFAULT NULL,
  `userName` varchar(20) DEFAULT NULL,
  `email` varchar(255) NOT NULL,
  `email_verified_at` timestamp NULL DEFAULT NULL,
  `dob` date DEFAULT NULL COMMENT 'date of birth',
  `tel` varchar(20) DEFAULT NULL,
  `tel_verified_at` timestamp NULL DEFAULT NULL,
  `address` varchar(255) DEFAULT NULL,
  `street_number` varchar(20) DEFAULT NULL,
  `street` varchar(255) DEFAULT NULL,
  `city` varchar(255) DEFAULT NULL,
  `country` varchar(255) DEFAULT NULL,
  `zip_code` varchar(9) DEFAULT NULL,
  `lat` text DEFAULT NULL,
  `lng` text DEFAULT NULL,
  `heading` int(11) NOT NULL DEFAULT 0,
  `profile_picture` longtext DEFAULT NULL,
  `planning_color` varchar(11) NOT NULL DEFAULT '#28B2FC',
  `isReception` tinyint(1) NOT NULL DEFAULT 0,
  `isWaiter` tinyint(1) NOT NULL DEFAULT 0,
  `isDelivery` int(1) NOT NULL DEFAULT 0,
  `admin` tinyint(1) NOT NULL DEFAULT 0,
  `access_id` int(11) DEFAULT NULL,
  `waiter_device_token` varchar(255) DEFAULT NULL COMMENT 'Device token of WR Waitrer',
  `reception_device_token` varchar(255) DEFAULT NULL COMMENT 'Device token of WR Reception',
  `delivery_device_token` varchar(255) DEFAULT NULL COMMENT 'Device token of WR Delivery',
  `token` varchar(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `terms_of_use_accepted` tinyint(1) NOT NULL DEFAULT 0,
  `creationDate` datetime NOT NULL DEFAULT current_timestamp(),
  `created_at` timestamp NULL DEFAULT current_timestamp(),
  `lastAccess` datetime DEFAULT NULL COMMENT 'can be deleted (29/05/2026)',
  `last_activity` timestamp NOT NULL DEFAULT current_timestamp(),
  `enabled` int(11) NOT NULL DEFAULT 1,
  `last_login_at` timestamp NULL DEFAULT NULL,
  `last_position_at` datetime DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;

-- --------------------------------------------------------

--
-- Structure de la table `users_devices`
--

CREATE TABLE `users_devices` (
  `user_id` int(11) NOT NULL,
  `merchant_id` varchar(25) DEFAULT NULL,
  `app` varchar(20) NOT NULL,
  `device_id` varchar(255) NOT NULL,
  `fcm_token` longtext NOT NULL,
  `last_used` timestamp NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `users_nfc_tags`
--

CREATE TABLE `users_nfc_tags` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `user_id` int(11) DEFAULT NULL,
  `tag_id` int(11) NOT NULL,
  `tag_token` varchar(50) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `users_rights`
--

CREATE TABLE `users_rights` (
  `id` int(11) NOT NULL,
  `user_id` varchar(64) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `token` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `access_wrwaiter` tinyint(1) NOT NULL DEFAULT 1,
  `access_wrreception` tinyint(1) NOT NULL DEFAULT 1,
  `access_wrdelivery` tinyint(1) NOT NULL DEFAULT 1,
  `position_id` varchar(64) DEFAULT NULL,
  `position_note` text DEFAULT NULL,
  `job_title` varchar(150) DEFAULT NULL,
  `role` varchar(32) NOT NULL DEFAULT 'employee',
  `contract_type_code` varchar(32) DEFAULT NULL,
  `contract_start_date` date DEFAULT NULL,
  `contract_end_date` date DEFAULT NULL,
  `probation_end_date` date DEFAULT NULL,
  `last_medical_checkup_date` date DEFAULT NULL,
  `contract_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `max_weekly_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `required_rest_days` int(11) NOT NULL DEFAULT 2,
  `sunday_premium` tinyint(1) NOT NULL DEFAULT 0,
  `night_premium` tinyint(1) NOT NULL DEFAULT 0,
  `hourly_rate` bigint(20) NOT NULL DEFAULT 0,
  `gross_monthly_salary` bigint(20) NOT NULL DEFAULT 0,
  `employer_charges_pct` decimal(5,2) NOT NULL DEFAULT 45.00,
  `transport_cost` bigint(20) NOT NULL DEFAULT 0,
  `hr_comment` text DEFAULT NULL,
  `manage_menu` tinyint(1) NOT NULL DEFAULT 0,
  `manage_plannings` tinyint(1) NOT NULL DEFAULT 0,
  `manage_users` tinyint(1) NOT NULL DEFAULT 0,
  `manage_settings` tinyint(1) NOT NULL DEFAULT 0,
  `manage_haccp` tinyint(1) NOT NULL DEFAULT 0,
  `view_reports` tinyint(1) NOT NULL DEFAULT 0,
  `export_reports` tinyint(1) NOT NULL DEFAULT 0,
  `view_financials` tinyint(1) NOT NULL DEFAULT 0,
  `export_financials` tinyint(1) NOT NULL DEFAULT 0,
  `manage_customers` tinyint(1) NOT NULL DEFAULT 0,
  `export_customers` tinyint(1) NOT NULL DEFAULT 0,
  `admin` tinyint(1) NOT NULL DEFAULT 0,
  `print_merchant_cash_report` tinyint(1) NOT NULL DEFAULT 0,
  `open_cash_drawer` tinyint(1) NOT NULL DEFAULT 0,
  `last_login_at` timestamp NOT NULL DEFAULT current_timestamp(),
  `login_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `pin_hash` varchar(64) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Doublure de structure pour la vue `user_status_view`
-- (Voir ci-dessous la vue réelle)
--
CREATE TABLE `user_status_view` (
`user_id` varchar(50)
,`first_name` varchar(40)
,`last_name` varchar(40)
,`lat` text
,`lng` text
,`heading` int(11)
,`status` varchar(19)
);

-- --------------------------------------------------------

--
-- Structure de la table `user_vacations`
--

CREATE TABLE `user_vacations` (
  `id` int(11) NOT NULL,
  `user_id` int(11) NOT NULL,
  `start_date` datetime NOT NULL,
  `end_date` datetime NOT NULL,
  `reason` varchar(255) DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `welloresto_stripe_customers`
--

CREATE TABLE `welloresto_stripe_customers` (
  `merchant_id` int(11) NOT NULL,
  `creator_user_id` int(11) DEFAULT NULL,
  `stripe_customer_id` varchar(255) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `without`
--

CREATE TABLE `without` (
  `id` int(11) NOT NULL,
  `order_id` int(11) NOT NULL,
  `order_item_id` int(11) NOT NULL DEFAULT 0,
  `component_id` int(11) NOT NULL,
  `product_id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;

-- --------------------------------------------------------

--
-- Structure de la table `z_platform_daily_activity_recording`
--

CREATE TABLE `z_platform_daily_activity_recording` (
  `date` date NOT NULL,
  `email_sent` int(11) NOT NULL DEFAULT 0,
  `direction_api_calls` int(11) NOT NULL DEFAULT 0,
  `matrix_api_calls` int(11) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

--
-- Index pour les tables déchargées
--

--
-- Index pour la table `allergens`
--
ALTER TABLE `allergens`
  ADD PRIMARY KEY (`allergen_id`);

--
-- Index pour la table `api_calls`
--
ALTER TABLE `api_calls`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `api_request_logs`
--
ALTER TABLE `api_request_logs`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `app_version`
--
ALTER TABLE `app_version`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `app_version_merchant`
--
ALTER TABLE `app_version_merchant`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `audit_logs`
--
ALTER TABLE `audit_logs`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `availabilities`
--
ALTER TABLE `availabilities`
  ADD PRIMARY KEY (`availability_id`);

--
-- Index pour la table `availabilities_products`
--
ALTER TABLE `availabilities_products`
  ADD PRIMARY KEY (`availability_product_id`);

--
-- Index pour la table `availabilities_schedules`
--
ALTER TABLE `availabilities_schedules`
  ADD PRIMARY KEY (`schedule_id`);

--
-- Index pour la table `available_languages`
--
ALTER TABLE `available_languages`
  ADD PRIMARY KEY (`code`);

--
-- Index pour la table `average_distribution_time`
--
ALTER TABLE `average_distribution_time`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `average_distribution_time_by_category`
--
ALTER TABLE `average_distribution_time_by_category`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `average_distribution_time_history`
--
ALTER TABLE `average_distribution_time_history`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `merchant_id` (`merchant_id`,`category`,`calculation_date`);

--
-- Index pour la table `barcodes`
--
ALTER TABLE `barcodes`
  ADD PRIMARY KEY (`merchant_id`,`barcode`),
  ADD KEY `merchant_id` (`merchant_id`,`barcode`);

--
-- Index pour la table `booked_location`
--
ALTER TABLE `booked_location`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_booked_location` (`booking_id`,`location_id`);

--
-- Index pour la table `bookings`
--
ALTER TABLE `bookings`
  ADD PRIMARY KEY (`booking_id`),
  ADD UNIQUE KEY `uq_bookings_merchant_number` (`merchant_id`,`booking_number`);

--
-- Index pour la table `bookings_settings`
--
ALTER TABLE `bookings_settings`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `booking_duration_rules`
--
ALTER TABLE `booking_duration_rules`
  ADD PRIMARY KEY (`rule_id`);

--
-- Index pour la table `booking_events`
--
ALTER TABLE `booking_events`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `booking_waitlist`
--
ALTER TABLE `booking_waitlist`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `brands`
--
ALTER TABLE `brands`
  ADD PRIMARY KEY (`brand_id`);

--
-- Index pour la table `broadcast_list`
--
ALTER TABLE `broadcast_list`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `calendar`
--
ALTER TABLE `calendar`
  ADD PRIMARY KEY (`date`);

--
-- Index pour la table `cash_desks`
--
ALTER TABLE `cash_desks`
  ADD PRIMARY KEY (`cash_desk_id`);

--
-- Index pour la table `cash_funds`
--
ALTER TABLE `cash_funds`
  ADD PRIMARY KEY (`cash_fund_id`);

--
-- Index pour la table `cash_registers`
--
ALTER TABLE `cash_registers`
  ADD PRIMARY KEY (`cash_register_id`);

--
-- Index pour la table `cash_registers_custom_items`
--
ALTER TABLE `cash_registers_custom_items`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `cash_registers_items`
--
ALTER TABLE `cash_registers_items`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `cash_reports`
--
ALTER TABLE `cash_reports`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `category_discount`
--
ALTER TABLE `category_discount`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `checkout_orderitems`
--
ALTER TABLE `checkout_orderitems`
  ADD PRIMARY KEY (`link_key`,`user_code`,`order_item_id`);

--
-- Index pour la table `cleaning_executions`
--
ALTER TABLE `cleaning_executions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_cleaning_executions_session_enabled` (`session_id`,`enabled`),
  ADD KEY `idx_cleaning_executions_surface_enabled` (`surface_id`,`enabled`),
  ADD KEY `idx_cleaning_executions_merchant_created` (`merchant_id`,`created_at`);

--
-- Index pour la table `cleaning_sessions`
--
ALTER TABLE `cleaning_sessions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_cleaning_sessions_merchant_enabled` (`merchant_id`,`enabled`),
  ADD KEY `idx_cleaning_sessions_merchant_created` (`merchant_id`,`created_at`);

--
-- Index pour la table `cleaning_surfaces`
--
ALTER TABLE `cleaning_surfaces`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_cleaning_surfaces_merchant_enabled` (`merchant_id`,`enabled`),
  ADD KEY `idx_cleaning_surfaces_zone_enabled` (`zone_id`,`enabled`),
  ADD KEY `idx_cleaning_surfaces_merchant_zone` (`merchant_id`,`zone_id`);

--
-- Index pour la table `cleaning_zones`
--
ALTER TABLE `cleaning_zones`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_cleaning_zones_merchant_enabled` (`merchant_id`,`enabled`),
  ADD KEY `idx_cleaning_zones_merchant_name` (`merchant_id`,`name`);

--
-- Index pour la table `components`
--
ALTER TABLE `components`
  ADD PRIMARY KEY (`component_id`);

--
-- Index pour la table `component_category`
--
ALTER TABLE `component_category`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `configurable_attributes`
--
ALTER TABLE `configurable_attributes`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `configurable_attribute_options`
--
ALTER TABLE `configurable_attribute_options`
  ADD PRIMARY KEY (`id`),
  ADD KEY `configurable_attribute_id` (`configurable_attribute_id`);

--
-- Index pour la table `consumables`
--
ALTER TABLE `consumables`
  ADD PRIMARY KEY (`consumable_id`);

--
-- Index pour la table `customer`
--
ALTER TABLE `customer`
  ADD PRIMARY KEY (`customer_id`),
  ADD KEY `idx_customer_lookup` (`merchant_id`,`enabled`,`customer_code`,`customer_name`,`customer_tel`,`customer_address`);

--
-- Index pour la table `customer_advertisement_emails`
--
ALTER TABLE `customer_advertisement_emails`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `customer_loyalty_programs`
--
ALTER TABLE `customer_loyalty_programs`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `customer_loyalty_program_reward_products`
--
ALTER TABLE `customer_loyalty_program_reward_products`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `customer_loyalty_program_target_products`
--
ALTER TABLE `customer_loyalty_program_target_products`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `customer_loyalty_progress`
--
ALTER TABLE `customer_loyalty_progress`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `customer_loyalty_progress_order`
--
ALTER TABLE `customer_loyalty_progress_order`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `customer_rewards`
--
ALTER TABLE `customer_rewards`
  ADD PRIMARY KEY (`reward_id`);

--
-- Index pour la table `delays`
--
ALTER TABLE `delays`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `deletion_reasons`
--
ALTER TABLE `deletion_reasons`
  ADD PRIMARY KEY (`deletion_reason_id`);

--
-- Index pour la table `delivery_position`
--
ALTER TABLE `delivery_position`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_delivery_position_session` (`delivery_session_id`,`recorded_at`),
  ADD KEY `idx_delivery_position_user` (`user_id`,`recorded_at`);

--
-- Index pour la table `delivery_session`
--
ALTER TABLE `delivery_session`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `delivery_session_order`
--
ALTER TABLE `delivery_session_order`
  ADD PRIMARY KEY (`delivery_session_id`,`order_id`);

--
-- Index pour la table `device_link`
--
ALTER TABLE `device_link`
  ADD PRIMARY KEY (`device_id`);

--
-- Index pour la table `discounts`
--
ALTER TABLE `discounts`
  ADD PRIMARY KEY (`discount_id`);

--
-- Index pour la table `discounts_products`
--
ALTER TABLE `discounts_products`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `discounts_products_options`
--
ALTER TABLE `discounts_products_options`
  ADD PRIMARY KEY (`discount_id`,`product_id`,`option_id`);

--
-- Index pour la table `discounts_schedules`
--
ALTER TABLE `discounts_schedules`
  ADD PRIMARY KEY (`schedule_id`);

--
-- Index pour la table `employees`
--
ALTER TABLE `employees`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_employees_merchant_user` (`merchant_id`,`user_id`),
  ADD UNIQUE KEY `uq_employees_merchant_member` (`merchant_id`,`member_id`),
  ADD KEY `idx_employees_merchant_active` (`merchant_id`,`active`),
  ADD KEY `idx_employees_merchant` (`merchant_id`),
  ADD KEY `idx_employees_contract_type` (`contract_type_code`),
  ADD KEY `idx_employees_position_id` (`position_id`),
  ADD KEY `idx_employees_member_id` (`member_id`);

--
-- Index pour la table `employee_documents`
--
ALTER TABLE `employee_documents`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_empdocs_merchant` (`merchant_id`),
  ADD KEY `idx_empdocs_employee` (`employee_id`);

--
-- Index pour la table `employment_agreement`
--
ALTER TABLE `employment_agreement`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `employment_contract`
--
ALTER TABLE `employment_contract`
  ADD PRIMARY KEY (`contract_id`);

--
-- Index pour la table `expiration_dates`
--
ALTER TABLE `expiration_dates`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `external_tokens`
--
ALTER TABLE `external_tokens`
  ADD PRIMARY KEY (`token_type`);

--
-- Index pour la table `extra`
--
ALTER TABLE `extra`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `firebase_fcm_access_token`
--
ALTER TABLE `firebase_fcm_access_token`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `floors`
--
ALTER TABLE `floors`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `floor_areas`
--
ALTER TABLE `floor_areas`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `floor_obstacles`
--
ALTER TABLE `floor_obstacles`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `goods_receipts`
--
ALTER TABLE `goods_receipts`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `haccp_corrective_actions`
--
ALTER TABLE `haccp_corrective_actions`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_haccp_corrective_actions_code` (`code`),
  ADD KEY `idx_haccp_corrective_actions_active` (`active`);

--
-- Index pour la table `haccp_settings`
--
ALTER TABLE `haccp_settings`
  ADD PRIMARY KEY (`merchant_id`),
  ADD UNIQUE KEY `uq_haccpsettings_merchant` (`merchant_id`);

--
-- Index pour la table `holiday_calendar`
--
ALTER TABLE `holiday_calendar`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_holiday_calendar_country_date` (`country_code`,`holiday_date`),
  ADD KEY `idx_holiday_calendar_country` (`country_code`);

--
-- Index pour la table `hours_amendments`
--
ALTER TABLE `hours_amendments`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_hours_amendments_merchant` (`merchant_id`),
  ADD KEY `idx_hours_amendments_employee` (`employee_id`);

--
-- Index pour la table `hours_of_operation`
--
ALTER TABLE `hours_of_operation`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `integration_deliveroo`
--
ALTER TABLE `integration_deliveroo`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `integration_deliveroo_attributes_mapping`
--
ALTER TABLE `integration_deliveroo_attributes_mapping`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_mapping` (`merchant_id`,`modifier_group_pos_id`);

--
-- Index pour la table `integration_deliveroo_options_mapping`
--
ALTER TABLE `integration_deliveroo_options_mapping`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_mapping` (`merchant_id`,`item_id`);

--
-- Index pour la table `integration_deliveroo_products_mapping`
--
ALTER TABLE `integration_deliveroo_products_mapping`
  ADD PRIMARY KEY (`item_id`,`product_id`,`merchant_id`);

--
-- Index pour la table `integration_uber_direct`
--
ALTER TABLE `integration_uber_direct`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `integration_uber_eats`
--
ALTER TABLE `integration_uber_eats`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `integration_uber_eats_attributes_mapping`
--
ALTER TABLE `integration_uber_eats_attributes_mapping`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_mapping` (`merchant_id`,`modifier_group_id`);

--
-- Index pour la table `integration_uber_eats_components_mapping`
--
ALTER TABLE `integration_uber_eats_components_mapping`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `integration_uber_eats_options_mapping`
--
ALTER TABLE `integration_uber_eats_options_mapping`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_mapping` (`merchant_id`,`item_id`);

--
-- Index pour la table `integration_uber_eats_products_mapping`
--
ALTER TABLE `integration_uber_eats_products_mapping`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `integration_uber_eats_reports`
--
ALTER TABLE `integration_uber_eats_reports`
  ADD PRIMARY KEY (`workflow_id`);

--
-- Index pour la table `invoices`
--
ALTER TABLE `invoices`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `kiosks`
--
ALTER TABLE `kiosks`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `kiosk_device_tokens`
--
ALTER TABLE `kiosk_device_tokens`
  ADD UNIQUE KEY `idx_device_token_hash` (`token_hash`);

--
-- Index pour la table `kiosk_enrollment_codes`
--
ALTER TABLE `kiosk_enrollment_codes`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `kiosk_settings`
--
ALTER TABLE `kiosk_settings`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `labels`
--
ALTER TABLE `labels`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `labor_rules`
--
ALTER TABLE `labor_rules`
  ADD PRIMARY KEY (`country_code`);

--
-- Index pour la table `locations`
--
ALTER TABLE `locations`
  ADD PRIMARY KEY (`location_id`);

--
-- Index pour la table `marketing_categories`
--
ALTER TABLE `marketing_categories`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_marketing_categories_merchant_name` (`merchant_id`,`name`),
  ADD KEY `idx_marketing_categories_merchant_enabled_order` (`merchant_id`,`enabled`,`display_order`);

--
-- Index pour la table `merchant`
--
ALTER TABLE `merchant`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `merchant_code`
--
ALTER TABLE `merchant_code`
  ADD PRIMARY KEY (`code_id`);

--
-- Index pour la table `merchant_google_maps_monthly`
--
ALTER TABLE `merchant_google_maps_monthly`
  ADD PRIMARY KEY (`merchant_id`,`month`);

--
-- Index pour la table `merchant_marketing_settings`
--
ALTER TABLE `merchant_marketing_settings`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `merchant_parameters`
--
ALTER TABLE `merchant_parameters`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `merchant_sms_monthly`
--
ALTER TABLE `merchant_sms_monthly`
  ADD PRIMARY KEY (`merchant_id`,`month`);

--
-- Index pour la table `merchant_translation_languages`
--
ALTER TABLE `merchant_translation_languages`
  ADD PRIMARY KEY (`merchant_id`,`lang_code`),
  ADD KEY `lang_code` (`lang_code`);

--
-- Index pour la table `migration_users`
--
ALTER TABLE `migration_users`
  ADD PRIMARY KEY (`_id`);

--
-- Index pour la table `notifications`
--
ALTER TABLE `notifications`
  ADD PRIMARY KEY (`notification_id`);

--
-- Index pour la table `orderitems`
--
ALTER TABLE `orderitems`
  ADD PRIMARY KEY (`order_item_id`,`order_id`,`product_id`),
  ADD KEY `idx_orderitems_product_id` (`product_id`) USING BTREE;

--
-- Index pour la table `orders`
--
ALTER TABLE `orders`
  ADD PRIMARY KEY (`order_id`),
  ADD KEY `idx_orders_state` (`state`),
  ADD KEY `idx_orders_brand_status` (`brand_status`),
  ADD KEY `idx_orders_merchant_id` (`merchant_id`);

--
-- Index pour la table `order_changes_log`
--
ALTER TABLE `order_changes_log`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `order_comments`
--
ALTER TABLE `order_comments`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `order_item_configuration`
--
ALTER TABLE `order_item_configuration`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_order_item_configuration_order_item_id` (`order_item_id`),
  ADD KEY `idx_order_item_configuration_configuration_attribute_option_id` (`configuration_attribute_option_id`);

--
-- Index pour la table `order_location`
--
ALTER TABLE `order_location`
  ADD PRIMARY KEY (`order_id`,`location_id`);

--
-- Index pour la table `order_ratings`
--
ALTER TABLE `order_ratings`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uniq_order_id` (`order_id`);

--
-- Index pour la table `packages`
--
ALTER TABLE `packages`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `payments`
--
ALTER TABLE `payments`
  ADD PRIMARY KEY (`payment_id`);

--
-- Index pour la table `pictures`
--
ALTER TABLE `pictures`
  ADD PRIMARY KEY (`picture_id`);

--
-- Index pour la table `planned_shifts`
--
ALTER TABLE `planned_shifts`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `planning_holiday_overrides`
--
ALTER TABLE `planning_holiday_overrides`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_planning_holiday_overrides_merchant_date` (`merchant_id`,`holiday_date`),
  ADD KEY `idx_planning_holiday_overrides_merchant` (`merchant_id`),
  ADD KEY `idx_planning_holiday_overrides_date` (`holiday_date`);

--
-- Index pour la table `planning_leave_requests`
--
ALTER TABLE `planning_leave_requests`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_leave_requests_merchant_employee` (`merchant_id`,`employee_id`),
  ADD KEY `idx_planning_leave_requests_status` (`status`),
  ADD KEY `idx_planning_leave_requests_range` (`start_date`,`end_date`),
  ADD KEY `idx_planning_leave_requests_requested_by` (`requested_by_user_id`),
  ADD KEY `idx_planning_leave_requests_processed_by` (`processed_by_user_id`);

--
-- Index pour la table `planning_positions`
--
ALTER TABLE `planning_positions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_positions_merchant` (`merchant_id`),
  ADD KEY `idx_planning_positions_merchant_label` (`merchant_id`,`label`),
  ADD KEY `idx_planning_positions_merchant_sort` (`merchant_id`,`sort_order`);

--
-- Index pour la table `planning_revenue_forecasts`
--
ALTER TABLE `planning_revenue_forecasts`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_planning_revenue_forecasts_merchant_date` (`merchant_id`,`forecast_date`),
  ADD KEY `idx_planning_revenue_forecasts_merchant` (`merchant_id`);

--
-- Index pour la table `planning_roles`
--
ALTER TABLE `planning_roles`
  ADD PRIMARY KEY (`role_id`);

--
-- Index pour la table `planning_settings`
--
ALTER TABLE `planning_settings`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_planning_settings_merchant` (`merchant_id`),
  ADD KEY `idx_planning_settings_merchant` (`merchant_id`),
  ADD KEY `idx_planning_settings_labor_country_code` (`labor_country_code`);

--
-- Index pour la table `planning_shifts`
--
ALTER TABLE `planning_shifts`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_shifts_merchant` (`merchant_id`),
  ADD KEY `idx_planning_shifts_week` (`week_id`),
  ADD KEY `idx_planning_shifts_employee_date` (`employee_id`,`shift_date`),
  ADD KEY `idx_planning_shifts_date` (`shift_date`),
  ADD KEY `idx_planning_shifts_position_id` (`position_id`);

--
-- Index pour la table `planning_shift_swap_requests`
--
ALTER TABLE `planning_shift_swap_requests`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_shift_swap_requests_merchant` (`merchant_id`),
  ADD KEY `idx_planning_shift_swap_requests_status` (`status`),
  ADD KEY `idx_planning_shift_swap_requests_requester` (`requester_employee_id`),
  ADD KEY `idx_planning_shift_swap_requests_target` (`target_employee_id`),
  ADD KEY `idx_planning_shift_swap_requests_requester_shift` (`requester_shift_id`),
  ADD KEY `idx_planning_shift_swap_requests_target_shift` (`target_shift_id`),
  ADD KEY `idx_planning_shift_swap_requests_requested_by` (`requested_by_user_id`),
  ADD KEY `idx_planning_shift_swap_requests_processed_by` (`processed_by_user_id`);

--
-- Index pour la table `planning_shift_templates`
--
ALTER TABLE `planning_shift_templates`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_shift_templates_merchant` (`merchant_id`);

--
-- Index pour la table `planning_time_entries`
--
ALTER TABLE `planning_time_entries`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_time_entries_merchant_employee` (`merchant_id`,`employee_id`),
  ADD KEY `idx_planning_time_entries_open` (`employee_id`,`clock_out_at`),
  ADD KEY `idx_planning_time_entries_shift` (`shift_id`),
  ADD KEY `idx_planning_time_entries_clock_in` (`clock_in_at`),
  ADD KEY `idx_planning_time_entries_attendance_source` (`attendance_source`);

--
-- Index pour la table `planning_weeks`
--
ALTER TABLE `planning_weeks`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_weeks_merchant_start` (`merchant_id`,`start_date`,`enabled`),
  ADD KEY `idx_planning_weeks_merchant` (`merchant_id`),
  ADD KEY `idx_planning_weeks_range` (`start_date`,`end_date`);

--
-- Index pour la table `planning_week_templates`
--
ALTER TABLE `planning_week_templates`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_week_templates_merchant` (`merchant_id`);

--
-- Index pour la table `planning_week_template_shifts`
--
ALTER TABLE `planning_week_template_shifts`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_planning_week_template_shifts_template` (`week_template_id`);

--
-- Index pour la table `printers`
--
ALTER TABLE `printers`
  ADD PRIMARY KEY (`printer_id`);

--
-- Index pour la table `productcateg`
--
ALTER TABLE `productcateg`
  ADD PRIMARY KEY (`categ_id`);

--
-- Index pour la table `products`
--
ALTER TABLE `products`
  ADD PRIMARY KEY (`product_id`,`merchant_Id`);

--
-- Index pour la table `product_allergens`
--
ALTER TABLE `product_allergens`
  ADD PRIMARY KEY (`product_id`,`allergen_id`);

--
-- Index pour la table `product_configurable_attribute`
--
ALTER TABLE `product_configurable_attribute`
  ADD PRIMARY KEY (`configurable_attribute_id`,`product_id`);

--
-- Index pour la table `product_marketing_categories`
--
ALTER TABLE `product_marketing_categories`
  ADD PRIMARY KEY (`product_id`),
  ADD KEY `idx_product_marketing_categories_merchant_category` (`merchant_id`,`marketing_category_id`);

--
-- Index pour la table `product_ratings`
--
ALTER TABLE `product_ratings`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_order_rating_id` (`order_rating_id`);

--
-- Index pour la table `product_tags`
--
ALTER TABLE `product_tags`
  ADD PRIMARY KEY (`product_id`,`tag_id`);

--
-- Index pour la table `purchased_components`
--
ALTER TABLE `purchased_components`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `qrcodes`
--
ALTER TABLE `qrcodes`
  ADD PRIMARY KEY (`QR_id`);

--
-- Index pour la table `receipts`
--
ALTER TABLE `receipts`
  ADD PRIMARY KEY (`receipt_id`);

--
-- Index pour la table `recipes`
--
ALTER TABLE `recipes`
  ADD PRIMARY KEY (`recipe_id`);

--
-- Index pour la table `requires`
--
ALTER TABLE `requires`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `restaurant_ticket`
--
ALTER TABLE `restaurant_ticket`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `scannorder_session`
--
ALTER TABLE `scannorder_session`
  ADD PRIMARY KEY (`user_code`);

--
-- Index pour la table `scannorder_settings`
--
ALTER TABLE `scannorder_settings`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `services_performed`
--
ALTER TABLE `services_performed`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `session_orderitem`
--
ALTER TABLE `session_orderitem`
  ADD PRIMARY KEY (`user_code`,`order_item_id`);

--
-- Index pour la table `shift_templates`
--
ALTER TABLE `shift_templates`
  ADD PRIMARY KEY (`shift_template_id`);

--
-- Index pour la table `shift_templates_items`
--
ALTER TABLE `shift_templates_items`
  ADD PRIMARY KEY (`item_id`);

--
-- Index pour la table `stock_evolution_records`
--
ALTER TABLE `stock_evolution_records`
  ADD PRIMARY KEY (`record_date`,`component_id`);

--
-- Index pour la table `stock_movements`
--
ALTER TABLE `stock_movements`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_stock_movements_order_id` (`order_id`);

--
-- Index pour la table `stock_movements_desc`
--
ALTER TABLE `stock_movements_desc`
  ADD PRIMARY KEY (`id`,`lang`);

--
-- Index pour la table `stock_movements_source`
--
ALTER TABLE `stock_movements_source`
  ADD PRIMARY KEY (`id`,`lang`);

--
-- Index pour la table `stripe_accounts`
--
ALTER TABLE `stripe_accounts`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `stripe_payments`
--
ALTER TABLE `stripe_payments`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `stripe_order_link` (`link_key`);

--
-- Index pour la table `subscriptions`
--
ALTER TABLE `subscriptions`
  ADD PRIMARY KEY (`id`,`merchant_id`,`package_id`);

--
-- Index pour la table `subscription_invoices`
--
ALTER TABLE `subscription_invoices`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `sub_cash_registers`
--
ALTER TABLE `sub_cash_registers`
  ADD PRIMARY KEY (`sub_cash_register_id`,`cash_register_id`);

--
-- Index pour la table `sys_attendance_sources`
--
ALTER TABLE `sys_attendance_sources`
  ADD PRIMARY KEY (`code`);

--
-- Index pour la table `sys_contract_types`
--
ALTER TABLE `sys_contract_types`
  ADD PRIMARY KEY (`code`);

--
-- Index pour la table `sys_planning_event_types`
--
ALTER TABLE `sys_planning_event_types`
  ADD PRIMARY KEY (`code`);

--
-- Index pour la table `tags`
--
ALTER TABLE `tags`
  ADD PRIMARY KEY (`tag_id`);

--
-- Index pour la table `temperature_readings`
--
ALTER TABLE `temperature_readings`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `temperature_reading_corrective_actions`
--
ALTER TABLE `temperature_reading_corrective_actions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_trca_reading` (`reading_id`),
  ADD KEY `idx_trca_action` (`action_id`),
  ADD KEY `idx_trca_merchant` (`merchant_id`);

--
-- Index pour la table `temperature_sessions`
--
ALTER TABLE `temperature_sessions`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `temperature_zones`
--
ALTER TABLE `temperature_zones`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `tva_categories`
--
ALTER TABLE `tva_categories`
  ADD PRIMARY KEY (`tva_id`);

--
-- Index pour la table `unit_of_measure`
--
ALTER TABLE `unit_of_measure`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `unit_of_measure_convert`
--
ALTER TABLE `unit_of_measure_convert`
  ADD PRIMARY KEY (`id_from`,`id_to`);

--
-- Index pour la table `unit_of_measure_desc`
--
ALTER TABLE `unit_of_measure_desc`
  ADD PRIMARY KEY (`id`,`lang`);

--
-- Index pour la table `upsell_suggestions`
--
ALTER TABLE `upsell_suggestions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_upsell_merchant_created` (`merchant_id`,`created_at`),
  ADD KEY `idx_upsell_cart_merchant` (`cart_signature`,`merchant_id`),
  ADD KEY `idx_upsell_acceptance` (`merchant_id`,`accepted_items`(1));

--
-- Index pour la table `users`
--
ALTER TABLE `users`
  ADD PRIMARY KEY (`user_id`),
  ADD UNIQUE KEY `name` (`name`);

--
-- Index pour la table `users_devices`
--
ALTER TABLE `users_devices`
  ADD PRIMARY KEY (`device_id`);

--
-- Index pour la table `users_nfc_tags`
--
ALTER TABLE `users_nfc_tags`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `tag_token` (`tag_token`);

--
-- Index pour la table `users_rights`
--
ALTER TABLE `users_rights`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `user_vacations`
--
ALTER TABLE `user_vacations`
  ADD PRIMARY KEY (`id`);

--
-- Index pour la table `welloresto_stripe_customers`
--
ALTER TABLE `welloresto_stripe_customers`
  ADD PRIMARY KEY (`merchant_id`);

--
-- Index pour la table `without`
--
ALTER TABLE `without`
  ADD PRIMARY KEY (`id`);

--
-- AUTO_INCREMENT pour les tables déchargées
--

--
-- AUTO_INCREMENT pour la table `api_calls`
--
ALTER TABLE `api_calls`
  MODIFY `id` int(255) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `api_request_logs`
--
ALTER TABLE `api_request_logs`
  MODIFY `id` bigint(20) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `app_version`
--
ALTER TABLE `app_version`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `app_version_merchant`
--
ALTER TABLE `app_version_merchant`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `average_distribution_time_history`
--
ALTER TABLE `average_distribution_time_history`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `booked_location`
--
ALTER TABLE `booked_location`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `bookings`
--
ALTER TABLE `bookings`
  MODIFY `booking_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `bookings_settings`
--
ALTER TABLE `bookings_settings`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `broadcast_list`
--
ALTER TABLE `broadcast_list`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `cash_desks`
--
ALTER TABLE `cash_desks`
  MODIFY `cash_desk_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `cash_funds`
--
ALTER TABLE `cash_funds`
  MODIFY `cash_fund_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `cash_registers`
--
ALTER TABLE `cash_registers`
  MODIFY `cash_register_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `cash_registers_custom_items`
--
ALTER TABLE `cash_registers_custom_items`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `cash_registers_items`
--
ALTER TABLE `cash_registers_items`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `cash_reports`
--
ALTER TABLE `cash_reports`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `category_discount`
--
ALTER TABLE `category_discount`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `components`
--
ALTER TABLE `components`
  MODIFY `component_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `component_category`
--
ALTER TABLE `component_category`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `configurable_attribute_options`
--
ALTER TABLE `configurable_attribute_options`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `consumables`
--
ALTER TABLE `consumables`
  MODIFY `consumable_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `customer`
--
ALTER TABLE `customer`
  MODIFY `customer_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `customer_advertisement_emails`
--
ALTER TABLE `customer_advertisement_emails`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `customer_loyalty_progress_order`
--
ALTER TABLE `customer_loyalty_progress_order`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `customer_rewards`
--
ALTER TABLE `customer_rewards`
  MODIFY `reward_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `delays`
--
ALTER TABLE `delays`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT COMMENT 'Do not delete records, place then as disabled instead';

--
-- AUTO_INCREMENT pour la table `deletion_reasons`
--
ALTER TABLE `deletion_reasons`
  MODIFY `deletion_reason_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `delivery_position`
--
ALTER TABLE `delivery_position`
  MODIFY `id` bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `delivery_session`
--
ALTER TABLE `delivery_session`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `discounts_products`
--
ALTER TABLE `discounts_products`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `discounts_schedules`
--
ALTER TABLE `discounts_schedules`
  MODIFY `schedule_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `employment_contract`
--
ALTER TABLE `employment_contract`
  MODIFY `contract_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `expiration_dates`
--
ALTER TABLE `expiration_dates`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `extra`
--
ALTER TABLE `extra`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `firebase_fcm_access_token`
--
ALTER TABLE `firebase_fcm_access_token`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `floors`
--
ALTER TABLE `floors`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `floor_areas`
--
ALTER TABLE `floor_areas`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `integration_deliveroo_attributes_mapping`
--
ALTER TABLE `integration_deliveroo_attributes_mapping`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `integration_deliveroo_options_mapping`
--
ALTER TABLE `integration_deliveroo_options_mapping`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `integration_uber_eats_attributes_mapping`
--
ALTER TABLE `integration_uber_eats_attributes_mapping`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `integration_uber_eats_components_mapping`
--
ALTER TABLE `integration_uber_eats_components_mapping`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `integration_uber_eats_options_mapping`
--
ALTER TABLE `integration_uber_eats_options_mapping`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `integration_uber_eats_products_mapping`
--
ALTER TABLE `integration_uber_eats_products_mapping`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `invoices`
--
ALTER TABLE `invoices`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `labels`
--
ALTER TABLE `labels`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `locations`
--
ALTER TABLE `locations`
  MODIFY `location_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `merchant`
--
ALTER TABLE `merchant`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `merchant_code`
--
ALTER TABLE `merchant_code`
  MODIFY `code_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `merchant_marketing_settings`
--
ALTER TABLE `merchant_marketing_settings`
  MODIFY `id` bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `notifications`
--
ALTER TABLE `notifications`
  MODIFY `notification_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `orderitems`
--
ALTER TABLE `orderitems`
  MODIFY `order_item_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `orders`
--
ALTER TABLE `orders`
  MODIFY `order_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `order_changes_log`
--
ALTER TABLE `order_changes_log`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `order_comments`
--
ALTER TABLE `order_comments`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `order_item_configuration`
--
ALTER TABLE `order_item_configuration`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `order_ratings`
--
ALTER TABLE `order_ratings`
  MODIFY `id` bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `packages`
--
ALTER TABLE `packages`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `payments`
--
ALTER TABLE `payments`
  MODIFY `payment_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `pictures`
--
ALTER TABLE `pictures`
  MODIFY `picture_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `planned_shifts`
--
ALTER TABLE `planned_shifts`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `planning_roles`
--
ALTER TABLE `planning_roles`
  MODIFY `role_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `productcateg`
--
ALTER TABLE `productcateg`
  MODIFY `categ_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `products`
--
ALTER TABLE `products`
  MODIFY `product_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `product_ratings`
--
ALTER TABLE `product_ratings`
  MODIFY `id` bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `purchased_components`
--
ALTER TABLE `purchased_components`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `qrcodes`
--
ALTER TABLE `qrcodes`
  MODIFY `QR_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `recipes`
--
ALTER TABLE `recipes`
  MODIFY `recipe_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `requires`
--
ALTER TABLE `requires`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `restaurant_ticket`
--
ALTER TABLE `restaurant_ticket`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `services_performed`
--
ALTER TABLE `services_performed`
  MODIFY `id` int(25) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `shift_templates`
--
ALTER TABLE `shift_templates`
  MODIFY `shift_template_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `shift_templates_items`
--
ALTER TABLE `shift_templates_items`
  MODIFY `item_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `stock_movements_desc`
--
ALTER TABLE `stock_movements_desc`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `stock_movements_source`
--
ALTER TABLE `stock_movements_source`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `stripe_payments`
--
ALTER TABLE `stripe_payments`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `subscriptions`
--
ALTER TABLE `subscriptions`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `subscription_invoices`
--
ALTER TABLE `subscription_invoices`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `sub_cash_registers`
--
ALTER TABLE `sub_cash_registers`
  MODIFY `sub_cash_register_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `tva_categories`
--
ALTER TABLE `tva_categories`
  MODIFY `tva_id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `unit_of_measure`
--
ALTER TABLE `unit_of_measure`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `users_nfc_tags`
--
ALTER TABLE `users_nfc_tags`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `users_rights`
--
ALTER TABLE `users_rights`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `user_vacations`
--
ALTER TABLE `user_vacations`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT pour la table `without`
--
ALTER TABLE `without`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;

-- --------------------------------------------------------

--
-- Structure de la vue `user_status_view`
--
DROP TABLE IF EXISTS `user_status_view`;

CREATE ALGORITHM=UNDEFINED DEFINER=`u231520952_root`@`127.0.0.1` SQL SECURITY DEFINER VIEW `user_status_view`  AS SELECT `u`.`user_id` AS `user_id`, `u`.`first_name` AS `first_name`, `u`.`last_name` AS `last_name`, `u`.`lat` AS `lat`, `u`.`lng` AS `lng`, `u`.`heading` AS `heading`, CASE WHEN `u`.`enabled` = 0 THEN 'DISABLED' WHEN `ds`.`id` is not null AND `ds`.`status` in ('1','PENDING','active') THEN 'IN_DELIVERY_SESSION' WHEN exists(select 1 from `user_vacations` `v` where `v`.`user_id` = `u`.`user_id` AND current_timestamp() between `v`.`start_date` and `v`.`end_date` limit 1) THEN 'VACATIONS' ELSE 'AVAILABLE' END AS `status` FROM (`users` `u` left join `delivery_session` `ds` on(`ds`.`user_id` = `u`.`user_id` and `ds`.`status` in ('1','PENDING','active'))) ;

--
-- Contraintes pour les tables déchargées
--

--
-- Contraintes pour la table `merchant_translation_languages`
--
ALTER TABLE `merchant_translation_languages`
  ADD CONSTRAINT `merchant_translation_languages_ibfk_1` FOREIGN KEY (`lang_code`) REFERENCES `available_languages` (`code`);

--
-- Contraintes pour la table `product_ratings`
--
ALTER TABLE `product_ratings`
  ADD CONSTRAINT `fk_product_ratings_order_rating` FOREIGN KEY (`order_rating_id`) REFERENCES `order_ratings` (`id`) ON DELETE CASCADE;
COMMIT;

/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;

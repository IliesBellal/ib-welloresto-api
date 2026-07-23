#!/usr/bin/env python3
"""Transform MySQL exports for Postgres loading.

The tool stays offline: it reads the target schema file, infers columns that need
special treatment, and generates ready-to-run Postgres SQL (or, for the legacy
"transform" command, rewrites CSV rows) accordingly. The main path is
generate-all-sql: it streams a phpMyAdmin/mysqldump SQL dump and emits one
numbered, transaction-wrapped INSERT .sql file per included table, with NULL
kept as the native SQL keyword throughout (never an empty string).
"""

from __future__ import annotations

import argparse
import csv
import heapq
import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterator, List, Optional, Sequence, Tuple


DEFAULT_SCHEMA = Path("docs/migration-postgres/04-schema-postgres-target.sql")
DEFAULT_MERCHANT_REPORT = Path("docs/migration-postgres/13-merchant-id-schema-update.md")
DEFAULT_BATCH_SIZE = 500

# Sentinel identity values known from migration docs.
SENTINEL_IDENTITY_RULES: Dict[Tuple[str, str], str] = {
    ("tva_categories", "tva_id"): "-1",
}

# MySQL non-strict mode stores the invalid placeholder "0000-00-00" (date) /
# "0000-00-00 00:00:00" (datetime) for an empty date/datetime column instead of
# NULL. Postgres `date`/`timestamptz` reject both forms outright - see
# docs/migration-postgres/36-full-data-load-rehearsal.md (discovery) and
# docs/migration-postgres/37-zero-date-sentinel-audit.md (per-column audit).
# Scoped to the exact (table, column) pairs audited there - NOT applied
# globally - so an un-audited column that happens to share a name is left
# untouched.
ZERO_DATE_SENTINELS: frozenset = frozenset({"0000-00-00", "0000-00-00 00:00:00"})

# Nullable target column: sentinel becomes the native SQL NULL keyword.
ZERO_DATE_TO_NULL_COLUMNS: frozenset = frozenset({
    ("cash_registers", "end_date"),
    ("customer", "customer_birthdate"),
    ("merchant_marketing_settings", "created_at"),
    ("merchant_marketing_settings", "updated_at"),
    ("orders", "estimated_ready"),
    ("users", "dob"),
})

# NOT NULL target column (doc 37): NULL is not an option, so the sentinel
# becomes an explicit epoch placeholder instead of being guessed at.
ZERO_DATE_TO_EPOCH_COLUMNS: frozenset = frozenset({
    ("merchant_parameters", "last_menu_update"),
    ("integration_uber_eats", "pos_provisionning_token_expiration_date"),
})

ZERO_DATE_EPOCH_LITERAL = "1970-01-01T00:00:00Z"

# A NULL that lands on a NOT NULL target column outside the zero-date sentinel
# case above (i.e. a real NULL in the source, not a MySQL non-strict date
# artifact) has no natural literal substitute - the row itself is dropped
# instead of being written malformed. Scoped to the exact (table, column) pairs
# decided in docs/migration-postgres/38-full-data-load-rehearsal-v2.md (2 rows,
# orderitems.order_id: order-less order items with no sensible order to attach
# to) - NOT applied globally.
ROW_DROP_IF_NULL_COLUMNS: frozenset = frozenset({
    ("orderitems", "order_id"),
})

# A NULL on these NOT NULL boolean columns is treated as "unset", coerced to
# FALSE (the column's own DEFAULT) at generation time rather than dropping the
# row - see docs/migration-postgres/38-full-data-load-rehearsal-v2.md (8 rows
# each, both columns on the same rows: settings created before these columns
# existed on the MySQL side, never backfilled). Scoped to the exact
# (table, column) pairs decided there - NOT applied globally.
NULL_TO_FALSE_COLUMNS: frozenset = frozenset({
    ("scannorder_settings", "takeaway_auto_accept"),
    ("scannorder_settings", "delivery_auto_accept"),
})

# MySQL reserves ENUM index 0 for an implicit "error" pseudo-value, rendered as the
# empty string '' - distinct from any declared label - and silently stored there in
# non-strict SQL mode instead of rejecting an insert whose source value matched none
# of the declared labels. See docs/migration-postgres/41-enum-empty-value-audit.md
# (7 rows, planning_shifts.status): the Go application itself can't produce '' on
# this column (falls back to the same default at creation, ignores blanks on
# update), so '' is mapped to that same default at generation time. Scoped to the
# exact (table, column) pair audited there - NOT applied to any other ENUM column.
EMPTY_ENUM_DEFAULT_COLUMNS: Dict[Tuple[str, str], str] = {
    ("planning_shifts", "status"): "planned",
}

# Postgres numeric type prefixes: values classified this way are emitted as bare,
# unquoted SQL literals. Everything else (varchar/text/timestamptz/date/jsonb/enum/...)
# is quoted as a SQL string literal, which Postgres implicitly casts on INSERT.
NUMERIC_TYPE_PREFIXES: Tuple[str, ...] = (
    "integer", "bigint", "smallint", "real", "double", "numeric", "decimal",
    "serial", "bigserial", "float",
)

# Tables excluded from SQL generation: orphan tables confirmed unused by the live
# Go codebase, per docs/migration-postgres/30-final-orphan-tables-list.md, adjusted
# by explicit decisions taken since ("conserver" / "supprimer" annotations):
#   - invoices and hours_amendments are explicitly KEPT (not excluded), despite
#     appearing in the orphan candidate list, per those decisions.
#   - user_vacations was already ruled VIVANTE in doc 30 and was never a candidate.
# 34 tables total: 17 from "orphelines confirmees" (invoices removed), 4 from
# "cas deja tranches", 13 from "cas encore incertains" (hours_amendments removed).
ORPHAN_EXCLUDED_TABLES: frozenset = frozenset({
    # orphelines confirmees (doc 30) - invoices kept out of this set ("conserver")
    "api_calls", "broadcast_list", "calendar", "cash_reports", "consumables",
    "integration_deliveroo_attributes_mapping", "integration_deliveroo_components_mapping",
    "integration_uber_eats_components_mapping", "integration_uber_eats_reports",
    "migration_users", "order_changes_log", "order_ratings", "pictures",
    "product_ratings", "stock_evolution_records", "timezone_info",
    "z_platform_daily_activity_recording",
    # cas deja tranches (doc 30) - orphelines confirmees sans ambiguite
    "average_distribution_time_by_category", "average_distribution_time_history",
    "stock_movements_desc", "stock_movements_source",
    # cas "a discuter" tranches "supprimer" (doc 30) - hours_amendments kept out ("conserver")
    "cash_funds", "category_discount", "checkout_orderitems",
    "customer_advertisement_emails", "employment_agreement", "employment_contract",
    "merchant_code", "notifications", "planned_shifts", "planning_roles",
    "shift_templates", "shift_templates_items", "users_nfc_tags",
})


@dataclass(frozen=True)
class TableInfo:
    name: str
    boolean_columns: Tuple[str, ...]
    merchant_id_columns: Tuple[str, ...]
    identity_columns: Tuple[str, ...]
    sentinel_identity_columns: Tuple[str, ...]
    column_kinds: Dict[str, str]

    @property
    def needs_transformation(self) -> bool:
        return bool(self.boolean_columns or self.merchant_id_columns or self.sentinel_identity_columns)


def _read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def _normalize_column_name(name: str) -> str:
    return name.strip().strip('`"')


def _split_table_blocks(schema_text: str) -> Dict[str, List[str]]:
    lines = schema_text.splitlines()
    tables: Dict[str, List[str]] = {}
    i = 0
    while i < len(lines):
        header = lines[i].strip()
        match = re.match(r"^CREATE TABLE\s+([A-Za-z0-9_]+)\s*\($", header)
        if match:
            table = match.group(1)
            block: List[str] = []
            i += 1
            while i < len(lines) and lines[i].strip() != ");":
                block.append(lines[i])
                i += 1
            tables[table] = block
        i += 1
    return tables


def foreign_keys_from_schema(schema_text: str) -> List[Tuple[str, str]]:
    """Return (child_table, parent_table) pairs for every FOREIGN KEY ... REFERENCES
    constraint in the target schema. There are only 2 in the whole schema."""
    edges: List[Tuple[str, str]] = []
    for table_name, block in _split_table_blocks(schema_text).items():
        for raw_line in block:
            match = re.search(r"FOREIGN KEY\s*\([^)]*\)\s*REFERENCES\s+([A-Za-z0-9_]+)", raw_line, re.IGNORECASE)
            if match:
                edges.append((table_name, match.group(1)))
    return edges


def order_tables_for_load(table_names: List[str], foreign_keys: List[Tuple[str, str]]) -> List[str]:
    """Order tables so referenced (parent) tables load before dependent (child) tables,
    falling back to alphabetical order otherwise (Kahn's algorithm, alphabetical tie-break
    for a deterministic, human-predictable order)."""
    included = set(table_names)
    children: Dict[str, List[str]] = {t: [] for t in included}
    in_degree: Dict[str, int] = {t: 0 for t in included}
    for child, parent in foreign_keys:
        if child in included and parent in included and child != parent:
            children[parent].append(child)
            in_degree[child] += 1

    ready = [t for t in included if in_degree[t] == 0]
    heapq.heapify(ready)
    ordered: List[str] = []
    while ready:
        table = heapq.heappop(ready)
        ordered.append(table)
        for child in children[table]:
            in_degree[child] -= 1
            if in_degree[child] == 0:
                heapq.heappush(ready, child)

    if len(ordered) != len(included):
        # A cycle would be a schema bug, not an input-data problem; fail loud rather
        # than silently drop tables from the load order.
        missing = included - set(ordered)
        raise ValueError(f"Cannot order tables, dependency cycle involves: {sorted(missing)}")

    return ordered


def load_merchant_columns_from_report(report_path: Path) -> Dict[str, Tuple[str, ...]]:
    report_text = _read_text(report_path)
    merchant_columns: Dict[str, List[str]] = {}

    for line in report_text.splitlines():
        # Markdown table row from report 13: | `table` | `column` | ... |
        match = re.match(r"^\|\s*`([^`]+)`\s*\|\s*`([^`]+)`\s*\|", line)
        if not match:
            continue
        table_name = match.group(1).strip()
        column_name = match.group(2).strip()
        merchant_columns.setdefault(table_name, []).append(column_name)

    # Keep stable output and enforce uniqueness.
    normalized: Dict[str, Tuple[str, ...]] = {}
    for table_name, cols in merchant_columns.items():
        deduped = sorted(set(cols), key=str.lower)
        normalized[table_name] = tuple(deduped)

    return normalized


def _classify_column_kind(column_type: str) -> str:
    if column_type == "boolean":
        return "boolean"
    if column_type == "bytea":
        return "bytea"
    if column_type.startswith(NUMERIC_TYPE_PREFIXES):
        return "numeric"
    return "text"


def load_schema(schema_path: Path, merchant_report_path: Path) -> Dict[str, TableInfo]:
    schema_text = _read_text(schema_path)
    merchant_columns_map = load_merchant_columns_from_report(merchant_report_path)
    tables = _split_table_blocks(schema_text)
    result: Dict[str, TableInfo] = {}

    for table_name, block in tables.items():
        boolean_columns: List[str] = []
        merchant_columns: List[str] = []
        identity_columns: List[str] = []
        sentinel_identity_columns: List[str] = []
        column_kinds: Dict[str, str] = {}

        for raw_line in block:
            line = raw_line.strip().rstrip(",")
            if not line or line.startswith("--"):
                continue

            match = re.match(r'^[`"]?([A-Za-z0-9_]+)[`"]?\s+([A-Za-z0-9_()]+)(.*)$', line)
            if not match:
                continue

            column = _normalize_column_name(match.group(1))
            column_type = match.group(2).lower()
            remainder = match.group(3).lower()

            column_kinds[column.lower()] = _classify_column_kind(column_type)

            if column_type == "boolean":
                boolean_columns.append(column)
            if "generated always as identity" in remainder:
                identity_columns.append(column)
                if (table_name, column) in SENTINEL_IDENTITY_RULES:
                    sentinel_identity_columns.append(column)

        # Merchant columns are intentionally sourced from report 13 (72 columns).
        if table_name in merchant_columns_map:
            # Keep the original report ordering, only filtering to columns that exist in table block.
            present_columns = {
                _normalize_column_name(re.match(r'^[`"]?([A-Za-z0-9_]+)[`"]?\s+', line.strip().rstrip(",")).group(1))
                for line in block
                if re.match(r'^[`"]?([A-Za-z0-9_]+)[`"]?\s+', line.strip().rstrip(","))
            }
            merchant_columns = [col for col in merchant_columns_map[table_name] if col in present_columns]

        result[table_name] = TableInfo(
            name=table_name,
            boolean_columns=tuple(boolean_columns),
            merchant_id_columns=tuple(merchant_columns),
            identity_columns=tuple(identity_columns),
            sentinel_identity_columns=tuple(sentinel_identity_columns),
            column_kinds=column_kinds,
        )

    return result


def _bool_to_pg(value: str) -> str:
    if value is None:
        return ""
    text = value.strip()
    if text == "":
        return ""
    lowered = text.lower()
    if lowered in {"1", "true", "t", "yes", "y"}:
        return "true"
    if lowered in {"0", "false", "f", "no", "n"}:
        return "false"
    return value


def _bool_to_sql_literal(value: str) -> str:
    """Render a MySQL 0/1-style boolean value as a native SQL TRUE/FALSE literal.

    Fails loudly on anything unrecognized rather than guessing: silently
    misreading a boolean flag is worse than stopping the run.
    """
    lowered = value.strip().lower()
    if lowered in {"1", "true", "t", "yes", "y"}:
        return "TRUE"
    if lowered in {"0", "false", "f", "no", "n"}:
        return "FALSE"
    raise ValueError(f"Unrecognized boolean literal for SQL generation: {value!r}")


def _sql_quote_string(text: str) -> str:
    """Quote a value as a Postgres string literal, doubling embedded single quotes.

    Postgres uses standard_conforming_strings (the default since 9.1), so a plain
    '...' literal treats backslashes as literal characters - only the quote
    character itself needs escaping.
    """
    return "'" + text.replace("'", "''") + "'"


def _bytea_literal(value: str) -> str:
    """Convert a MySQL 0x-hex binary literal into a Postgres bytea hex literal.

    Only the 0x-hex form is supported: it is the only form observed in the real
    export for the one bytea column in the target schema (kiosks.admin_pin_encrypted).
    Any other form fails loudly instead of risking silent corruption of binary/
    encrypted data.
    """
    text = value.strip()
    if not (text.lower().startswith("0x") and len(text) > 2):
        raise ValueError(f"Unsupported bytea source literal form (expected 0x-hex): {text[:2]!r}...")
    hex_digits = text[2:]
    return "'\\x" + hex_digits + "'"


@dataclass(frozen=True)
class SqlFieldRules:
    """Precomputed per-table lookups so formatting a row never re-derives them."""

    boolean_fields: frozenset
    merchant_fields: frozenset
    column_kinds: Dict[str, str]
    zero_date_to_null_fields: frozenset
    zero_date_to_epoch_fields: frozenset
    null_to_false_fields: frozenset
    empty_enum_default_fields: Dict[str, str]

    @staticmethod
    def from_table_info(table_info: TableInfo) -> "SqlFieldRules":
        return SqlFieldRules(
            boolean_fields=frozenset(c.lower() for c in table_info.boolean_columns),
            merchant_fields=frozenset(c.lower() for c in table_info.merchant_id_columns),
            column_kinds=table_info.column_kinds,
            zero_date_to_null_fields=frozenset(
                col for (tbl, col) in ZERO_DATE_TO_NULL_COLUMNS if tbl == table_info.name
            ),
            zero_date_to_epoch_fields=frozenset(
                col for (tbl, col) in ZERO_DATE_TO_EPOCH_COLUMNS if tbl == table_info.name
            ),
            null_to_false_fields=frozenset(
                col for (tbl, col) in NULL_TO_FALSE_COLUMNS if tbl == table_info.name
            ),
            empty_enum_default_fields={
                col.lower(): default
                for (tbl, col), default in EMPTY_ENUM_DEFAULT_COLUMNS.items()
                if tbl == table_info.name
            },
        )


def format_sql_value(field_name: str, value: Optional[str], rules: SqlFieldRules) -> str:
    """Render one parsed dump field as a native Postgres SQL literal.

    NULL stays the bare SQL keyword (never '' or a quoted empty string) so the
    NULL / empty-string distinction from the source data is preserved end to end.
    """
    lowered = field_name.lower()

    if value is None:
        # NOT NULL boolean columns explicitly decided to coerce NULL -> FALSE at
        # generation (doc 38), checked before the generic NULL passthrough so it
        # can't be shadowed by anything below.
        if lowered in rules.null_to_false_fields:
            return "FALSE"
        return "NULL"

    # Zero-date sentinel: scoped to the exact (table, column) pairs audited in
    # doc 37, checked before any other rule so it can't be shadowed by the
    # generic text-quoting fallback below.
    if value.strip() in ZERO_DATE_SENTINELS:
        if lowered in rules.zero_date_to_null_fields:
            return "NULL"
        if lowered in rules.zero_date_to_epoch_fields:
            return _sql_quote_string(ZERO_DATE_EPOCH_LITERAL)

    # Empty-string ENUM sentinel: scoped to the exact (table, column) pair audited in
    # doc 41, checked before any other rule so it can't be shadowed by the generic
    # text-quoting fallback below (which would otherwise emit '' verbatim - not a
    # valid label for a Postgres enum type).
    if value.strip() == "" and lowered in rules.empty_enum_default_fields:
        return _sql_quote_string(rules.empty_enum_default_fields[lowered])

    if lowered in rules.boolean_fields:
        return _bool_to_sql_literal(value)
    if lowered in rules.merchant_fields:
        return _sql_quote_string(value.strip())

    kind = rules.column_kinds.get(lowered, "text")
    if kind == "bytea":
        return _bytea_literal(value)
    if kind == "numeric":
        text = value.strip()
        return text if text != "" else "NULL"
    return _sql_quote_string(value)


def inspect_table(schema: Dict[str, TableInfo], table_name: str) -> Dict[str, object]:
    info = schema.get(table_name)
    if info is None:
        raise KeyError(f"Unknown table: {table_name}")
    return {
        "table": info.name,
        "needs_transformation": info.needs_transformation,
        "boolean_columns": list(info.boolean_columns),
        "merchant_id_columns": list(info.merchant_id_columns),
        "identity_columns": list(info.identity_columns),
        "sentinel_identity_columns": list(info.sentinel_identity_columns),
        "requires_overriding_system_value": bool(info.sentinel_identity_columns),
    }


def transform_csv(input_path: Path, output_path: Path, table_info: TableInfo) -> Dict[str, object]:
    with input_path.open("r", encoding="utf-8-sig", newline="") as source, output_path.open(
        "w", encoding="utf-8", newline=""
    ) as target:
        reader = csv.DictReader(source)
        if reader.fieldnames is None:
            raise ValueError("CSV input has no header row")

        writer = csv.DictWriter(target, fieldnames=reader.fieldnames, lineterminator="\n")
        writer.writeheader()

        boolean_fields = {column.lower() for column in table_info.boolean_columns}
        merchant_fields = {column.lower() for column in table_info.merchant_id_columns}
        sentinel_rules = {
            column.lower(): SENTINEL_IDENTITY_RULES[(table_info.name, column)]
            for column in table_info.sentinel_identity_columns
            if (table_info.name, column) in SENTINEL_IDENTITY_RULES
        }

        total_rows = 0
        sentinel_hits = 0

        for row in reader:
            total_rows += 1
            transformed = {}
            for field_name in reader.fieldnames:
                value = row.get(field_name, "")
                if field_name.lower() in boolean_fields:
                    transformed[field_name] = _bool_to_pg(value)
                elif field_name.lower() in merchant_fields:
                    transformed[field_name] = "" if value is None else str(value).strip()
                else:
                    transformed[field_name] = value

                sentinel_value = sentinel_rules.get(field_name.lower())
                if sentinel_value is not None and transformed[field_name].strip() == sentinel_value:
                    sentinel_hits += 1

            writer.writerow(transformed)

    return {
        "rows": total_rows,
        "sentinel_identity_hits": sentinel_hits,
        "requires_overriding_system_value": sentinel_hits > 0,
    }


def _parse_sql_string_literal(s: str, i: int) -> Tuple[str, int]:
    """Decode a single-quoted MySQL string literal starting at s[i] == "'".

    Returns the decoded value and the index just past the closing quote.
    Handles backslash escaping (mysqldump/phpMyAdmin default, NO_BACKSLASH_ESCAPES
    not set in this export) as well as doubled '' quoting for robustness.
    """
    assert s[i] == "'"
    i += 1
    out: List[str] = []
    n = len(s)
    escape_map = {
        "0": "\0", "'": "'", '"': '"', "b": "\b", "n": "\n",
        "r": "\r", "t": "\t", "Z": "\x1a", "\\": "\\",
    }
    while i < n:
        c = s[i]
        if c == "\\" and i + 1 < n:
            out.append(escape_map.get(s[i + 1], s[i + 1]))
            i += 2
            continue
        if c == "'":
            if i + 1 < n and s[i + 1] == "'":
                out.append("'")
                i += 2
                continue
            return "".join(out), i + 1
        out.append(c)
        i += 1
    raise ValueError("Unterminated string literal in SQL row tuple")


def parse_row_values(row_text: str) -> List[Optional[str]]:
    """Parse one VALUES tuple line, e.g. "(1,'a','b\\'c',NULL)," into raw field values.

    NULL maps to None. Unquoted tokens (numbers, hex literals) are returned as
    their raw text. This is a streaming, quote-aware tokenizer, not a naive
    comma split, so it stays correct across embedded commas/parens in strings.
    """
    s = row_text.strip()
    if s.endswith(",") or s.endswith(";"):
        s = s[:-1]
    s = s.strip()
    if not (s.startswith("(") and s.endswith(")")):
        raise ValueError("Row does not look like a VALUES tuple")
    s = s[1:-1]

    values: List[Optional[str]] = []
    i = 0
    n = len(s)
    while i < n:
        while i < n and s[i] in " \t":
            i += 1
        if i >= n:
            break
        if s[i] == "'":
            value, i = _parse_sql_string_literal(s, i)
            values.append(value)
        else:
            start = i
            while i < n and s[i] != ",":
                i += 1
            token = s[start:i].strip()
            values.append(None if token.upper() == "NULL" else token)
        while i < n and s[i] in " \t":
            i += 1
        if i < n and s[i] == ",":
            i += 1
    return values


_DUMP_CREATE_TABLE_RE = re.compile(r"^CREATE TABLE `(\w+)`\s*\($", re.IGNORECASE)
_DUMP_COLUMN_LINE_RE = re.compile(r"^`(\w+)`\s")
_DUMP_BLOCK_KEYWORDS = ("PRIMARY KEY", "UNIQUE KEY", "KEY ", "CONSTRAINT", "FOREIGN KEY", "FULLTEXT")


def parse_dump_create_tables(dump_path: Path) -> Dict[str, Tuple[str, ...]]:
    """Extract table name -> ordered column names straight from the dump's own
    CREATE TABLE statements (not the Postgres target schema).

    This is the source of truth for a table's source columns, independent of
    whether any INSERT rows exist for it - needed so that zero-row included
    tables (e.g. hours_amendments in this export) still get a correctly shaped
    (empty) output file instead of being skipped for lack of an INSERT header.
    """
    tables: Dict[str, Tuple[str, ...]] = {}
    current_table: Optional[str] = None
    current_columns: List[str] = []
    with dump_path.open("r", encoding="utf-8") as f:
        for line in f:
            stripped = line.strip()
            if current_table is None:
                match = _DUMP_CREATE_TABLE_RE.match(stripped)
                if match:
                    current_table = match.group(1)
                    current_columns = []
                continue
            if stripped.startswith(")"):
                # phpMyAdmin/mysqldump closes with ") ENGINE=... COLLATE=...;", not
                # a bare ");" - keys/indexes are added later via separate ALTER TABLE
                # statements, so every line in between is a column definition.
                tables[current_table] = tuple(current_columns)
                current_table = None
                continue
            column_match = _DUMP_COLUMN_LINE_RE.match(stripped)
            if column_match and not stripped.upper().startswith(_DUMP_BLOCK_KEYWORDS):
                current_columns.append(column_match.group(1))
    return tables


_INSERT_HEADER_RE = re.compile(r"^INSERT INTO `(\w+)`\s*\(([^)]*)\)\s*VALUES", re.IGNORECASE)


def iter_dump_rows(dump_path: Path) -> "Iterator[Tuple[str, Tuple[str, ...], List[Optional[str]]]]":
    """Stream (table_name, columns, values) for every data row in a phpMyAdmin/mysqldump SQL export.

    Reads the file line by line (no full-file buffering) since real exports can be
    hundreds of MB. Column names come from the INSERT INTO header on each batch,
    so the mapping stays correct even when a table is split across many INSERT
    statements.
    """
    current_table: Optional[str] = None
    current_columns: Tuple[str, ...] = ()
    with dump_path.open("r", encoding="utf-8") as f:
        for line in f:
            stripped = line.strip()
            if not stripped:
                continue
            header_match = _INSERT_HEADER_RE.match(stripped)
            if header_match:
                current_table = header_match.group(1)
                current_columns = tuple(
                    _normalize_column_name(c) for c in header_match.group(2).split(",")
                )
                continue
            if stripped.startswith("(") and current_table is not None:
                values = parse_row_values(stripped)
                if len(values) != len(current_columns):
                    raise ValueError(
                        f"Column/value count mismatch in table {current_table}: "
                        f"{len(current_columns)} columns vs {len(values)} values"
                    )
                yield current_table, current_columns, values


def inspect_dump(dump_path: Path, schema: Dict[str, TableInfo]) -> Dict[str, object]:
    """Structural-only pass over the dump: row counts per table and transform needs.

    Never returns field values, only table/column names and counts, so it is
    safe to print or log even when the dump contains real production data.
    """
    row_counts: Dict[str, int] = {}
    columns_by_table: Dict[str, Tuple[str, ...]] = {}
    for table, columns, _values in iter_dump_rows(dump_path):
        row_counts[table] = row_counts.get(table, 0) + 1
        columns_by_table[table] = columns

    tables_report = []
    for table in sorted(row_counts):
        info = schema.get(table)
        tables_report.append(
            {
                "table": table,
                "rows": row_counts[table],
                "columns": list(columns_by_table[table]),
                "known_in_target_schema": info is not None,
                "needs_transformation": info.needs_transformation if info else None,
            }
        )

    return {
        "dump": str(dump_path),
        "tables_with_data": len(row_counts),
        "total_rows": sum(row_counts.values()),
        "unmapped_tables": sorted(t for t in row_counts if t not in schema),
        "tables": tables_report,
    }


def filter_columns_to_schema(
    table_info: TableInfo, source_columns: Tuple[str, ...]
) -> Tuple[Tuple[str, ...], Tuple[str, ...]]:
    """Split a table's source (dump) columns into (kept, dropped) against the target
    schema's declared columns.

    Some source columns are deliberately not carried over to Postgres (e.g. dead
    columns removed from the target schema - see 35-dead-columns-removal.md). Those
    must be dropped from the generated INSERT rather than either blocking the whole
    table or being inserted into a column that no longer exists on the target side.
    """
    kept = tuple(c for c in source_columns if c.lower() in table_info.column_kinds)
    dropped = tuple(c for c in source_columns if c.lower() not in table_info.column_kinds)
    return kept, dropped


class SqlTableWriter:
    """Buffers rows for one table and emits batched, transaction-wrapped INSERTs.

    NULL is written as the bare SQL keyword throughout - never '' or a quoted
    empty string - so the NULL / empty-string distinction survives end to end,
    unlike the old CSV pipeline where both collapsed to an empty cell.
    """

    def __init__(
        self,
        handle,
        table_name: str,
        source_columns: Tuple[str, ...],
        output_columns: Tuple[str, ...],
        rules: SqlFieldRules,
        setval_columns: Tuple[str, ...],
        batch_size: int = DEFAULT_BATCH_SIZE,
        dropped_columns: Tuple[str, ...] = (),
    ) -> None:
        self.handle = handle
        self.table_name = table_name
        self.columns = output_columns
        # Positions to pull out of each raw dump row: output_columns is a subset of
        # source_columns (same relative order), so this stays a fixed index list
        # computed once instead of a per-row dict lookup.
        self.keep_indices = [source_columns.index(c) for c in output_columns]
        self.rules = rules
        self.setval_columns = setval_columns
        self.needs_overriding = bool(setval_columns)
        self.batch_size = batch_size
        self.dropped_columns = dropped_columns
        self.drop_if_null_fields = frozenset(
            col.lower() for (tbl, col) in ROW_DROP_IF_NULL_COLUMNS if tbl == table_name
        )
        self.dropped_null_key_rows = 0
        self.buffer: List[str] = []
        self.row_count = 0
        self._write_preamble()

    def _write_preamble(self) -> None:
        self.handle.write(f"-- Table: {self.table_name}\n")
        self.handle.write("-- Generated by data-migration/transform_mysql_csv.py (generate-all-sql)\n")
        self.handle.write("-- Structural transform only: booleans -> TRUE/FALSE, merchant_id -> quoted text,\n")
        self.handle.write("-- NULL preserved as the native SQL keyword throughout.\n")
        if self.dropped_columns:
            dropped_list = ", ".join(self.dropped_columns)
            self.handle.write(
                f"-- Source columns not carried over to Postgres (absent from target schema): {dropped_list}\n"
            )
        self.handle.write("BEGIN;\n\n")

    def add_row(self, values: List[Optional[str]]) -> None:
        selected = [values[i] for i in self.keep_indices]
        # Checked on the *formatted* value, not the raw parsed one: a source value
        # does not have to be the literal NULL keyword to end up rendered as the
        # bare SQL NULL token (e.g. a non-numeric source string on a column the
        # target schema declares numeric is passed through unquoted as-is - see
        # docs/migration-postgres/38-full-data-load-rehearsal-v2.md, orderitems.order_id).
        formatted = [format_sql_value(col, val, self.rules) for col, val in zip(self.columns, selected)]
        if self.drop_if_null_fields:
            for col, fval in zip(self.columns, formatted):
                # Case-insensitive: a bare (unquoted) bare token renders as NULL to
                # Postgres regardless of source casing ("null"/"NULL"/"Null" are
                # the same keyword) - see the comment on ROW_DROP_IF_NULL_COLUMNS.
                if col.lower() in self.drop_if_null_fields and fval.upper() == "NULL":
                    self.dropped_null_key_rows += 1
                    return
        self.buffer.append("(" + ", ".join(formatted) + ")")
        self.row_count += 1
        if len(self.buffer) >= self.batch_size:
            self._flush_batch()

    def _flush_batch(self) -> None:
        if not self.buffer:
            return
        column_list = ", ".join(self.columns)
        overriding = " OVERRIDING SYSTEM VALUE" if self.needs_overriding else ""
        self.handle.write(f"INSERT INTO {self.table_name} ({column_list}){overriding} VALUES\n")
        self.handle.write(",\n".join(self.buffer))
        self.handle.write(";\n\n")
        self.buffer = []

    def finalize(self) -> None:
        self._flush_batch()
        self.handle.write("COMMIT;\n")
        for id_column in self.setval_columns:
            self.handle.write(
                f"\nSELECT setval(pg_get_serial_sequence('{self.table_name}', '{id_column}'), "
                f"COALESCE(MAX({id_column}), 1), MAX({id_column}) IS NOT NULL) "
                f"FROM {self.table_name};\n"
            )


def _setval_columns_for(table_info: TableInfo, dump_columns: Tuple[str, ...]) -> Tuple[str, ...]:
    """Identity columns from the target schema that are actually present in this
    table's source columns - i.e. columns we are explicitly inserting a value for,
    which is exactly when OVERRIDING SYSTEM VALUE + a trailing setval() are required.

    Returned in their Postgres catalog form (lower-cased), NOT the dump's original
    casing: none of the target schema's identity columns are declared with double
    quotes, so Postgres folds every one of them to lower case at CREATE TABLE time
    (docs/migration-postgres/04-schema-postgres-target.sql) regardless of how the
    source MySQL dump happened to case them. This matters because the trailing
    setval() below passes the column name as a plain string argument to
    pg_get_serial_sequence(), which - unlike a parsed SQL identifier - is never
    case-folded: a mixed-case name there does a case-sensitive lookup against the
    catalog and misses the (lower-cased) real column, e.g. qrcodes.QR_id -
    see docs/migration-postgres/42-full-data-load-rehearsal-v4.md.
    """
    dump_columns_lower = {c.lower() for c in dump_columns}
    return tuple(
        identity_col.lower()
        for identity_col in table_info.identity_columns
        if identity_col.lower() in dump_columns_lower
    )


def generate_sql_for_table(
    dump_path: Path,
    output_path: Path,
    table_info: TableInfo,
    dump_columns: Tuple[str, ...],
    batch_size: int = DEFAULT_BATCH_SIZE,
) -> Dict[str, object]:
    """Stream one table's rows out of the SQL dump into a ready-to-run Postgres .sql file."""
    rules = SqlFieldRules.from_table_info(table_info)
    output_columns, dropped_columns = filter_columns_to_schema(table_info, dump_columns)
    setval_columns = _setval_columns_for(table_info, output_columns)

    with output_path.open("w", encoding="utf-8", newline="\n") as target:
        writer = SqlTableWriter(
            target, table_info.name, dump_columns, output_columns, rules, setval_columns, batch_size, dropped_columns
        )
        for table, columns, values in iter_dump_rows(dump_path):
            if table != table_info.name:
                continue
            writer.add_row(values)
        writer.finalize()

    return {
        "rows": writer.row_count,
        "overriding_system_value": writer.needs_overriding,
        "setval_columns": list(setval_columns),
        "dropped_source_columns": list(dropped_columns),
        "dropped_null_key_rows": writer.dropped_null_key_rows,
    }


def generate_all_sql(
    dump_path: Path,
    schema: Dict[str, TableInfo],
    output_dir: Path,
    batch_size: int = DEFAULT_BATCH_SIZE,
    schema_text: Optional[str] = None,
) -> Dict[str, object]:
    """Single streaming pass over the whole dump, fanning rows out to one numbered
    .sql file per included table, in dependency-then-alphabetical load order.

    Re-reading a 250MB+ dump once per table would be far slower than one pass with
    multiple open writers, so every row is routed to its table's writer as the file
    streams past. Orphan tables (ORPHAN_EXCLUDED_TABLES) never get a file. Tables
    present in the dump but absent from the target schema are skipped and reported.
    """
    output_dir.mkdir(parents=True, exist_ok=True)

    dump_tables = parse_dump_create_tables(dump_path)
    unmapped_tables = sorted(t for t in dump_tables if t not in schema)
    orphan_tables = sorted(t for t in dump_tables if t in ORPHAN_EXCLUDED_TABLES)
    included_tables = [t for t in dump_tables if t in schema and t not in ORPHAN_EXCLUDED_TABLES]

    if schema_text is None:
        schema_text = _read_text(DEFAULT_SCHEMA)
    foreign_keys = foreign_keys_from_schema(schema_text)
    ordered_tables = order_tables_for_load(included_tables, foreign_keys)

    filenames = {
        table: f"{idx:03d}_{table}.sql" for idx, table in enumerate(ordered_tables, start=1)
    }

    writers: Dict[str, SqlTableWriter] = {}
    handles: Dict[str, object] = {}
    failed: Dict[str, str] = {}
    dropped_columns_by_table: Dict[str, List[str]] = {}

    try:
        for table in ordered_tables:
            info = schema[table]
            dump_columns = dump_tables[table]
            rules = SqlFieldRules.from_table_info(info)
            output_columns, dropped_columns = filter_columns_to_schema(info, dump_columns)
            if dropped_columns:
                dropped_columns_by_table[table] = list(dropped_columns)
            setval_columns = _setval_columns_for(info, output_columns)
            handle = (output_dir / filenames[table]).open("w", encoding="utf-8", newline="\n")
            handles[table] = handle
            writers[table] = SqlTableWriter(
                handle, table, dump_columns, output_columns, rules, setval_columns, batch_size, dropped_columns
            )

        for table, columns, values in iter_dump_rows(dump_path):
            if table in failed:
                continue
            writer = writers.get(table)
            if writer is None:
                continue
            try:
                writer.add_row(values)
            except ValueError as exc:
                # A value that does not fit its declared column kind (e.g. a
                # "boolean" column holding something other than 0/1) is a real
                # source-data/schema-mapping problem, not something to guess
                # around. Stop that one table, but let the other ~130 clean
                # tables finish in the same pass instead of aborting the batch.
                failed[table] = str(exc)

        for table, writer in writers.items():
            if table in failed:
                continue
            writer.finalize()
    finally:
        for handle in handles.values():
            handle.close()

    for table in failed:
        # The partial file has no COMMIT/setval and must not be mistaken for a
        # ready-to-run script.
        (output_dir / filenames[table]).unlink(missing_ok=True)

    succeeded_tables = [t for t in ordered_tables if t not in failed]

    return {
        "output_dir": str(output_dir),
        "tables_generated": len(succeeded_tables),
        "load_order": [filenames[t] for t in succeeded_tables],
        "row_counts": {t: writers[t].row_count for t in succeeded_tables},
        "overriding_system_value_tables": sorted(t for t in succeeded_tables if writers[t].needs_overriding),
        "unmapped_tables_skipped": unmapped_tables,
        "orphan_tables_skipped": orphan_tables,
        "failed_tables": failed,
        "dropped_source_columns_by_table": dropped_columns_by_table,
        "dropped_null_key_rows_by_table": {
            t: writers[t].dropped_null_key_rows
            for t in succeeded_tables
            if writers[t].dropped_null_key_rows > 0
        },
    }


def list_tables(schema: Dict[str, TableInfo], only_transformed: Optional[bool] = None) -> List[str]:
    names = []
    for table_name in sorted(schema):
        info = schema[table_name]
        if only_transformed is None or info.needs_transformation == only_transformed:
            names.append(table_name)
    return names


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Transform MySQL CSV exports for Postgres loads.")
    parser.add_argument("--schema", default=str(DEFAULT_SCHEMA), help="Path to the Postgres target schema SQL file")
    parser.add_argument(
        "--merchant-report",
        default=str(DEFAULT_MERCHANT_REPORT),
        help="Path to report 13 containing the 72 merchant_id columns",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    inspect_parser = subparsers.add_parser("inspect", help="Inspect one table and report whether it needs transformations")
    inspect_parser.add_argument("--table", required=True, help="Table name to inspect")

    transform_parser = subparsers.add_parser("transform", help="Transform a CSV export for one table")
    transform_parser.add_argument("--table", required=True, help="Table name to transform")
    transform_parser.add_argument("--input", required=True, help="Input CSV path")
    transform_parser.add_argument("--output", required=True, help="Output CSV path")

    list_parser = subparsers.add_parser("list", help="List tables by transformation requirement")
    list_parser.add_argument("--mode", choices=["all", "transform", "direct"], default="all")

    inspect_dump_parser = subparsers.add_parser(
        "inspect-dump", help="Structural-only inspection of a phpMyAdmin/mysqldump SQL export"
    )
    inspect_dump_parser.add_argument("--dump", required=True, help="Path to the raw SQL dump file")

    generate_sql_parser = subparsers.add_parser(
        "generate-sql", help="Generate a ready-to-run Postgres INSERT .sql file for one table straight from the SQL dump"
    )
    generate_sql_parser.add_argument("--dump", required=True, help="Path to the raw SQL dump file")
    generate_sql_parser.add_argument("--table", required=True, help="Table name to extract")
    generate_sql_parser.add_argument("--output", required=True, help="Output .sql path")
    generate_sql_parser.add_argument(
        "--batch-size", type=int, default=DEFAULT_BATCH_SIZE, help="Rows per multi-row INSERT statement"
    )

    generate_all_sql_parser = subparsers.add_parser(
        "generate-all-sql",
        help="Generate one numbered, ready-to-run Postgres .sql file per included table in one streaming pass",
    )
    generate_all_sql_parser.add_argument("--dump", required=True, help="Path to the raw SQL dump file")
    generate_all_sql_parser.add_argument(
        "--output-dir", required=True, help="Directory to write one NNN_<table>.sql per included table into"
    )
    generate_all_sql_parser.add_argument(
        "--batch-size", type=int, default=DEFAULT_BATCH_SIZE, help="Rows per multi-row INSERT statement"
    )

    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    schema_path = Path(args.schema)
    merchant_report_path = Path(args.merchant_report)
    schema = load_schema(schema_path, merchant_report_path)

    if args.command == "inspect":
        report = inspect_table(schema, args.table)
        print(json.dumps(report, indent=2, sort_keys=True))
        return 0

    if args.command == "transform":
        info = schema.get(args.table)
        if info is None:
            raise SystemExit(f"Unknown table: {args.table}")
        transform_report = transform_csv(Path(args.input), Path(args.output), info)
        payload = inspect_table(schema, args.table)
        payload.update(transform_report)
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    if args.command == "list":
        if args.mode == "all":
            payload = {
                "transform": list_tables(schema, True),
                "direct": list_tables(schema, False),
            }
        elif args.mode == "transform":
            payload = {"transform": list_tables(schema, True)}
        else:
            payload = {"direct": list_tables(schema, False)}
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    if args.command == "inspect-dump":
        report = inspect_dump(Path(args.dump), schema)
        print(json.dumps(report, indent=2, sort_keys=True))
        return 0

    if args.command == "generate-sql":
        info = schema.get(args.table)
        if info is None:
            raise SystemExit(f"Unknown table: {args.table}")
        dump_path = Path(args.dump)
        dump_columns = parse_dump_create_tables(dump_path).get(args.table)
        if dump_columns is None:
            raise SystemExit(f"Table not found in dump: {args.table}")
        transform_report = generate_sql_for_table(
            dump_path, Path(args.output), info, dump_columns, args.batch_size
        )
        payload = inspect_table(schema, args.table)
        payload.update(transform_report)
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    if args.command == "generate-all-sql":
        report = generate_all_sql(
            Path(args.dump), schema, Path(args.output_dir), args.batch_size, schema_text=_read_text(schema_path)
        )
        print(json.dumps(report, indent=2, sort_keys=True))
        return 0

    parser.error("Unsupported command")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())

package orders

import "strings"

// QueryFilter pairs a SQL fragment (using `?` placeholders) with its arguments,
// in the order the placeholders appear in SQL. Concatenating QueryFilters keeps
// the args slice in sync with the generated SQL, so no value ever gets
// interpolated directly into a query string.
//
// This shape is deliberately the same one sqlx.Rebind()-based Postgres queries
// need (SQL fragment + ordered args) — see docs/migration-postgres/02-security-fix-orders-builder.md.
type QueryFilter struct {
	SQL  string
	Args []interface{}
}

// NewFilter builds a QueryFilter from a SQL fragment and its ordered arguments.
func NewFilter(sql string, args ...interface{}) QueryFilter {
	return QueryFilter{SQL: sql, Args: args}
}

// Append concatenates f and other, preserving SQL/Args order.
func (f QueryFilter) Append(other QueryFilter) QueryFilter {
	args := make([]interface{}, 0, len(f.Args)+len(other.Args))
	args = append(args, f.Args...)
	args = append(args, other.Args...)
	return QueryFilter{SQL: f.SQL + other.SQL, Args: args}
}

// InFilter builds a " AND <column> IN (?,?,...) " fragment for the given values.
// Returns an empty QueryFilter if values is empty — callers must handle the
// "no matching rows" case themselves before calling this.
func InFilter(column string, values []string) QueryFilter {
	if len(values) == 0 {
		return QueryFilter{}
	}
	args := make([]interface{}, len(values))
	placeholders := make([]string, len(values))
	for i, v := range values {
		placeholders[i] = "?"
		args[i] = v
	}
	return QueryFilter{
		SQL:  " AND " + column + " IN (" + strings.Join(placeholders, ",") + ") ",
		Args: args,
	}
}

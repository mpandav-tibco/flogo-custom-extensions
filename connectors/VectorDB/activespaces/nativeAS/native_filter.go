package vectordb

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var identifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateIdentifier ensures a table/field name is a safe SQL identifier,
// preventing SQL injection through collection names or metadata field names.
func validateIdentifier(name string) error {
	if !identifierRe.MatchString(name) {
		return newError(ErrCodeInvalidCollectionName, fmt.Sprintf("invalid identifier %q", name), nil)
	}
	return nil
}

// sqlLiteral renders a Go value as a safe SQL literal (strings single-quoted with
// embedded quotes doubled).
func sqlLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'g', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", t), "'", "''") + "'"
	}
}

// buildFilterSQL converts the connector's provider-agnostic filter map into a SQL
// WHERE clause over the JSON metadata column. Supported per-key forms:
//
//	"field": scalar                  -> json_extract(metadata,'$.field') = scalar
//	"field": [a, b]                   -> ... IN (a, b)
//	"field": {"$gte": 1, "$lt": 10}   -> ... >= 1 AND ... < 10
//	"field": {"$in": [a, b]}          -> ... IN (a, b)
//
// All identifiers are validated and all values are escaped (no SQL injection).
func buildFilterSQL(filters map[string]interface{}) (string, error) {
	if len(filters) == 0 {
		return "", nil
	}
	var parts []string
	for field, v := range filters {
		if err := validateIdentifier(field); err != nil {
			return "", err
		}
		key := fmt.Sprintf("json_extract(metadata, '$.%s')", field)
		switch val := v.(type) {
		case map[string]interface{}:
			for op, ov := range val {
				lop := strings.ToLower(op)
				if lop == "$in" || lop == "in" {
					s, err := inClause(key, ov)
					if err != nil {
						return "", err
					}
					parts = append(parts, s)
					continue
				}
				parts = append(parts, fmt.Sprintf("%s %s %s", key, sqlOp(op), sqlLiteral(ov)))
			}
		case []interface{}:
			s, err := inClause(key, val)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		default:
			parts = append(parts, fmt.Sprintf("%s = %s", key, sqlLiteral(v)))
		}
	}
	return strings.Join(parts, " AND "), nil
}

func inClause(key string, v any) (string, error) {
	arr, ok := v.([]interface{})
	if !ok {
		return "", newError(ErrCodeProviderError, "'in' filter requires an array value", nil)
	}
	items := make([]string, len(arr))
	for i, iv := range arr {
		items[i] = sqlLiteral(iv)
	}
	return fmt.Sprintf("%s IN (%s)", key, strings.Join(items, ", ")), nil
}

func sqlOp(op string) string {
	switch strings.ToLower(op) {
	case "$ne", "ne":
		return "<>"
	case "$gt", "gt":
		return ">"
	case "$gte", "gte":
		return ">="
	case "$lt", "lt":
		return "<"
	case "$lte", "lte":
		return "<="
	case "$like", "like":
		return "LIKE"
	default:
		return "="
	}
}

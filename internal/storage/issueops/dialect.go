package issueops

// SQLDialect captures the small expression differences between Dolt/MySQL SQL
// and SQLite-compatible engines such as doltlite.
type SQLDialect int

const (
	SQLDialectDolt SQLDialect = iota
	SQLDialectSQLite
)

func (d SQLDialect) CurrentTimestamp() string {
	if d == SQLDialectSQLite {
		return "CURRENT_TIMESTAMP"
	}
	return "UTC_TIMESTAMP()"
}

func (d SQLDialect) RecentCreatedAtExpr() string {
	if d == SQLDialectSQLite {
		return "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-48 hours')"
	}
	return "DATE_SUB(NOW(), INTERVAL 48 HOUR)"
}

func (d SQLDialect) ChildIDLikeExpr() string {
	if d == SQLDialectSQLite {
		return "id LIKE (? || '.%')"
	}
	return "id LIKE CONCAT(?, '.%')"
}

func (d SQLDialect) MetadataEqualsExpr() string {
	if d == SQLDialectSQLite {
		return "json_extract(metadata, ?) = ?"
	}
	return "JSON_UNQUOTE(JSON_EXTRACT(metadata, ?)) = ?"
}

func (d SQLDialect) MetadataExistsExpr() string {
	if d == SQLDialectSQLite {
		return "json_extract(metadata, ?) IS NOT NULL"
	}
	return "JSON_EXTRACT(metadata, ?) IS NOT NULL"
}

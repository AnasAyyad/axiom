package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type tableGrant struct {
	privileges string
	tables     []string
}

func existingPublicTables(
	ctx context.Context,
	tx pgx.Tx,
) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, `SELECT relation.relname
FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = 'public' AND relation.relkind IN ('r','p','v','m','f')`)
	if err != nil {
		return nil, fmt.Errorf("role_table_lookup_failed")
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("role_table_lookup_failed")
		}
		result[table] = struct{}{}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("role_table_lookup_failed")
	}
	return result, nil
}

func filterTableGrants(
	grants []tableGrant,
	available map[string]struct{},
) []tableGrant {
	result := make([]tableGrant, 0, len(grants))
	for _, grant := range grants {
		tables := make([]string, 0, len(grant.tables))
		for _, table := range grant.tables {
			if _, exists := available[table]; exists {
				tables = append(tables, table)
			}
		}
		if len(tables) > 0 {
			result = append(result, tableGrant{
				privileges: grant.privileges,
				tables:     tables,
			})
		}
	}
	return result
}

func validDistinctRoles(roles []string) bool {
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if !roleNamePattern.MatchString(role) {
			return false
		}
		seen[role] = struct{}{}
	}
	return len(seen) == len(roles)
}

func applyTableGrants(
	ctx context.Context,
	tx pgx.Tx,
	roleName string,
	grants []tableGrant,
) error {
	role := pgx.Identifier{roleName}.Sanitize()
	if _, err := tx.Exec(
		ctx,
		"REVOKE ALL ON ALL TABLES IN SCHEMA public FROM "+role,
	); err != nil {
		return fmt.Errorf("role_revoke_failed")
	}
	for _, grant := range grants {
		if _, err := tx.Exec(
			ctx,
			grantSQL(grant.privileges, grant.tables, role),
		); err != nil {
			return fmt.Errorf("role_grant_failed")
		}
	}
	return nil
}

func grantSQL(privileges string, tables []string, role string) string {
	quoted := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted = append(
			quoted,
			pgx.Identifier{"public", table}.Sanitize(),
		)
	}
	return "GRANT " + privileges + " ON " +
		strings.Join(quoted, ", ") + " TO " + role
}

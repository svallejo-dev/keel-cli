package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// PostgresOps operates on a Postgres instance via pgx (host endpoint).
type PostgresOps struct{}

// QueryResult holds the outcome of an SQL statement.
type QueryResult struct {
	Columns      []string
	Rows         [][]string
	RowsAffected int64
	Command      string
}

func (o *PostgresOps) connect(ctx context.Context, inst *domain.Instance, db string) (*pgx.Conn, error) {
	if err := requireReal(inst); err != nil {
		return nil, err
	}
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return nil, fmt.Errorf("no endpoint")
	}
	if db == "" {
		db = defaultDB(inst)
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		inst.Cred.Username, inst.Cred.Password, ep.Host, ep.Port, db)
	return pgx.Connect(ctx, dsn)
}

func defaultDB(inst *domain.Instance) string {
	if db := inst.Cred.Extra["dbname"]; db != "" {
		return db
	}
	return "postgres"
}

// ListDatabases returns non-template databases.
func (o *PostgresOps) ListDatabases(ctx context.Context, inst *domain.Instance) ([]string, error) {
	conn, err := o.connect(ctx, inst, "")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// CreateDatabase creates a new database.
func (o *PostgresOps) CreateDatabase(ctx context.Context, inst *domain.Instance, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	conn, err := o.connect(ctx, inst, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, name))
	return err
}

// DropDatabase deletes a database.
func (o *PostgresOps) DropDatabase(ctx context.Context, inst *domain.Instance, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	conn, err := o.connect(ctx, inst, "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name))
	return err
}

// ListTables lists tables in the public schema of db.
func (o *PostgresOps) ListTables(ctx context.Context, inst *domain.Instance, db string) ([]string, error) {
	conn, err := o.connect(ctx, inst, db)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Exec runs an arbitrary SQL statement, returning rows for queries or the affected
// count for commands.
func (o *PostgresOps) Exec(ctx context.Context, inst *domain.Instance, db, sql string) (*QueryResult, error) {
	conn, err := o.connect(ctx, inst, db)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	res := &QueryResult{}
	for _, fd := range fds {
		res.Columns = append(res.Columns, string(fd.Name))
	}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = formatValue(v)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tag := rows.CommandTag()
	res.RowsAffected = tag.RowsAffected()
	res.Command = tag.String()
	return res, nil
}

func formatValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case time.Time:
		return t.Format(time.RFC3339)
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// validateIdent allows only safe SQL identifier characters.
func validateIdent(name string) error {
	if name == "" {
		return fmt.Errorf("empty identifier")
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !ok {
			return fmt.Errorf("invalid identifier %q (use letters, digits, underscore)", name)
		}
	}
	if strings.ContainsAny(name[:1], "0123456789") {
		return fmt.Errorf("identifier %q cannot start with a digit", name)
	}
	return nil
}

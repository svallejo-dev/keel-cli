package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/ops"
)

func newPgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pg",
		Short: "PostgreSQL (RDS) operations: databases, tables, SQL",
	}
	cmd.AddCommand(pgDBCmd(), pgTablesCmd(), pgSQLCmd())
	return cmd
}

func pgDBCmd() *cobra.Command {
	db := &cobra.Command{Use: "db", Aliases: []string{"database"}, Short: "Manage databases"}

	list := &cobra.Command{
		Use:   "list <instance>",
		Short: "List databases",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindPostgres)
			if err != nil {
				return exitErr(err)
			}
			dbs, err := mgr.Ops().Postgres.ListDatabases(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			for _, d := range dbs {
				fmt.Fprintln(cmd.OutOrStdout(), d)
			}
			return nil
		},
	}

	create := &cobra.Command{
		Use:   "create <instance> <dbname>",
		Short: "Create a database",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindPostgres)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Postgres.CreateDatabase(cmd.Context(), inst, args[1]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s database %s created\n", styleOK.Render("✓"), bold(args[1]))
			return nil
		},
	}

	var yes bool
	drop := &cobra.Command{
		Use:   "drop <instance> <dbname>",
		Short: "Drop a database",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindPostgres)
			if err != nil {
				return exitErr(err)
			}
			if !yes && !confirm(fmt.Sprintf("drop database %q (data lost)?", args[1])) {
				return nil
			}
			if err := mgr.Ops().Postgres.DropDatabase(cmd.Context(), inst, args[1]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s database %s dropped\n", styleOK.Render("✓"), args[1])
			return nil
		},
	}
	drop.Flags().BoolVar(&yes, "yes", false, "skip confirmation")

	db.AddCommand(list, create, drop)
	return db
}

func pgTablesCmd() *cobra.Command {
	var dbName string
	cmd := &cobra.Command{
		Use:   "tables <instance>",
		Short: "List tables in a database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindPostgres)
			if err != nil {
				return exitErr(err)
			}
			tables, err := mgr.Ops().Postgres.ListTables(cmd.Context(), inst, dbName)
			if err != nil {
				return exitErr(err)
			}
			if len(tables) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no tables"))
				return nil
			}
			for _, t := range tables {
				fmt.Fprintln(cmd.OutOrStdout(), t)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dbName, "db", "", "database name (default: instance's default DB)")
	return cmd
}

func pgSQLCmd() *cobra.Command {
	var command, dbName string
	cmd := &cobra.Command{
		Use:   "sql <instance> -c \"SQL\"",
		Short: "Run an SQL statement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if command == "" {
				return exitErr(fmt.Errorf("-c \"SQL\" is required"))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindPostgres)
			if err != nil {
				return exitErr(err)
			}
			res, err := mgr.Ops().Postgres.Exec(cmd.Context(), inst, dbName, command)
			if err != nil {
				return exitErr(err)
			}
			printQueryResult(cmd, res)
			return nil
		},
	}
	cmd.Flags().StringVarP(&command, "command", "c", "", "SQL to execute")
	cmd.Flags().StringVar(&dbName, "db", "", "database name (default: instance's default DB)")
	return cmd
}

func printQueryResult(cmd *cobra.Command, res *ops.QueryResult) {
	out := cmd.OutOrStdout()
	if len(res.Columns) > 0 && len(res.Rows) > 0 {
		tw := newTabWriter(out)
		fmt.Fprintln(tw, heading(strings.Join(res.Columns, "\t")))
		for _, row := range res.Rows {
			fmt.Fprintln(tw, strings.Join(row, "\t"))
		}
		tw.Flush()
		fmt.Fprintf(out, "%s\n", dim(fmt.Sprintf("(%d rows)", len(res.Rows))))
		return
	}
	cmdTag := res.Command
	if cmdTag == "" {
		cmdTag = "OK"
	}
	fmt.Fprintf(out, "%s %s\n", styleOK.Render("✓"), dim(fmt.Sprintf("%s (%d rows affected)", cmdTag, res.RowsAffected)))
}

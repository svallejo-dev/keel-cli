package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newRedisCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redis",
		Short: "Redis (ElastiCache) operations: keys, get/set/del, info",
	}
	cmd.AddCommand(redisKeysCmd(), redisGetCmd(), redisSetCmd(), redisDelCmd(), redisInfoCmd(), redisFlushCmd())
	return cmd
}

func redisKeysCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "keys <instance> [pattern]",
		Short: "Scan keys (default pattern *)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindRedis)
			if err != nil {
				return exitErr(err)
			}
			pattern := "*"
			if len(args) == 2 {
				pattern = args[1]
			}
			keys, err := mgr.Ops().Redis.Scan(cmd.Context(), inst, pattern, limit)
			if err != nil {
				return exitErr(err)
			}
			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no keys"))
				return nil
			}
			for _, k := range keys {
				fmt.Fprintln(cmd.OutOrStdout(), k)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "max keys to return")
	return cmd
}

func redisGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <instance> <key>",
		Short: "Get a key's value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindRedis)
			if err != nil {
				return exitErr(err)
			}
			v, err := mgr.Ops().Redis.Get(cmd.Context(), inst, args[1])
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), v)
			return nil
		},
	}
}

func redisSetCmd() *cobra.Command {
	var ttl string
	cmd := &cobra.Command{
		Use:   "set <instance> <key> <value>",
		Short: "Set a key",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindRedis)
			if err != nil {
				return exitErr(err)
			}
			var d time.Duration
			if ttl != "" {
				d, err = time.ParseDuration(ttl)
				if err != nil {
					return exitErr(fmt.Errorf("invalid --ttl: %w", err))
				}
			}
			if err := mgr.Ops().Redis.Set(cmd.Context(), inst, args[1], args[2], d); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s set %s\n", styleOK.Render("✓"), args[1])
			return nil
		},
	}
	cmd.Flags().StringVar(&ttl, "ttl", "", "expiry, e.g. 30s, 5m, 1h (default: none)")
	return cmd
}

func redisDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "del <instance> <key...>",
		Short: "Delete keys",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindRedis)
			if err != nil {
				return exitErr(err)
			}
			n, err := mgr.Ops().Redis.Del(cmd.Context(), inst, args[1:]...)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s deleted %d key(s)\n", styleOK.Render("✓"), n)
			return nil
		},
	}
}

func redisInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <instance>",
		Short: "Show server INFO",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindRedis)
			if err != nil {
				return exitErr(err)
			}
			info, err := mgr.Ops().Redis.Info(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprint(cmd.OutOrStdout(), info)
			return nil
		},
	}
}

func redisFlushCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "flush <instance>",
		Short: "Remove all keys (FLUSHDB)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindRedis)
			if err != nil {
				return exitErr(err)
			}
			if !yes && !confirm("flush ALL keys in "+args[0]+"?") {
				return nil
			}
			if err := mgr.Ops().Redis.FlushDB(cmd.Context(), inst); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s flushed\n", styleOK.Render("✓"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	return cmd
}

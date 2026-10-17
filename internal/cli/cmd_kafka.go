package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newKafkaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kafka",
		Short: "Kafka (MSK) operations: topics, produce, consume",
	}
	cmd.AddCommand(kafkaTopicCmd(), kafkaProduceCmd(), kafkaConsumeCmd())
	return cmd
}

func kafkaTopicCmd() *cobra.Command {
	topic := &cobra.Command{Use: "topic", Short: "Manage topics"}

	list := &cobra.Command{
		Use:   "list <instance>",
		Short: "List topics",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindKafka)
			if err != nil {
				return exitErr(err)
			}
			topics, err := mgr.Ops().Kafka.ListTopics(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			if len(topics) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no topics"))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("TOPIC\tPARTITIONS\tREPLICAS"))
			for _, t := range topics {
				fmt.Fprintf(tw, "%s\t%d\t%d\n", t.Name, t.Partitions, t.Replicas)
			}
			tw.Flush()
			return nil
		},
	}

	var partitions, replication int
	var configs []string
	create := &cobra.Command{
		Use:   "create <instance> <topic>",
		Short: "Create a topic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindKafka)
			if err != nil {
				return exitErr(err)
			}
			cfg, err := parseParams(configs)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Kafka.CreateTopic(cmd.Context(), inst, args[1], partitions, replication, cfg); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s topic %s created\n", styleOK.Render("✓"), bold(args[1]))
			return nil
		},
	}
	create.Flags().IntVarP(&partitions, "partitions", "p", 1, "number of partitions")
	create.Flags().IntVarP(&replication, "replication", "r", 1, "replication factor")
	create.Flags().StringArrayVarP(&configs, "config", "c", nil, "topic config key=value (repeatable)")

	var yes bool
	del := &cobra.Command{
		Use:   "delete <instance> <topic>",
		Short: "Delete a topic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindKafka)
			if err != nil {
				return exitErr(err)
			}
			if !yes && !confirm(fmt.Sprintf("delete topic %q?", args[1])) {
				return nil
			}
			if err := mgr.Ops().Kafka.DeleteTopic(cmd.Context(), inst, args[1]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s topic %s deleted\n", styleOK.Render("✓"), args[1])
			return nil
		},
	}
	del.Flags().BoolVar(&yes, "yes", false, "skip confirmation")

	describe := &cobra.Command{
		Use:   "describe <instance> <topic>",
		Short: "Describe a topic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindKafka)
			if err != nil {
				return exitErr(err)
			}
			out, err := mgr.Ops().Kafka.DescribeTopic(cmd.Context(), inst, args[1])
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}

	topic.AddCommand(list, create, del, describe)
	return topic
}

func kafkaProduceCmd() *cobra.Command {
	var message, key string
	cmd := &cobra.Command{
		Use:   "produce <instance> <topic>",
		Short: "Produce a message to a topic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return exitErr(fmt.Errorf("--message is required"))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindKafka)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Kafka.Produce(cmd.Context(), inst, args[1], key, message); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s produced to %s\n", styleOK.Render("✓"), args[1])
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "message body")
	cmd.Flags().StringVarP(&key, "key", "k", "", "message key (optional)")
	return cmd
}

func kafkaConsumeCmd() *cobra.Command {
	var n int
	var fromStart bool
	cmd := &cobra.Command{
		Use:   "consume <instance> <topic>",
		Short: "Consume messages from a topic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindKafka)
			if err != nil {
				return exitErr(err)
			}
			recs, err := mgr.Ops().Kafka.Consume(cmd.Context(), inst, args[1], n, fromStart)
			if err != nil {
				return exitErr(err)
			}
			if len(recs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no messages"))
				return nil
			}
			for _, r := range recs {
				fmt.Fprintln(cmd.OutOrStdout(), r)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "num", "n", 10, "max messages to read")
	cmd.Flags().BoolVar(&fromStart, "from-start", false, "read from the beginning")
	return cmd
}

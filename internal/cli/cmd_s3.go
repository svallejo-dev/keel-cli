package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newS3Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "s3",
		Short: "S3 operations: list/put/get/delete objects",
	}
	cmd.AddCommand(s3LsCmd(), s3PutCmd(), s3GetCmd(), s3RmCmd())
	return cmd
}

func s3LsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ls <instance> [prefix]",
		Aliases: []string{"list"},
		Short:   "List objects",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindS3)
			if err != nil {
				return exitErr(err)
			}
			prefix := ""
			if len(args) == 2 {
				prefix = args[1]
			}
			objs, err := mgr.Ops().S3.ListObjects(cmd.Context(), inst, prefix)
			if err != nil {
				return exitErr(err)
			}
			if len(objs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no objects"))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("KEY\tSIZE\tMODIFIED"))
			for _, o := range objs {
				fmt.Fprintf(tw, "%s\t%d\t%s\n", o.Key, o.Size, o.LastModified.Format("2006-01-02 15:04:05"))
			}
			tw.Flush()
			return nil
		},
	}
	return cmd
}

func s3PutCmd() *cobra.Command {
	var body, file string
	cmd := &cobra.Command{
		Use:   "put <instance> <key>",
		Short: "Upload an object (from --body text or --file path)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindS3)
			if err != nil {
				return exitErr(err)
			}
			var data []byte
			switch {
			case file != "":
				data, err = os.ReadFile(file)
				if err != nil {
					return exitErr(err)
				}
			case body != "":
				data = []byte(body)
			default:
				return exitErr(fmt.Errorf("provide --body or --file"))
			}
			if err := mgr.Ops().S3.PutObject(cmd.Context(), inst, args[1], data, ""); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s put %s (%d bytes)\n", styleOK.Render("✓"), args[1], len(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "inline object content")
	cmd.Flags().StringVar(&file, "file", "", "read object content from a file")
	return cmd
}

func s3GetCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "get <instance> <key>",
		Short: "Download an object (to stdout or --out file)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindS3)
			if err != nil {
				return exitErr(err)
			}
			data, err := mgr.Ops().S3.GetObject(cmd.Context(), inst, args[1])
			if err != nil {
				return exitErr(err)
			}
			if out != "" {
				if err := os.WriteFile(out, data, 0o644); err != nil {
					return exitErr(err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s wrote %d bytes to %s\n", styleOK.Render("✓"), len(data), out)
				return nil
			}
			cmd.OutOrStdout().Write(data)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "write to file instead of stdout")
	return cmd
}

func s3RmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rm <instance> <key>",
		Short: "Delete an object",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindS3)
			if err != nil {
				return exitErr(err)
			}
			if !yes && !confirm(fmt.Sprintf("delete object %q?", args[1])) {
				return nil
			}
			if err := mgr.Ops().S3.DeleteObject(cmd.Context(), inst, args[1]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s deleted %s\n", styleOK.Render("✓"), args[1])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	return cmd
}

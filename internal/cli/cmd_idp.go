package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newIdpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "idp",
		Short: "Identity Platform (GCP) operations: users, claims, sign-in tokens",
		Long: `idp manages the users of a gcp-identity instance (Google Cloud Identity Platform,
emulated) and signs in as them to obtain ID tokens for calling your APIs.

Examples:
  keel idp user create auth --email ana@test.com --password secret123
  keel idp user claims auth ana@test.com '{"role":"admin"}'
  keel idp token auth --email ana@test.com --password secret123
  curl -H "Authorization: Bearer $(keel idp token auth -e ana@test.com -p secret123)" localhost:8080/me`,
	}
	user := &cobra.Command{
		Use:   "user",
		Short: "Manage accounts",
	}
	user.AddCommand(idpUserListCmd(), idpUserCreateCmd(), idpUserDeleteCmd(), idpUserClaimsCmd())
	cmd.AddCommand(user, idpTokenCmd(), idpResetCmd())
	return cmd
}

func idpUserListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list <instance>",
		Aliases: []string{"ls"},
		Short:   "List accounts",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindGCPIdentity)
			if err != nil {
				return exitErr(err)
			}
			users, err := mgr.Ops().Identity.ListUsers(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			if len(users) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no users — create one with `keel idp user create`"))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("UID\tEMAIL\tNAME\tVERIFIED\tDISABLED\tCLAIMS"))
			for _, u := range users {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", u.UID, u.Email, u.DisplayName,
					strconv.FormatBool(u.EmailVerified), strconv.FormatBool(u.Disabled), u.CustomClaims)
			}
			return tw.Flush()
		},
	}
}

func idpUserCreateCmd() *cobra.Command {
	var email, password, name string
	cmd := &cobra.Command{
		Use:   "create <instance> --email <e> --password <p>",
		Short: "Create an email/password account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" || password == "" {
				return exitErr(fmt.Errorf("--email and --password are required"))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindGCPIdentity)
			if err != nil {
				return exitErr(err)
			}
			uid, err := mgr.Ops().Identity.CreateUser(cmd.Context(), inst, email, password, name)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", styleOK.Render("✓ user created"), email, dim("uid="+uid))
			return nil
		},
	}
	cmd.Flags().StringVarP(&email, "email", "e", "", "account email")
	cmd.Flags().StringVarP(&password, "password", "p", "", "account password (min 6 chars)")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	return cmd
}

func idpUserDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <instance> <uid|email>",
		Short: "Delete an account",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindGCPIdentity)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Identity.DeleteUser(cmd.Context(), inst, args[1]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", styleOK.Render("✓ user deleted"), args[1])
			return nil
		},
	}
}

func idpUserClaimsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claims <instance> <uid|email> <json>",
		Short: `Set custom claims (e.g. '{"role":"admin"}'; '{}' clears them)`,
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindGCPIdentity)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Identity.SetClaims(cmd.Context(), inst, args[1], args[2]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", styleOK.Render("✓ claims set"), args[1], dim("(in the next ID token)"))
			return nil
		},
	}
}

func idpTokenCmd() *cobra.Command {
	var email, password string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "token <instance> --email <e> --password <p>",
		Short: "Sign in and print the ID token (JWT) to call your APIs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" || password == "" {
				return exitErr(fmt.Errorf("--email and --password are required"))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindGCPIdentity)
			if err != nil {
				return exitErr(err)
			}
			res, err := mgr.Ops().Identity.SignIn(cmd.Context(), inst, email, password)
			if err != nil {
				return exitErr(err)
			}
			if asJSON {
				b, _ := json.MarshalIndent(map[string]string{
					"uid": res.UID, "id_token": res.IDToken, "refresh_token": res.RefreshToken, "expires_in": res.ExpiresIn,
				}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			}
			// Bare token on stdout so it composes: $(keel idp token …).
			fmt.Fprintln(cmd.OutOrStdout(), res.IDToken)
			return nil
		},
	}
	cmd.Flags().StringVarP(&email, "email", "e", "", "account email")
	cmd.Flags().StringVarP(&password, "password", "p", "", "account password")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print uid, ID token, refresh token and expiry as JSON")
	return cmd
}

func idpResetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset <instance>",
		Short: "Delete every account in the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return exitErr(fmt.Errorf("this deletes every user of %s — re-run with --yes to confirm", args[0]))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindGCPIdentity)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Identity.Reset(cmd.Context(), inst); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", styleOK.Render("✓ all users deleted in"), args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm deleting every user")
	return cmd
}

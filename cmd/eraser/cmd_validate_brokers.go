package main

import (
	"fmt"
	"os"

	"github.com/drumandbytes/eraser/data"
	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/spf13/cobra"
)

func validateBrokersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate-brokers [file]",
		Short: "Check a brokers.yaml for structural problems",
		Long: `Parse a broker list and report structural problems: a truncated
entry count, missing id or name, duplicate ids, unknown region values,
implausible email addresses, and malformed opt-out/website URLs.

With no argument it checks the list this binary would load (--brokers,
then ~/.eraser/brokers.yaml, then the embedded copy). Pass a path to check
a candidate file before committing it. Exits non-zero if anything is wrong,
so it doubles as a CI check.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := brokerFile
			if len(args) == 1 {
				path = args[0]
			}

			var raw []byte
			var err error
			if path != "" {
				raw, err = os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("failed to read %s: %w", path, err)
				}
			} else {
				raw = data.BrokersYAML
			}

			db, err := broker.Validate(raw)
			if err != nil {
				return err
			}

			where := path
			if where == "" {
				where = "embedded broker list"
			}
			fmt.Printf("✓ %s is valid (%d brokers)\n", where, len(db.Brokers))
			return nil
		},
	}
	return cmd
}

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

			// No path: check both lists this binary ships. A given file:
			// use the lower floor, since it's often a partial candidate
			// batch or the deliberately small verified list.
			if path == "" {
				if err := validateOne("embedded broker list", data.BrokersYAML, broker.MinSaneBrokerCount); err != nil {
					return err
				}
				return validateOne("embedded verified list", data.BrokersVerifiedYAML, broker.MinVerifiedBrokerCount)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", path, err)
			}
			return validateOne(path, raw, broker.MinVerifiedBrokerCount)
		},
	}
	return cmd
}

func validateOne(where string, raw []byte, minCount int) error {
	db, err := broker.Validate(raw, minCount)
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s is valid (%d brokers)\n", where, len(db.Brokers))
	return nil
}

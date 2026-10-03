package main

import (
	"context"
	"fmt"
	"os"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/spf13/cobra"
)

func updateBrokersCmd() *cobra.Command {
	var (
		url   string
		check bool
	)

	cmd := &cobra.Command{
		Use:   "update-brokers",
		Short: "Fetch the latest broker list from the public repository",
		Long: `Download a fresh data/brokers.yaml from the public repo into
~/.eraser/brokers.yaml, which then takes precedence over the copy built into
this binary. The request is conditional (If-None-Match): if nothing has
changed it costs a few hundred bytes and writes nothing.

The app itself is not updated - only the broker list. Never runs automatically.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateBrokers(url, check)
		},
	}

	cmd.Flags().StringVar(&url, "url", broker.DefaultUpdateURL, "source URL for brokers.yaml")
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update is available (exit 1 if so); write nothing")

	return cmd
}

func runUpdateBrokers(url string, check bool) error {
	res, err := broker.Update(context.Background(), url, check)
	if err != nil {
		return err
	}
	if !res.Changed {
		fmt.Printf("✓ Broker list is up to date (%d entries).\n", res.Before)
		return nil
	}
	if check {
		fmt.Println("⬆️  A newer broker list is available. Run `eraser update-brokers` to fetch it.")
		os.Exit(1)
	}
	fmt.Println("⬇️  Broker list updated")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("   %s\n", res.Path)
	fmt.Printf("   %d entries (was %d)\n", res.Count, res.Before)
	return nil
}

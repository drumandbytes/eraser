package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/spf13/cobra"
)

func listBrokersCmd() *cobra.Command {
	var regions, categories []string
	var search string
	var missingEmail bool

	cmd := &cobra.Command{
		Use:   "list-brokers",
		Short: "List all data brokers in the database",
		Long:  "Show all data brokers that will receive removal requests.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListBrokers(regions, categories, search, missingEmail)
		},
	}

	cmd.Flags().StringSliceVar(&regions, "region", nil, "Only show brokers in these regions: us, eu, global (comma-separated or repeated)")
	cmd.Flags().StringSliceVar(&categories, "category", nil, "Only show brokers in these categories (comma-separated or repeated)")
	cmd.Flags().StringVar(&search, "search", "", "Only show brokers whose name or ID contains this text")
	cmd.Flags().BoolVar(&missingEmail, "missing-email", false, "Only show brokers with no email on file (need manual follow-up)")

	return cmd
}

func addBrokerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-broker",
		Short: "Add a new data broker to the database",
		Long:  "Interactively add a new data broker to the local broker database.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddBroker()
		},
	}
}

func runListBrokers(regions, categories []string, search string, missingEmail bool) error {
	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	search = strings.ToLower(strings.TrimSpace(search))

	matched := make([]broker.Broker, 0, len(brokerDB.Brokers))
	for _, b := range brokerDB.Select(nil, regions, categories, nil, nil) {
		if search != "" && !strings.Contains(strings.ToLower(b.Name), search) && !strings.Contains(strings.ToLower(b.ID), search) {
			continue
		}
		if missingEmail && b.Email != "" {
			continue
		}
		matched = append(matched, b)
	}

	if len(regions) > 0 || len(categories) > 0 || search != "" || missingEmail {
		fmt.Printf("📋 Data Brokers (%d of %d total match your filters)\n", len(matched), len(brokerDB.Brokers))
	} else {
		fmt.Printf("📋 Data Brokers (%d total)\n", len(matched))
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for _, b := range matched {
		fmt.Printf("\n%s [%s]\n", b.Name, b.ID)
		if b.Email != "" {
			fmt.Printf("  📧 %s\n", b.Email)
		} else {
			fmt.Printf("  📧 (none - needs manual follow-up)\n")
		}
		if b.Website != "" {
			fmt.Printf("  🌐 %s\n", b.Website)
		}
		if b.OptOutURL != "" {
			fmt.Printf("  🔗 Opt-out: %s\n", b.OptOutURL)
		}
		fmt.Printf("  🌍 Region: %s\n", b.Region)
		if b.Category != "" {
			fmt.Printf("  📁 Category: %s\n", b.Category)
		}
	}

	return nil
}

func runAddBroker() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("➕ Add New Data Broker")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	b := broker.Broker{}

	b.Name = prompt(reader, "Broker name: ")
	b.ID = broker.NewID(b.Name)
	b.Email = prompt(reader, "Privacy/removal email: ")
	b.Website = prompt(reader, "Website (optional): ")
	b.OptOutURL = prompt(reader, "Opt-out URL (optional): ")
	b.Region = prompt(reader, "Region (us/eu/global): ")
	b.Category = prompt(reader, "Category (people-search/marketing/background-check): ")

	if b.ID == "" {
		return fmt.Errorf("broker name is required")
	}
	if problems := b.Problems(); len(problems) > 0 {
		return fmt.Errorf("invalid broker: %s", strings.Join(problems, "; "))
	}

	brokerPath, maintaining := listFileWritePath()
	if !maintaining {
		// installed copy: your own entries file, which update-brokers never touches
		current, err := broker.Load("")
		if err != nil {
			return fmt.Errorf("failed to load brokers: %w", err)
		}
		if current.FindByID(b.ID) != nil || current.FindByName(b.Name) != nil {
			return fmt.Errorf("%q is already in the broker list", b.Name)
		}
		if err := broker.SaveLocal(b); err != nil {
			return fmt.Errorf("failed to save: %w", err)
		}
		fmt.Println()
		fmt.Printf("✅ Added %s to your own brokers (%s)\n", b.Name, broker.LocalPath())
		fmt.Println("   Others would benefit too: https://github.com/drumandbytes/eraser/issues/new?template=broker.yml")
		return nil
	}

	var brokerDB *broker.BrokerDatabase
	if _, err := os.Stat(brokerPath); os.IsNotExist(err) {
		brokerDB = &broker.BrokerDatabase{}
	} else {
		var err error
		brokerDB, err = broker.LoadFromFile(brokerPath)
		if err != nil {
			return fmt.Errorf("failed to load brokers: %w", err)
		}
	}

	if err := brokerDB.Add(b); err != nil {
		return err
	}

	if err := brokerDB.Save(brokerPath); err != nil {
		return fmt.Errorf("failed to save brokers: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Added %s to broker database\n", b.Name)

	return nil
}

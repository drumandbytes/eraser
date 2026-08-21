package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/spf13/cobra"
)

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage multiple profiles (e.g. separate household members)",
		Long: `Most installs only ever need one profile - the one set up by 'eraser init'.
Use these subcommands only if you want to send removal requests for more
than one identity (e.g. a spouse or family member) from this same install,
sharing one config file, broker database, and inbox.`,
	}

	cmd.AddCommand(profileListCmd())
	cmd.AddCommand(profileAddCmd())

	return cmd
}

func profileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileList()
		},
	}
}

func profileAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new named profile",
		Long: `Interactively add a new named profile to the config file, so you can send
removal requests, check status, etc. for a second identity via --profile <id>.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileAdd()
		},
	}
}

func runProfileList() error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	profiles := cfg.GetProfiles()

	fmt.Println("👤 Configured Profiles")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	for _, p := range profiles {
		fmt.Printf("\n%s\n", p.ID)
		fmt.Printf("  %s %s <%s>\n", p.FirstName, p.LastName, p.Email)
	}
	fmt.Println()

	if len(profiles) == 1 {
		fmt.Println("Only one profile configured - --profile can be omitted anywhere.")
	} else {
		fmt.Println("Use --profile <id> on send/status/monitor/pipeline/fill/confirm/mark-bounced to select one.")
	}

	return nil
}

func runProfileAdd() error {
	reader := bufio.NewReader(os.Stdin)
	configPath := resolveConfigPath()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config (run 'eraser init' first): %w", err)
	}

	existingProfiles := cfg.GetProfiles()

	fmt.Println("➕ Add Profile")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	id := strings.ToLower(strings.TrimSpace(prompt(reader, "Profile ID (short, unique, e.g. 'spouse', 'kid1'): ")))
	if id == "" {
		return fmt.Errorf("profile ID is required")
	}
	for _, p := range existingProfiles {
		if strings.EqualFold(p.ID, id) {
			return fmt.Errorf("profile %q already exists", id)
		}
	}

	np := config.NamedProfile{ID: id}
	np.FirstName = prompt(reader, "First name: ")
	np.LastName = prompt(reader, "Last name: ")
	np.Email = prompt(reader, "Email address: ")
	np.Address = prompt(reader, "Street address (optional): ")
	np.City = prompt(reader, "City (optional): ")
	np.State = prompt(reader, "State/Province (optional): ")
	np.ZipCode = prompt(reader, "ZIP/Postal code (optional): ")
	np.Country = prompt(reader, "Country (optional): ")
	np.Phone = prompt(reader, "Phone number (optional): ")

	cfg.Profiles = append(existingProfiles, np)

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Added profile %q\n", id)
	fmt.Printf("Use it with: eraser send --profile %s\n", id)

	return nil
}

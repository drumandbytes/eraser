package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/drumandbytes/eraser/internal/config"
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
	cmd.AddCommand(profileEditCmd())
	cmd.AddCommand(profileRemoveCmd())

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

func profileEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit an existing profile's details",
		Long: `Interactively update a profile's name, email, and address fields. The
profile's ID itself can't be changed here - it's stored verbatim in
history.db, so changing it would orphan that profile's existing send
history.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileEdit(args[0])
		},
	}
}

func profileRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <id>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a profile",
		Long: `Removes a profile from the config file. This does not delete its existing
send history - removal_requests rows stay in history.db tagged with the
now-orphaned profile ID, and become visible again if a profile with the
same ID is re-added. You can't remove the only configured profile.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileRemove(args[0])
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
		fmt.Printf("  %s <%s>\n", p.FullName(), p.Email)
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

	rawID := strings.TrimSpace(prompt(reader, "Profile ID (short, unique, e.g. 'spouse', 'kid1'): "))
	if rawID == "" {
		return fmt.Errorf("profile ID is required")
	}
	// Run the typed ID through the same charset rule the web UI's
	// auto-generated IDs use (config.SlugifyID) - a raw ID with spaces,
	// punctuation, or non-ASCII characters would otherwise round-trip
	// incorrectly through the web UI's cookie-based profile switcher (Go's
	// cookie writer silently drops bytes outside 0x20-0x7e instead of
	// quoting them).
	id := config.SlugifyID(rawID)
	for _, p := range existingProfiles {
		if strings.EqualFold(p.ID, id) {
			return fmt.Errorf("profile %q already exists", id)
		}
	}

	np := config.NamedProfile{ID: id}
	np.FirstName = prompt(reader, "First name: ")
	np.MiddleName = prompt(reader, "Middle name (optional): ")
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

func runProfileEdit(id string) error {
	reader := bufio.NewReader(os.Stdin)
	configPath := resolveConfigPath()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	existing, err := cfg.GetProfile(id)
	if err != nil {
		return err
	}

	fmt.Printf("✏️  Edit Profile %q\n", existing.ID)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Press Enter on any question to keep its current value, shown in [brackets].")
	fmt.Println()

	updated := existing
	updated.FirstName = promptWithDefault(reader, "First name", existing.FirstName)
	updated.MiddleName = promptWithDefault(reader, "Middle name (optional)", existing.MiddleName)
	updated.LastName = promptWithDefault(reader, "Last name", existing.LastName)
	updated.Email = promptWithDefault(reader, "Email address", existing.Email)
	updated.Address = promptWithDefault(reader, "Street address (optional)", existing.Address)
	updated.City = promptWithDefault(reader, "City (optional)", existing.City)
	updated.State = promptWithDefault(reader, "State/Province (optional)", existing.State)
	updated.ZipCode = promptWithDefault(reader, "ZIP/Postal code (optional)", existing.ZipCode)
	updated.Country = promptWithDefault(reader, "Country (optional)", existing.Country)
	updated.Phone = promptWithDefault(reader, "Phone number (optional)", existing.Phone)

	if len(cfg.Profiles) > 0 {
		for i, p := range cfg.Profiles {
			if strings.EqualFold(p.ID, existing.ID) {
				cfg.Profiles[i] = updated
				break
			}
		}
	} else {
		// Legacy single-profile mode (no profiles: list yet) - write back to
		// the top-level profile: block rather than promoting to a profiles:
		// list just because it was edited.
		cfg.Profile = updated.Profile
	}

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Updated profile %q\n", existing.ID)

	return nil
}

func runProfileRemove(id string) error {
	reader := bufio.NewReader(os.Stdin)
	configPath := resolveConfigPath()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	profiles := cfg.GetProfiles()
	if len(profiles) <= 1 {
		return fmt.Errorf("can't remove the only configured profile")
	}

	existing, err := cfg.GetProfile(id)
	if err != nil {
		return err
	}

	answer := prompt(reader, fmt.Sprintf(
		"Remove profile %q (%s <%s>)? This does not delete its existing send history - just the profile itself. Type 'yes' to confirm: ",
		existing.ID, existing.FullName(), existing.Email,
	))
	if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	remaining := make([]config.NamedProfile, 0, len(profiles)-1)
	for _, p := range profiles {
		if !strings.EqualFold(p.ID, existing.ID) {
			remaining = append(remaining, p)
		}
	}
	cfg.Profiles = remaining

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Removed profile %q\n", existing.ID)

	return nil
}

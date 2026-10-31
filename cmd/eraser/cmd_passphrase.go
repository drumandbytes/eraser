package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
)

const minPassphraseLen = 8

// readSecret reads a line from the terminal without echoing it. Replaceable in tests.
var readSecret = func(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("not a terminal")
	}
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func init() {
	config.PassphraseProvider = func() (string, error) { return readSecret("Eraser passphrase: ") }
}

const passphraseHeadsUp = `Heads-up: with a passphrase set, runs with nobody at the keyboard can't unlock your mail password.
  - 'eraser schedule install' (the OS scheduler) is refused, and an installed one will fail to send.
  - 'eraser auto' and 'eraser serve' ask once at startup and then keep running; after a reboot you enter it again.
  - There is no recovery. If you forget it, re-enter your mail passwords with 'eraser init'.`

func passphraseCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "passphrase",
		Short: "Protect your stored mail passwords with a passphrase",
		Long: `By default your mail passwords are encrypted with a key bound to this machine and user.
A passphrase adds a second gate: the passwords can't be decrypted without it, even by someone logged in as you.`,
	}
	set := &cobra.Command{
		Use:          "set",
		Short:        "Set or change the passphrase",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, args []string) error { return runPassphraseSet(yes) },
	}
	set.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation after the heads-up")
	cmd.AddCommand(set, &cobra.Command{
		Use:          "clear",
		Short:        "Remove the passphrase (back to machine-bound encryption)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, args []string) error { return runPassphraseClear() },
	})
	return cmd
}

func runPassphraseSet(yes bool) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	fmt.Println(passphraseHeadsUp)
	fmt.Println()
	if !yes {
		answer := prompt(bufio.NewReader(os.Stdin), "Set a passphrase? [y/N]: ")
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Nothing changed.")
			return nil
		}
	}

	first, err := readSecret("New passphrase: ")
	if err != nil {
		return fmt.Errorf("can't read the passphrase: %w", err)
	}
	if len(first) < minPassphraseLen {
		return fmt.Errorf("use at least %d characters", minPassphraseLen)
	}
	again, err := readSecret("Repeat passphrase: ")
	if err != nil {
		return err
	}
	if first != again {
		return errors.New("the passphrases don't match")
	}

	if err := cfg.SetPassphraseProtection(first); err != nil {
		return err
	}
	if err := config.Save(resolveConfigPath(), cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("✅ Mail passwords are now protected by your passphrase.")
	if schedule.Installed() {
		fmt.Println("⚠️  The OS scheduler is installed and can no longer send. Run 'eraser schedule remove', then use 'eraser auto' or 'eraser serve'.")
	}
	return nil
}

func runPassphraseClear() error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !cfg.PassphraseProtected {
		fmt.Println("No passphrase is set.")
		return nil
	}
	if err := cfg.Unlock(); err != nil {
		return err
	}
	if err := cfg.SetPassphraseProtection(""); err != nil {
		return err
	}
	if err := config.Save(resolveConfigPath(), cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("✅ Passphrase removed. Mail passwords are encrypted for this machine and user only.")
	return nil
}

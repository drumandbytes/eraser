package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
	"github.com/spf13/cobra"
)

func scheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Run Eraser automatically via the OS scheduler (launchd / systemd)",
		Long: `Have the operating system run 'eraser auto --once' ` + schedule.Every + `: send to
brokers that are due, then check the inbox for replies. Runs missed while the
machine was asleep or off happen at the next wake.

macOS uses a launchd agent (output in auto.log next to your config), Linux a
systemd user timer (output in 'journalctl --user -u eraser-auto'). Elsewhere,
run 'eraser auto' in the foreground instead.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:          "install",
		Short:        "Install (or update) the OS job",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, args []string) error { return runScheduleInstall() },
	})
	cmd.AddCommand(&cobra.Command{
		Use:          "remove",
		Short:        "Remove the OS job",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := schedule.Remove(); err != nil {
				return err
			}
			fmt.Println("✅ Removed the scheduled job.")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:          "status",
		Short:        "Show whether the OS job is installed and how the last cycle went",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         func(cmd *cobra.Command, args []string) error { return runScheduleStatus() },
	})
	return cmd
}

func runScheduleInstall() error {
	if !schedule.Supported() {
		return fmt.Errorf("no OS scheduler support on %s - run 'eraser auto' in the foreground instead (it loops every 6h)", runtime.GOOS)
	}

	cfgPath, err := filepath.Abs(resolveConfigPath())
	if err != nil {
		return err
	}
	// Refuse a job that would fail on every run.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("fix your config before scheduling: %w", err)
	}
	if cfg.IsManualSend() && len(cfg.ConfiguredInboxes()) == 0 {
		return fmt.Errorf("nothing to automate: send_mode is manual and no inbox is configured")
	}

	exe, err := stableExecutable()
	if err != nil {
		return err
	}

	job := schedule.Job{Exe: exe, ConfigPath: cfgPath, LogPath: filepath.Join(filepath.Dir(cfgPath), "auto.log")}
	if err := schedule.Install(job); err != nil {
		return err
	}
	fmt.Printf("✅ Scheduled: %s auto --once, %s.\n", exe, schedule.Every)
	if cfg.IsManualSend() {
		fmt.Println("   send_mode is manual, so runs only check the inbox.")
	}
	switch runtime.GOOS {
	case "darwin":
		fmt.Printf("   Output: %s\n", job.LogPath)
	case "linux":
		fmt.Println("   Output: journalctl --user -u eraser-auto")
		fmt.Println("   On a server with no login session, run 'loginctl enable-linger' so the timer runs while you're logged out.")
	}
	fmt.Println("   Check on it with 'eraser schedule status'.")
	return nil
}

// stableExecutable is the path the OS job should run. It prefers the eraser
// on PATH (e.g. Homebrew's symlink, which survives upgrades) when that's the
// same binary as this one, and refuses a 'go run' temp build.
func stableExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to find the eraser binary: %w", err)
	}
	selfReal, err := filepath.EvalSymlinks(self)
	if err != nil {
		selfReal = self
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tmp = os.TempDir()
	}
	if strings.HasPrefix(selfReal, filepath.Clean(tmp)+string(filepath.Separator)) || strings.Contains(selfReal, "go-build") {
		return "", fmt.Errorf("this is a temporary 'go run' build (%s) - build or install eraser first, then run 'eraser schedule install' from that binary", selfReal)
	}
	if onPath, err := exec.LookPath("eraser"); err == nil {
		if abs, err := filepath.Abs(onPath); err == nil {
			if real, err := filepath.EvalSymlinks(abs); err == nil && real == selfReal {
				return abs, nil
			}
		}
	}
	return selfReal, nil
}

func runScheduleStatus() error {
	dir := filepath.Dir(resolveConfigPath())
	switch {
	case schedule.Installed():
		fmt.Printf("OS scheduler: installed (%s)\n", schedule.Every)
	case schedule.Supported():
		fmt.Println("OS scheduler: not installed ('eraser schedule install' to set it up)")
	default:
		fmt.Printf("OS scheduler: not available on %s ('eraser auto' loops in the foreground instead)\n", runtime.GOOS)
	}

	state, err := schedule.LoadState(dir)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Println("Last cycle:   none yet")
		return nil
	}
	via := map[string]string{"os": "OS scheduler", "once": "eraser auto --once", "loop": "eraser auto loop", "serve": "eraser serve"}[state.Mode]
	if via == "" {
		via = state.Mode
	}
	fmt.Printf("Last cycle:   %s (%s ago, via %s), %d sent\n",
		state.LastRun.Format("2006-01-02 15:04"), time.Since(state.LastRun).Round(time.Minute), via, state.Sent)
	if state.Error != "" {
		fmt.Printf("Last error:   %s\n", state.Error)
	}
	return nil
}

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/schedule"
	"github.com/spf13/cobra"
)

func scheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Run Delistd automatically via the OS scheduler (launchd / systemd)",
		Long: `Have the operating system run 'delistd auto --once' ` + schedule.Every + `: send to
brokers that are due, then check the inbox for replies. Runs missed while the
machine was asleep or off happen at the next wake.

macOS uses a launchd agent (output in auto.log next to your config), Linux a
systemd user timer (output in 'journalctl --user -u eraser-auto'). Elsewhere,
run 'delistd auto' in the foreground, or set schedule.enabled: true and keep
'delistd serve' running (Settings -> Automation in the web UI does both).`,
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
		return fmt.Errorf("no OS scheduler support on %s - run 'delistd auto' in the foreground instead (it loops every 6h), or keep 'delistd serve' running with schedule.enabled: true", runtime.GOOS)
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	job, err := schedule.NewJob(cfg, resolveConfigPath())
	if err != nil {
		return err
	}
	if err := schedule.Install(job); err != nil {
		return err
	}
	fmt.Printf("✅ Scheduled: %s auto --once, %s.\n", job.Exe, schedule.Every)
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
	fmt.Println("   Check on it with 'delistd schedule status'.")
	return nil
}

func runScheduleStatus() error {
	dir := filepath.Dir(resolveConfigPath())
	switch {
	case schedule.Installed():
		fmt.Printf("OS scheduler: installed (%s)\n", schedule.Every)
	case schedule.Supported():
		fmt.Println("OS scheduler: not installed ('delistd schedule install' to set it up)")
	default:
		fmt.Printf("OS scheduler: not available on %s ('delistd auto' loops in the foreground instead)\n", runtime.GOOS)
	}

	state, err := schedule.LoadState(dir)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Println("Last cycle:   none yet")
		return nil
	}
	via := map[string]string{"os": "OS scheduler", "once": "delistd auto --once", "loop": "delistd auto", "serve": "delistd serve"}[state.Mode]
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

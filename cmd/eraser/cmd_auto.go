package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/schedule"
	"github.com/spf13/cobra"
)

// autoInboxDays covers the gap between runs with room to spare; replies
// already stored are skipped by AddBrokerResponse's dedup.
const autoInboxDays = 7

// maxAutoLog is when auto.log (launchd's stdout/stderr file) gets rotated
// to auto.log.1 at the start of a run.
const maxAutoLog = 5 << 20

func autoCmd() *cobra.Command {
	var once bool
	var every time.Duration

	cmd := &cobra.Command{
		Use:   "auto",
		Short: "Send due removal requests and check the inbox, once or on a loop",
		Long: `Run one unattended cycle for every profile: send to brokers that are due
(resend cooldown and daily_send_limit apply, as with 'eraser send'), then
scan each configured inbox for replies.

'eraser schedule install' has the OS run 'eraser auto --once' every 6 hours.
Where that isn't available (Windows, containers) or wanted, run 'eraser auto'
in the foreground and it loops every --every until stopped.

Only one cycle runs at a time across all modes; a run that finds another in
progress exits quietly.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if once {
				mode := "once"
				if schedule.UnderOSJob() {
					mode = "os"
				}
				return runAutoCycle(mode)
			}
			return runAutoLoop(every)
		},
	}

	cmd.Flags().BoolVar(&once, "once", false, "Run one cycle and exit")
	cmd.Flags().DurationVar(&every, "every", 6*time.Hour, "Time between cycles in loop mode (minimum 1h)")

	return cmd
}

func runAutoLoop(every time.Duration) error {
	if every < time.Hour {
		return fmt.Errorf("--every must be at least 1h, got %s", every)
	}
	if schedule.Installed() {
		return fmt.Errorf("the OS scheduler already runs these cycles ('eraser schedule status'); run 'eraser schedule remove' first to loop here instead")
	}

	for {
		if err := runAutoCycle("loop"); err != nil {
			// Keep looping: the next cycle may well succeed (flaky IMAP,
			// provider rate limit), and the error is in the state file.
			fmt.Fprintf(os.Stderr, "cycle failed: %v\n", err)
		}
		fmt.Printf("Next cycle at %s\n", time.Now().Add(every).Format("2006-01-02 15:04"))

		// Signals are caught only while waiting. During a cycle Ctrl+C keeps
		// its default and kills the process: that's safe mid-send (history is
		// written per broker, the OS drops the lock) and doesn't make the
		// user wait out a 15-minute send.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		select {
		case <-ctx.Done():
			stop()
			return nil
		case <-time.After(every):
			stop()
		}
	}
}

func runAutoCycle(mode string) error {
	cfgPath := resolveConfigPath()
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	release, ok, err := schedule.TryLock(dir)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Another Eraser cycle is already running - skipping this one.")
		return nil
	}
	defer release()

	rotateLog(filepath.Join(dir, "auto.log"))

	start := time.Now()
	fmt.Printf("━━ Eraser cycle %s ━━\n", start.Format("2006-01-02 15:04:05"))
	cycleErr := autoCycle(cfgPath, start)

	state := schedule.State{LastRun: start, Mode: mode, Sent: sentSince(cfgPath, start)}
	if cycleErr != nil {
		state.Error = cycleErr.Error()
	}
	if err := schedule.SaveState(dir, state); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
	}
	return cycleErr
}

func autoCycle(cfgPath string, start time.Time) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var errs []error
	if cfg.IsManualSend() {
		fmt.Println("⏭️  send_mode is manual - skipping sends (they need you to send each email by hand).")
	} else {
		// runSend reads the --profile global; point it at each profile in
		// turn and put it back afterwards.
		saved := profileFlag
		for _, p := range cfg.GetProfiles() {
			profileFlag = p.ID
			if err := runSend(); err != nil {
				errs = append(errs, fmt.Errorf("send (%s): %w", p.ID, err))
			}
		}
		profileFlag = saved
	}

	if len(cfg.ConfiguredInboxes()) > 0 {
		if err := runMonitor(autoInboxDays, true, false); err != nil {
			errs = append(errs, fmt.Errorf("inbox: %w", err))
		}
	}
	return errors.Join(errs...)
}

// sentSince counts sends across all profiles since start, for the state file.
func sentSince(cfgPath string, start time.Time) int {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return 0
	}
	store, err := history.NewStore(history.DBPathFor(cfgPath))
	if err != nil {
		return 0
	}
	defer func() { _ = store.Close() }()
	total := 0
	for _, p := range cfg.GetProfiles() {
		if n, err := store.CountSentSince(p.ID, start); err == nil {
			total += n
		}
	}
	return total
}

// rotateLog moves an oversized auto.log aside. launchd reopens the file for
// every run, so the next run writes a fresh one.
func rotateLog(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxAutoLog {
		_ = os.Rename(path, path+".1")
	}
}

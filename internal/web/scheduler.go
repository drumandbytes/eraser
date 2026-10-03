package web

import (
	"context"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/drumandbytes/eraser/internal/schedule"
)

// The in-app scheduler is the fallback for when the OS job isn't installed:
// while schedule.enabled is set, serve runs `eraser auto --once` every
// schedule.Interval. Running the CLI cycle as a child process (rather than a
// second implementation here) keeps one send/scan path, one lock and one
// state file for every mode.

// cycleExecutable is the binary a cycle re-runs; tests swap it for a stub.
var cycleExecutable = os.Executable

// runScheduler checks once a minute whether a cycle is due, until ctx ends.
func (s *Server) runScheduler(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if s.cycleDue() {
			s.startCycle()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// inAppScheduling reports whether serve is the one scheduling cycles.
func (s *Server) inAppScheduling() bool {
	cfg := s.getConfig()
	return cfg != nil && cfg.Schedule.Enabled && !s.osInstalled()
}

func (s *Server) cycleDue() bool {
	if !s.inAppScheduling() || s.jobManager.AnyActive() || s.cycleInProgress() {
		return false
	}
	st, err := schedule.LoadState(s.dataDir)
	if err != nil {
		log.Printf("Warning: %v", err)
		return false
	}
	// The state file is shared with the CLI and the OS job, so a recent
	// cycle from any mode counts, and restarting serve doesn't trigger one.
	return st == nil || time.Since(st.LastRun) >= schedule.Interval
}

// startCycle launches one cycle in the background. It returns false if one
// started by this server is still running or a web send job is active.
// cycleMu makes that check and a "Send all" job start mutually exclusive
// (see handleAPISendAll), so the two can't pick the same brokers at once.
func (s *Server) startCycle() bool {
	s.cycleMu.Lock()
	if s.cycleRunning || s.jobManager.AnyActive() {
		s.cycleMu.Unlock()
		return false
	}
	s.cycleRunning = true
	s.cycleMu.Unlock()

	go func() {
		defer func() {
			s.cycleMu.Lock()
			s.cycleRunning = false
			s.cycleMu.Unlock()
		}()
		exe, err := cycleExecutable()
		if err != nil {
			log.Printf("Automatic run: can't find the eraser binary: %v", err)
			return
		}
		cmd := exec.Command(exe, "auto", "--once", "--config", s.configPath)
		cmd.Env = append(os.Environ(), "ERASER_AUTO_MODE=serve")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		log.Printf("Automatic run starting")
		if err := cmd.Run(); err != nil {
			log.Printf("Automatic run finished with an error: %v", err)
			return
		}
		log.Printf("Automatic run finished")
	}()
	return true
}

// cycleInProgress reports whether any cycle (this server's, the CLI's or the
// OS job's) holds the shared lock right now.
func (s *Server) cycleInProgress() bool {
	release, ok, err := schedule.TryLock(s.dataDir)
	if err != nil {
		return false
	}
	if ok {
		release()
		return false
	}
	return true
}

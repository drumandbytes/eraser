package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drumandbytes/delistd/internal/broker"
	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/history"
	"github.com/drumandbytes/delistd/internal/template"
	"github.com/drumandbytes/delistd/internal/web"
	"github.com/spf13/cobra"
)

func serveCmd() *cobra.Command {
	var port int
	var listen string
	var allowedHosts string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local web interface",
		Long: `Start a local web server providing a browser-based interface for Delistd.

This opens a visual dashboard where you can:
- Set up your profile and email settings
- Browse and manage data brokers
- Send removal requests with visual progress
- View history and statistics

The server runs locally on your machine - no data is sent to external servers.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServeWithHosts(port, listen, allowedHosts)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1", "Interface to bind. Leave it on loopback; 0.0.0.0 is for a container whose port is published to the host's 127.0.0.1 only (the web UI has no login)")
	cmd.Flags().StringVar(&allowedHosts, "allowed-hosts", "", "Comma-separated list of allowed Host header values (e.g. '192.168.1.100,unraid.local' or '*' to allow all). Defaults to DELISTD_ALLOWED_HOSTS")

	return cmd
}

func runServe(port int, listen string) error {
	return runServeWithHosts(port, listen, "")
}

func runServeWithHosts(port int, listen string, allowedHostsRaw string) error {
	web.Version = version
	configPath := resolveConfigPath()
	var cfg *config.Config
	if _, err := os.Stat(configPath); err == nil {
		cfg, err = config.Load(configPath)
		if err != nil {
			fmt.Printf("⚠️  Config exists but failed to load: %v\n", err)
			fmt.Println("The setup wizard will help you reconfigure.")
			cfg = nil
		} else if err := cfg.Unlock(); errors.Is(err, config.ErrLocked) {
			fmt.Println("🔒 Mail passwords are protected by a passphrase. Open the web UI to unlock.")
		} else if err != nil {
			return err
		}
	}

	var brokerOpts config.Options
	if cfg != nil {
		brokerOpts = cfg.Options
	}
	brokerDB, err := broker.LoadList(brokerFile, brokerOpts.BrokerFile, brokerOpts.BrokerList)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	store, err := openHistoryStore(history.DBPathFor(configPath))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	tmplEngine, err := template.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to initialize templates: %w", err)
	}
	if err := tmplEngine.UseConfig(cfg); err != nil {
		return fmt.Errorf("custom template: %w", err)
	}

	server, err := web.NewServer(port, cfg, configPath, brokerDB, store, tmplEngine)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}
	server.BrokerOverride = brokerFile
	server.ListenHost = listen
	if allowedHostsRaw == "" {
		allowedHostsRaw = config.Getenv(config.AllowedHostsEnv)
	}
	server.AllowedHosts = web.ParseAllowedHosts(allowedHostsRaw)
	warnNonLoopback(os.Stdout, listen)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	return server.Start()
}

// warnNonLoopback tells the user the UI is reachable beyond this machine when
// listen isn't a loopback address; it prints nothing for loopback.
func warnNonLoopback(w io.Writer, listen string) {
	if web.IsLoopbackListen(listen) {
		return
	}
	_, _ = fmt.Fprintf(w, "⚠️  Listening on %s, not loopback. The web UI has no login and holds your mail credentials:\n", listen)
	_, _ = fmt.Fprintln(w, "   only do this inside a container whose port is published to 127.0.0.1 (docker run -p 127.0.0.1:8080:8080).")
}

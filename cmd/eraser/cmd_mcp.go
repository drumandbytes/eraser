package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func mcpCmd() *cobra.Command {
	var headless, demo bool
	var chromePath string
	cmd := &cobra.Command{
		Use: "mcp", Short: "Expose Eraser tasks to a local AI agent over MCP (stdio)",
		Long: "Start a local MCP server using the same configuration and history as the web UI.\nConfigure begin_batch and retry_action to prompt for approval in your MCP client.\nChrome/Chromium is required for browser actions. Use --demo to try fake local forms.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			var s *mcpserver.Service
			var cleanup func()
			var err error
			if demo {
				s, cleanup, err = mcpserver.NewDemo(headless, chromePath)
			} else {
				s, err = mcpserver.New(mcpserver.Options{ConfigPath: resolveConfigPath(), BrokerPath: brokerFile, ProfileID: profileFlag, Headless: headless, ChromePath: chromePath})
			}
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}
			defer func() { _ = s.Close() }()
			fmt.Fprintln(os.Stderr, "Eraser MCP ready. Waiting for a local client; logs use stderr.")
			err = s.MCP.Run(ctx, &mcp.StdioTransport{})
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return err
		},
	}
	cmd.AddCommand(mcpApproveCmd(), mcpPendingCmd())
	cmd.Flags().BoolVar(&headless, "headless", false, "Hide Chrome (visible by default for verification handoff)")
	cmd.Flags().BoolVar(&demo, "demo", false, "Use isolated fake data and local sample forms; never access real email or brokers")
	cmd.Flags().StringVar(&chromePath, "chrome-path", "", "Optional path to Chrome/Chromium")
	return cmd
}

func mcpApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use: "approve <plan_id>", Short: "Approve a previewed MCP batch (once, for 15 minutes)",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openHistoryStore(history.DBPathFor(resolveConfigPath()))
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			if err := store.ApproveBatch(args[0], "cli"); err != nil {
				return fmt.Errorf("approve %s: %w", args[0], err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Approved batch %s. The agent can begin it once, within 15 minutes.\n", args[0])
			return nil
		},
	}
}

func mcpPendingCmd() *cobra.Command {
	return &cobra.Command{
		Use: "pending", Short: "List MCP batches waiting for approval",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistoryStore(history.DBPathFor(resolveConfigPath()))
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			pending, err := store.PendingBatchApprovals()
			if err != nil {
				return err
			}
			if len(pending) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No batches waiting for approval.")
				return nil
			}
			for _, a := range pending {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  profile=%s  expires=%s\n%s\n\n", a.PlanID, a.ProfileID, a.ExpiresAt.Format(time.RFC3339), a.Preview)
			}
			return nil
		},
	}
}

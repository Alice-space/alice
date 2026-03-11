package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"alice/internal/app"
	"alice/internal/platform"

	"github.com/spf13/cobra"
)

const defaultConfigPath = "configs/alice.yaml"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run executes the CLI with the given arguments and returns the exit code.
// This is used for testing.
func run(args []string) int {
	root := newRootCmd()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:   "alice",
		Short: "Alice is a workflow automation platform",
		Long:  `Alice is a workflow automation platform with event-driven architecture.`,
	}

	root.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "path to config file")

	root.AddCommand(newServeCmd(&configPath))
	root.AddCommand(newClientCmd(&configPath))

	return root
}

func newServeCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Alice server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := platform.LoadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			application, err := app.Bootstrap(cfg)
			if err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := application.Start(ctx); err != nil {
				return fmt.Errorf("start: %w", err)
			}

			// Wait for interrupt signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			// Graceful shutdown
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()

			if err := application.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shutdown: %w", err)
			}

			return nil
		},
	}
}

func newClientCmd(configPath *string) *cobra.Command {
	client := &cobra.Command{
		Use:   "client",
		Short: "Client commands for interacting with Alice server",
	}

	// Client flags would be handled by subcommands
	client.AddCommand(newSubmitCmd())
	client.AddCommand(newGetCmd())
	client.AddCommand(newListCmd())
	client.AddCommand(newResolveCmd())
	client.AddCommand(newCancelCmd())
	client.AddCommand(newAdminCmd())

	return client
}

// Placeholder commands - these would be fully implemented similar to original
func newSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit [message|event|fire]",
		Short: "Submit messages or events",
	}
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [resource]",
		Short: "Get a resource by ID",
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [resources]",
		Short: "List resources",
	}
}

func newResolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve [approval|wait]",
		Short: "Resolve approvals or waits",
	}
}

func newCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel [task]",
		Short: "Cancel a task",
	}
}

func newAdminCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "admin [replay|reconcile|rebuild|redrive]",
		Short: "Admin operations",
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeCmd() *cobra.Command {
	runtimeCmd := &cobra.Command{
		Use:   "runtime",
		Short: "Call the Alice runtime HTTP API from bundled skills",
		Args:  cobra.NoArgs,
	}
	runtimeCmd.AddCommand(
		newRuntimeMessageCmd(),
		newRuntimeMemoryCmd(),
		newRuntimeAutomationCmd(),
		newRuntimeCampaignCmd(),
	)
	return runtimeCmd
}

func withRuntimeClient(
	run func(context.Context, *runtimeapi.Client, mcpbridge.SessionContext, *cobra.Command, []string) error,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, session, err := loadRuntimeClient()
		if err != nil {
			return err
		}
		return run(cmd.Context(), client, session, cmd, args)
	}
}

func loadRuntimeClient() (*runtimeapi.Client, mcpbridge.SessionContext, error) {
	baseURL := strings.TrimSpace(os.Getenv(runtimeapi.EnvBaseURL))
	if baseURL == "" {
		return nil, mcpbridge.SessionContext{}, fmt.Errorf("missing %s", runtimeapi.EnvBaseURL)
	}
	client := runtimeapi.NewClient(baseURL, os.Getenv(runtimeapi.EnvToken))
	if client == nil || !client.IsEnabled() {
		return nil, mcpbridge.SessionContext{}, fmt.Errorf("runtime api client is unavailable")
	}
	session := mcpbridge.SessionContextFromEnv(os.Getenv)
	if err := session.Validate(); err != nil {
		return nil, mcpbridge.SessionContext{}, fmt.Errorf("invalid current alice session: %w", err)
	}
	return client, session, nil
}

func printRuntimeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readRuntimeTextArgOrStdin(args []string) (string, error) {
	body, err := readRuntimeBodyArgOrStdin(args)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("message body is empty")
	}
	return text, nil
}

func readRuntimeBodyArgOrStdin(args []string) ([]byte, error) {
	if len(args) > 0 {
		return []byte(args[0]), nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("missing request body")
	}
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("missing request body")
	}
	return body, nil
}

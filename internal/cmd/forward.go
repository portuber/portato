package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	"github.com/spf13/cobra"
)

var forwardCmd = &cobra.Command{
	Use:    "forward <name>",
	Short:  "Start a single tunnel in the foreground (debug helper)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		path := cfgFile
		if path == "" {
			path = config.DefaultPath()
		}
		cfg, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !hasTunnel(cfg, name) {
			return fmt.Errorf("tunnel %q not found in config", name)
		}

		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		engine := forward.NewEngine(ctx, cfg, log)
		if err := engine.Enable(name); err != nil {
			return fmt.Errorf("enable %q: %w", name, err)
		}

		fmt.Fprintf(os.Stderr, "forwarding %q; press Ctrl-C to stop\n", name)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for _, s := range engine.List() {
						if s.Error != "" {
							fmt.Fprintf(os.Stderr, "  %s: %s (%s)\n", s.Name, s.State, s.Error)
						} else {
							fmt.Fprintf(os.Stderr, "  %s: %s\n", s.Name, s.State)
						}
					}
				}
			}
		}()

		<-ctx.Done()
		fmt.Fprintf(os.Stderr, "stopping...\n")
		_ = engine.Disable(name)
		return nil
	},
}

func hasTunnel(cfg *config.Config, name string) bool {
	for _, t := range cfg.Tunnels {
		if t.Name == name {
			return true
		}
	}
	return false
}

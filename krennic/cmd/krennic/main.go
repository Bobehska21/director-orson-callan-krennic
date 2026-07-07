// Command krennic is the local AI code-review agent: a background daemon plus
// a CLI to inspect and control it.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/acme/krennic/internal/agent"
	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/telemetry"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "krennic",
		Short: "Lokální AI code-review agent — zachytí každou změnu, publikuje shadow ref a nechá ji přečíst AI",
	}
	var cfgPath string
	root.PersistentFlags().StringVar(&cfgPath, "config", config.DefaultPath(), "cesta ke config.toml")

	root.AddCommand(
		runCmd(&cfgPath),
		statusCmd(&cfgPath),
		recentCmd(&cfgPath),
		showCmd(&cfgPath),
		pauseCmd(&cfgPath),
		resumeCmd(&cfgPath),
		keysCmd(),
		doctorCmd(&cfgPath),
		gcCmd(&cfgPath),
		initConfigCmd(&cfgPath),
		hubCmd(&cfgPath),
		teamCmd(&cfgPath),
		auditCmd(&cfgPath),
		doneCmd(&cfgPath),
		syncCmd(&cfgPath),
		&cobra.Command{Use: "version", Short: "Vypíše verzi", Run: func(*cobra.Command, []string) { fmt.Println("krennic", version) }},
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "chyba:", err)
		os.Exit(1)
	}
}

func runCmd(cfgPath *string) *cobra.Command {
	var debug bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Spustí agenta (daemon)",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return fmt.Errorf("%w\n(spusť `krennic init-config` pro vytvoření výchozího configu)", err)
			}
			log := telemetry.NewLogger(debug)
			a, err := agent.New(cfg, log)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			log.Info("krennic starting", "version", version, "dashboard", "http://"+cfg.Agent.DashboardAddr)
			return a.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&debug, "debug", false, "verbose logging")
	return cmd
}

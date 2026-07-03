package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acme/krennic/internal/agent"
	"github.com/acme/krennic/internal/ai"
	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/gitxport"
	"github.com/acme/krennic/internal/secrets"
	"github.com/acme/krennic/internal/store"
	"github.com/spf13/cobra"
)

// knownSecrets are the credential names Krennic uses.
var knownSecrets = []string{"anthropic", "gemini", "git-shadow", "status-token", "hub-token"}

func keysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "keys", Short: "Správa tajemství v OS keychainu"}

	cmd.AddCommand(&cobra.Command{
		Use:   "set <name>",
		Short: "Uloží tajemství (čte ze stdin, nikdy z argv)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			val, err := readSecretStdin(fmt.Sprintf("Hodnota pro %q (nebude vypsána): ", args[0]))
			if err != nil {
				return err
			}
			if val == "" {
				return fmt.Errorf("prázdná hodnota")
			}
			if err := secrets.Store(args[0], val); err != nil {
				return err
			}
			fmt.Printf("Uloženo do keychainu: %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Vypíše, která tajemství jsou nastavena",
		RunE: func(*cobra.Command, []string) error {
			for _, name := range knownSecrets {
				mark := "✗"
				if secrets.Has(name) {
					mark = "✓"
				}
				fmt.Printf("  %s %s\n", mark, name)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "del <name>",
		Short: "Smaže tajemství",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := secrets.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("Smazáno: %s\n", args[0])
			return nil
		},
	})
	return cmd
}

func doctorCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Ověří prostředí (git, keychain, providers, repozitáře)",
		RunE: func(*cobra.Command, []string) error {
			ok := true
			check := func(name string, pass bool, detail string) {
				mark := "✓"
				if !pass {
					mark = "✗"
					ok = false
				}
				fmt.Printf("  %s %-28s %s\n", mark, name, detail)
			}

			// git version
			maj, min := gitxport.New(".").Version()
			check("git", maj >= 2, fmt.Sprintf("%d.%d", maj, min))
			if maj == 2 && min < 51 {
				fmt.Println("      → git < 2.51: použije se univerzální commit-tree transport (OK)")
			}

			// keychain round-trip
			probe := "doctor-probe"
			kcErr := secrets.Store(probe, "ok")
			if kcErr == nil {
				_, kcErr = secrets.Resolve(probe)
				_ = secrets.Delete(probe)
			}
			check("keychain", kcErr == nil, statusOf(kcErr))

			// providers
			cli := ai.NewClaudeCLI()
			check("provider: claude-cli", cli.Available(), boolStr(cli.Available(), "na PATH", "nenalezen"))
			check("provider: anthropic key", secrets.Has("anthropic"), boolStr(secrets.Has("anthropic"), "nastaven", "chybí"))
			check("provider: gemini key", secrets.Has("gemini"), boolStr(secrets.Has("gemini"), "nastaven", "chybí"))

			// config + repos
			cfg, cerr := config.Load(*cfgPath)
			check("config", cerr == nil, statusOf(cerr))
			if cerr == nil {
				repos := agent.ResolveRepos(cfg)
				check("repozitáře", len(repos) > 0, fmt.Sprintf("%d nalezeno", len(repos)))
				remotes := agent.ResolveRemotes(cfg, repos)
				for _, r := range repos {
					url := remotes[r]
					fmt.Printf("      - %s  → ai-remote: %s\n", r, orDash(url))
				}
				check("status publishing", true, boolStr(cfg.Status.Enabled, "zapnuto", "vypnuto (opt-in)"))
				if cfg.Hub.URL != "" {
					check("hub reporting", secrets.Has(cfg.Hub.TokenIdentity),
						cfg.Hub.URL+boolStr(secrets.Has(cfg.Hub.TokenIdentity), " (token ✓)", " (token chybí!)"))
				} else {
					check("hub reporting", true, "vypnuto (lokální režim)")
				}
			}

			fmt.Println()
			if ok {
				fmt.Println("Vše připraveno. Spusť `krennic run`.")
			} else {
				fmt.Println("Některé kontroly selhaly — viz ✗ výše.")
			}
			return nil
		},
	}
}

func gcCmd(cfgPath *string) *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Úklid: promaže staré záznamy a stínové refy zaniklých branchí",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			st, err := store.Open(agent.DBPath(cfg))
			if err != nil {
				return err
			}
			defer st.Close()
			cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
			n, err := st.PruneOlderThan(cutoff)
			if err != nil {
				return err
			}
			fmt.Printf("Promazáno %d starých záznamů (> %d dní).\n", n, days)

			repos := agent.ResolveRepos(cfg)
			deleted := 0
			for _, root := range repos {
				g := gitxport.New(root)
				g.SSHKey = cfg.Git.SSHKeyPath
				refs, err := g.ListShadowRefs(cfg.Git.ShadowNamespace)
				if err != nil {
					continue
				}
				// Build the set of live branches in dash-sanitized form (shadow refs
				// replace "/" with "-"), so branches with slashes compare correctly.
				live := map[string]bool{}
				for _, b := range g.LocalBranches() {
					live[strings.ReplaceAll(b, "/", "-")] = true
				}
				for _, ref := range refs {
					branch := branchFromShadowRef(ref, cfg.Git.ShadowNamespace, agent.UserSlug(), g.RepoName())
					if branch != "" && !live[branch] {
						if err := g.DeleteShadowRef("ai-remote", ref); err == nil {
							deleted++
							fmt.Printf("  smazán stínový ref zaniklé branche: %s\n", ref)
						}
					}
				}
			}
			fmt.Printf("Smazáno %d stínových refů.\n", deleted)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "retenční hranice ve dnech")
	return cmd
}

func initConfigCmd(cfgPath *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init-config",
		Short: "Vytvoří výchozí config.toml",
		RunE: func(*cobra.Command, []string) error {
			path := *cfgPath
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("config už existuje: %s (použij --force pro přepsání)", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(exampleConfig), 0o600); err != nil {
				return err
			}
			fmt.Printf("Vytvořeno: %s\nUprav watch_roots a spusť `krennic keys set anthropic`, pak `krennic doctor`.\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "přepsat existující config")
	return cmd
}

// branchFromShadowRef extracts the (dash-sanitized) branch from a shadow ref
// of the form <namespace>/<user>/<repo>/<branch>.
func branchFromShadowRef(ref, namespace, user, repo string) string {
	prefix := fmt.Sprintf("%s/%s/%s/", namespace, user, repo)
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ""
}

func statusOf(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}
func boolStr(b bool, y, n string) string {
	if b {
		return y
	}
	return n
}
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

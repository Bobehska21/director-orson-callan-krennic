package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/acme/krennic/internal/config"
	"github.com/acme/krennic/internal/hub"
	"github.com/acme/krennic/internal/secrets"
	"github.com/acme/krennic/internal/telemetry"
	"github.com/spf13/cobra"
)

// hubCmd runs the central collection server.
func hubCmd(cfgPath *string) *cobra.Command {
	var addr, dbPath, token string
	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Spustí centrální sběrné místo (týmový audit všech změn)",
		RunE: func(*cobra.Command, []string) error {
			cfg, _ := config.Load(*cfgPath)
			if addr == "" {
				addr = cfg.Hub.ListenAddr
			}
			if addr == "" {
				addr = ":8787"
			}
			if dbPath == "" {
				dbPath = cfg.Hub.DBPath
			}
			if dbPath == "" {
				dir := filepath.Dir(config.DefaultPath())
				_ = os.MkdirAll(dir, 0o700)
				dbPath = filepath.Join(dir, "krennic-hub.db")
			}
			// Token precedence: flag > env > keychain identity.
			if token == "" {
				token = os.Getenv("KRENNIC_HUB_TOKEN")
			}
			if token == "" && cfg.Hub.TokenIdentity != "" {
				token, _ = secrets.Resolve(cfg.Hub.TokenIdentity)
			}

			log := telemetry.NewLogger(false)
			st, err := hub.OpenStore(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()
			if token == "" {
				log.Warn("HUB BĚŽÍ BEZ TOKENU — kdokoli může zapisovat. Nastav KRENNIC_HUB_TOKEN nebo keychain 'hub-token'.")
			}
			srv := hub.NewServer(st, token, log)
			log.Info("krennic hub naslouchá", "addr", addr, "db", dbPath,
				"dashboard", "http://"+addrHuman(addr))
			return srv.ListenAndServe(addr)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "bind adresa (default :8787 nebo z configu)")
	cmd.Flags().StringVar(&dbPath, "db", "", "cesta k audit databázi")
	cmd.Flags().StringVar(&token, "token", "", "sdílený token (jinak env KRENNIC_HUB_TOKEN / keychain)")
	return cmd
}

// teamCmd shows the team-wide feed from the hub.
func teamCmd(cfgPath *string) *cobra.Command {
	var user, repo string
	var limit int
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Týmový přehled: kdo co kde změnil (čte z hubu)",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			if cfg.Hub.URL == "" {
				return fmt.Errorf("v configu není [hub] url — nastav adresu hubu")
			}
			token, _ := secrets.Resolve(cfg.Hub.TokenIdentity)
			url := fmt.Sprintf("%s/api/feed?limit=%d", cfg.Hub.URL, limit)
			if user != "" {
				url += "&user=" + user
			}
			if repo != "" {
				url += "&repo=" + repo
			}
			entries, err := hubGet(url, token)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("(zatím žádné změny)")
				return nil
			}
			fmt.Printf("%-16s  %-14s  %-16s  %-12s  %-9s  %s\n", "ČAS", "KDO", "REPO", "BRANCH", "VERDIKT", "SOUBORY")
			for _, e := range entries {
				r := e.Report
				ts := ""
				if t, err := time.Parse(time.RFC3339Nano, e.ReceivedAt); err == nil {
					ts = t.Local().Format("02.01 15:04:05")
				}
				files := ""
				if len(r.Files) > 0 {
					files = r.Files[0]
					if len(r.Files) > 1 {
						files += fmt.Sprintf(" +%d", len(r.Files)-1)
					}
				}
				verdict := r.Verdict
				if verdict == "" {
					verdict = r.Relevance
				}
				fmt.Printf("%-16s  %-14s  %-16s  %-12s  %-9s  %s\n",
					ts, cut(nz(r.Developer.GitName, r.Developer.UserSlug), 14),
					cut(r.Repo, 16), cut(r.Branch, 12), cut(nz(verdict, "—"), 9), files)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "filtr podle uživatele")
	cmd.Flags().StringVar(&repo, "repo", "", "filtr podle repozitáře")
	cmd.Flags().IntVar(&limit, "limit", 50, "počet záznamů")
	return cmd
}

// auditCmd verifies the hub's tamper-evident chain.
func auditCmd(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "audit", Short: "Ověření integrity týmového auditu"}
	cmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Zkontroluje, že audit nebyl pozměněn ani zkrácen",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			if cfg.Hub.URL == "" {
				return fmt.Errorf("v configu není [hub] url")
			}
			token, _ := secrets.Resolve(cfg.Hub.TokenIdentity)
			var res struct {
				OK        bool   `json:"ok"`
				Count     int    `json:"count"`
				BrokenSeq int64  `json:"broken_seq"`
				Detail    string `json:"detail"`
			}
			if err := getJSONAuth(cfg.Hub.URL+"/api/verify", token, &res); err != nil {
				return err
			}
			if res.OK {
				fmt.Printf("✓ Audit neporušen — %d záznamů, řetězec sedí.\n", res.Count)
			} else {
				fmt.Printf("✗ AUDIT NARUŠEN u záznamu #%d: %s\n", res.BrokenSeq, res.Detail)
			}
			return nil
		},
	})
	return cmd
}

// --- small helpers for hub HTTP ---

type feedEntry struct {
	Seq        int64  `json:"seq"`
	ReceivedAt string `json:"received_at"`
	EntryHash  string `json:"entry_hash"`
	Report     struct {
		Developer struct {
			UserSlug string `json:"user_slug"`
			GitName  string `json:"git_name"`
			GitEmail string `json:"git_email"`
			Machine  string `json:"machine"`
		} `json:"developer"`
		Repo      string   `json:"repo"`
		Branch    string   `json:"branch"`
		Files     []string `json:"files"`
		Relevance string   `json:"relevance"`
		Verdict   string   `json:"verdict"`
	} `json:"report"`
}

func hubGet(url, token string) ([]feedEntry, error) {
	var out []feedEntry
	err := getJSONAuth(url, token, &out)
	return out, err
}

func getJSONAuth(url, token string, v any) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hub nedostupný? (%w)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("hub HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func nz(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func addrHuman(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "127.0.0.1" + addr
	}
	return addr
}

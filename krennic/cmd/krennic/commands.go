package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/acme/krennic/internal/config"
	"github.com/spf13/cobra"
)

// daemonBase returns the daemon's dashboard base URL from config (or default).
func daemonBase(cfgPath string) string {
	addr := "127.0.0.1:7373"
	if cfg, err := config.Load(cfgPath); err == nil && cfg.Agent.DashboardAddr != "" {
		addr = cfg.Agent.DashboardAddr
	}
	return "http://" + addr
}

func getJSON(url string, v any) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("agent neběží? (%w)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func postURL(url string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("agent neběží? (%w)", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func statusCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Stav běžícího agenta",
		RunE: func(*cobra.Command, []string) error {
			var s struct {
				Running     bool     `json:"running"`
				Paused      bool     `json:"paused"`
				PausedUntil string   `json:"paused_until"`
				Repos       []string `json:"repos"`
				QueueDepth  int      `json:"queue_depth"`
				SpendToday  float64  `json:"spend_today"`
				Budget      float64  `json:"budget"`
			}
			if err := getJSON(daemonBase(*cfgPath)+"/api/status", &s); err != nil {
				return err
			}
			state := "RUNNING"
			if s.Paused {
				state = "PAUSED do " + s.PausedUntil
			}
			fmt.Printf("Stav:        %s\n", state)
			fmt.Printf("Fronta:      %d\n", s.QueueDepth)
			fmt.Printf("Repozitáře:  %d\n", len(s.Repos))
			fmt.Printf("Náklady dnes: $%.4f / $%.2f\n", s.SpendToday, s.Budget)
			for _, r := range s.Repos {
				fmt.Printf("  - %s\n", r)
			}
			return nil
		},
	}
}

func recentCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "recent",
		Short: "Poslední analyzované změny",
		RunE: func(*cobra.Command, []string) error {
			var recs []map[string]any
			if err := getJSON(daemonBase(*cfgPath)+"/api/recent?limit=25", &recs); err != nil {
				return err
			}
			if len(recs) == 0 {
				fmt.Println("(zatím žádné změny)")
				return nil
			}
			fmt.Printf("%-8s  %-18s  %-8s  %-9s  %s\n", "ČAS", "REPO", "TRIAGE", "VERDIKT", "CHANGE_ID")
			for _, r := range recs {
				ev, _ := r["event"].(map[string]any)
				repo := nested(ev, "repo", "name")
				triage := "—"
				if t, ok := r["triage"].(map[string]any); ok {
					triage, _ = t["relevance"].(string)
				}
				verdict := "—"
				if v, ok := r["review"].(map[string]any); ok {
					verdict, _ = v["verdict"].(string)
				}
				id, _ := ev["change_id"].(string)
				ts := ""
				if u, ok := r["updated_at"].(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, u); err == nil {
						ts = t.Local().Format("15:04:05")
					}
				}
				fmt.Printf("%-8s  %-18s  %-8s  %-9s  %s\n", ts, cut(repo, 18), triage, verdict, cut(id, 12))
			}
			return nil
		},
	}
}

func showCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <change_id>",
		Short: "Detail jedné analýzy",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var rec any
			if err := getJSON(daemonBase(*cfgPath)+"/api/record?id="+args[0], &rec); err != nil {
				return err
			}
			b, _ := json.MarshalIndent(rec, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
}

func pauseCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pause [duration]",
		Short: "Pozastaví analýzu (např. 30m, 1h)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			secs := 3600
			if len(args) == 1 {
				d, err := time.ParseDuration(args[0])
				if err != nil {
					return fmt.Errorf("neplatná doba %q (použij např. 30m, 1h)", args[0])
				}
				secs = int(d.Seconds())
			}
			if err := postURL(daemonBase(*cfgPath) + "/api/pause?seconds=" + strconv.Itoa(secs)); err != nil {
				return err
			}
			fmt.Printf("Pozastaveno na %d s\n", secs)
			return nil
		},
	}
}

func resumeCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Obnoví analýzu",
		RunE: func(*cobra.Command, []string) error {
			if err := postURL(daemonBase(*cfgPath) + "/api/resume"); err != nil {
				return err
			}
			fmt.Println("Obnoveno")
			return nil
		},
	}
}

func nested(m map[string]any, keys ...string) string {
	cur := m
	for i, k := range keys {
		if cur == nil {
			return ""
		}
		if i == len(keys)-1 {
			s, _ := cur[k].(string)
			return s
		}
		cur, _ = cur[k].(map[string]any)
	}
	return ""
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// readSecretStdin reads one line of secret input without leaving it in argv.
func readSecretStdin(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

package agent

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// newHTTPServer builds the localhost dashboard + control API. The CLI talks to
// these endpoints (status/recent/pause/resume); a browser gets the dashboard.
func (a *Agent) newHTTPServer() *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(a.metrics.Prometheus()))
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		depth, _ := a.store.PendingCount()
		outbox, _ := a.store.OutboxDepth()
		day := time.Now().UTC().Format("2006-01-02")
		spend, _ := a.store.SpendForDay(day)
		writeJSON(w, map[string]any{
			"running":      true,
			"paused":       a.isPaused(),
			"paused_until": a.pausedUntilStr(),
			"repos":        a.repos,
			"team_sync":    a.teamSyncStatus(r),
			"queue_depth":  depth,
			"outbox_depth": outbox,
			"hub_enabled":  a.hubClient != nil,
			"spend_today":  spend,
			"budget":       a.cfg.Budget.DailyUSD,
			"metrics":      a.metrics.Snapshot(),
		})
	})

	mux.HandleFunc("/api/recent", func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		recs, err := a.store.RecentRecords(limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, recs)
	})

	mux.HandleFunc("/api/record", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		rec, err := a.store.GetRecord(id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, rec)
	})

	mux.HandleFunc("/api/pause", func(w http.ResponseWriter, r *http.Request) {
		secs := 3600
		if v := r.URL.Query().Get("seconds"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				secs = n
			}
		}
		a.pause(time.Duration(secs) * time.Second)
		writeJSON(w, map[string]any{"paused": true, "seconds": secs})
	})

	mux.HandleFunc("/api/resume", func(w http.ResponseWriter, r *http.Request) {
		a.resume()
		writeJSON(w, map[string]any{"paused": false})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(dashboardHTML))
	})

	return &http.Server{
		Addr:              a.cfg.Agent.DashboardAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func (a *Agent) teamSyncStatus(r *http.Request) any {
	if a.teamSync == nil || !a.teamSync.Enabled() {
		return map[string]any{"enabled": false}
	}
	return map[string]any{
		"enabled": true,
		"repos":   a.teamSync.StatusAll(r.Context()),
	}
}

func (a *Agent) pausedUntilStr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pausedUntil.IsZero() || time.Now().After(a.pausedUntil) {
		return ""
	}
	return a.pausedUntil.Format(time.RFC3339)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

const dashboardHTML = `<!doctype html>
<html lang="cs"><head><meta charset="utf-8"><title>Krennic</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
 body{font:14px/1.5 system-ui,sans-serif;margin:0;background:#0f1115;color:#e6e6e6}
 header{padding:16px 24px;background:#171a21;border-bottom:1px solid #262a33;display:flex;gap:16px;align-items:center}
 h1{font-size:16px;margin:0}
 .pill{padding:2px 8px;border-radius:10px;font-size:12px}
 .ok{background:#123a1e;color:#7ee2a0}.warn{background:#3a2e12;color:#e2c67e}
 main{padding:24px;max-width:1100px;margin:0 auto}
 .stats{display:flex;gap:16px;flex-wrap:wrap;margin-bottom:24px}
 .card{background:#171a21;border:1px solid #262a33;border-radius:8px;padding:12px 16px;min-width:130px}
 .card .n{font-size:22px;font-weight:600}
 .card .l{font-size:12px;color:#8a919e}
 .team{display:none;margin-bottom:24px;background:#171a21;border:1px solid #262a33;border-radius:8px;padding:12px 16px}
 .team h2{font-size:14px;margin:0 0 8px}
 .team .repo{display:flex;gap:8px;justify-content:space-between;border-top:1px solid #262a33;padding:8px 0}
 .team .repo:first-of-type{border-top:0}
 .team .muted{color:#8a919e}
 .team .pending{color:#e2c67e}.team .error{color:#ff6b6b}
 table{width:100%;border-collapse:collapse}
 th,td{text-align:left;padding:8px 10px;border-bottom:1px solid #262a33;font-size:13px;vertical-align:top}
 th{color:#8a919e;font-weight:500}
 .sev-critical,.sev-high{color:#ff6b6b}.sev-medium{color:#e2c67e}.sev-low{color:#7ee2a0}
 .rel-risky,.rel-notable{color:#e2c67e}.rel-trivial{color:#8a919e}
 button{background:#262a33;color:#e6e6e6;border:1px solid #333;border-radius:6px;padding:4px 10px;cursor:pointer}
 code{background:#0b0d11;padding:1px 4px;border-radius:4px}
</style></head><body>
<header><h1>🛡️ Krennic</h1><span id="state" class="pill">…</span>
 <button onclick="pause()">Pause 1h</button><button onclick="resume()">Resume</button>
 <span style="margin-left:auto;color:#8a919e" id="spend"></span></header>
<main>
 <div class="stats" id="stats"></div>
 <section class="team" id="team-sync"><h2>Team sync</h2><div id="team-sync-rows"></div></section>
 <table><thead><tr><th>Čas</th><th>Repo / branch</th><th>Soubory</th><th>Triage</th><th>Verdikt</th><th>Nálezy</th></tr></thead>
 <tbody id="rows"></tbody></table>
</main>
<script>
async function refresh(){
 const s=await (await fetch('/api/status')).json();
 document.getElementById('state').textContent=s.paused?'PAUSED':'RUNNING';
 document.getElementById('state').className='pill '+(s.paused?'warn':'ok');
 document.getElementById('spend').textContent='Dnes: $'+(s.spend_today||0).toFixed(3)+' / $'+(s.budget||0);
 const m=s.metrics||{};
 const stat=(l,n)=>'<div class="card"><div class="n">'+n+'</div><div class="l">'+l+'</div></div>';
 document.getElementById('stats').innerHTML=
   stat('Fronta',s.queue_depth||0)+
   stat('Zpracováno',Math.round(m.krennic_changes_processed_total||0))+
   stat('Eskalace',Math.round(m.krennic_triage_escalations_total||0))+
   stat('Push chyby',Math.round(m.krennic_shadow_push_failures_total||0))+
   stat('Ø latence AI',Math.round(m.krennic_model_latency_ms_avg||0)+' ms');
 const team=document.getElementById('team-sync');
 const teamRows=document.getElementById('team-sync-rows');
 if(s.team_sync&&s.team_sync.enabled){
   team.style.display='block';
   teamRows.innerHTML=(s.team_sync.repos||[]).map(r=>{
     let cls='',state='aktuální';
     if(r.update_pending){cls='pending';state='nová verze čeká'}
     if(r.error){cls='error';state='chyba: '+r.error}
     const dirty=r.dirty?' rozpracováno':'';
     return '<div class="repo"><div>'+r.path+'<br><span class="muted">'+r.branch+dirty+'</span></div><div class="'+cls+'">'+state+'</div></div>';
   }).join('');
 } else {
   team.style.display='none';
 }
 const recs=await (await fetch('/api/recent?limit=40')).json()||[];
 document.getElementById('rows').innerHTML=recs.map(r=>{
   const e=r.event,t=r.triage,v=r.review;
   const f=(v&&v.findings)?v.findings.length:0;
   return '<tr><td>'+new Date(r.updated_at).toLocaleTimeString()+'</td>'+
    '<td>'+e.repo.name+'<br><code>'+e.repo.branch+'</code></td>'+
    '<td>'+e.summary.files_changed+' (+'+e.summary.lines_added+'/-'+e.summary.lines_removed+')</td>'+
    '<td class="rel-'+(t?t.relevance:'')+'">'+(t?t.relevance:'—')+'</td>'+
    '<td>'+(v?v.verdict:'—')+'</td>'+
    '<td>'+(f?f+' ⚠️':'—')+'</td></tr>';
 }).join('');
}
function pause(){fetch('/api/pause?seconds=3600',{method:'POST'}).then(refresh)}
function resume(){fetch('/api/resume',{method:'POST'}).then(refresh)}
refresh();setInterval(refresh,3000);
</script></body></html>`

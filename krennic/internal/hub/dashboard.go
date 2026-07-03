package hub

const dashboardHTML = `<!doctype html>
<html lang="cs"><head><meta charset="utf-8"><title>Krennic Hub — týmový přehled</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
 body{font:14px/1.5 system-ui,sans-serif;margin:0;background:#0f1115;color:#e6e6e6}
 header{padding:16px 24px;background:#171a21;border-bottom:1px solid #262a33;display:flex;gap:16px;align-items:center;flex-wrap:wrap}
 h1{font-size:16px;margin:0}
 .pill{padding:2px 10px;border-radius:10px;font-size:12px}
 .ok{background:#123a1e;color:#7ee2a0}.bad{background:#3a1212;color:#ff9b9b}
 main{padding:24px;max-width:1300px;margin:0 auto}
 .stats{display:flex;gap:16px;flex-wrap:wrap;margin-bottom:16px}
 .card{background:#171a21;border:1px solid #262a33;border-radius:8px;padding:10px 16px;min-width:120px}
 .card .n{font-size:22px;font-weight:600}.card .l{font-size:12px;color:#8a919e}
 input{background:#0b0d11;border:1px solid #333;color:#e6e6e6;border-radius:6px;padding:5px 8px}
 table{width:100%;border-collapse:collapse}
 th,td{text-align:left;padding:7px 10px;border-bottom:1px solid #262a33;font-size:13px;vertical-align:top}
 th{color:#8a919e;font-weight:500}
 code{background:#0b0d11;padding:1px 5px;border-radius:4px;font-size:12px}
 .who{font-weight:600}.email{color:#8a919e;font-size:12px}
 .v-request-changes{color:#ff6b6b}.v-comment{color:#e2c67e}.v-pass{color:#7ee2a0}
 .files{color:#9ab;font-size:12px}
</style></head><body>
<header><h1>🗂️ Krennic Hub</h1>
 <span id="chain" class="pill">kontrola…</span>
 <input id="fUser" placeholder="filtr: uživatel" oninput="load()">
 <input id="fRepo" placeholder="filtr: repo" oninput="load()">
 <span style="margin-left:auto;color:#8a919e" id="upd"></span></header>
<main>
 <div class="stats" id="stats"></div>
 <table><thead><tr>
   <th>Čas</th><th>Kdo</th><th>Stroj</th><th>Repo / branch</th>
   <th>Soubory (co kde)</th><th>Řádky</th><th>AI verdikt</th><th>#</th>
 </tr></thead><tbody id="rows"></tbody></table>
</main>
<script>
async function load(){
 const st=await (await fetch('/api/stats')).json();
 const stat=(l,n)=>'<div class="card"><div class="n">'+n+'</div><div class="l">'+l+'</div></div>';
 document.getElementById('stats').innerHTML=
   stat('Změn celkem',st.total||0)+stat('Vývojářů',st.developers||0)+stat('Repozitářů',st.repos||0);

 const v=await (await fetch('/api/verify')).json();
 const c=document.getElementById('chain');
 c.textContent=v.ok?('✓ audit neporušen ('+v.count+')'):('✗ NAROUŠENO u #'+v.broken_seq);
 c.className='pill '+(v.ok?'ok':'bad');

 const u=document.getElementById('fUser').value, rp=document.getElementById('fRepo').value;
 let q='/api/feed?limit=200'; if(u)q+='&user='+encodeURIComponent(u); if(rp)q+='&repo='+encodeURIComponent(rp);
 const feed=await (await fetch(q)).json()||[];
 document.getElementById('rows').innerHTML=feed.map(e=>{
   const r=e.report;
   const files=(r.files||[]).slice(0,4).join(', ')+((r.files||[]).length>4?' +'+((r.files.length)-4):'');
   const verdict=r.verdict||(r.relevance||'—');
   return '<tr>'+
    '<td>'+new Date(e.received_at).toLocaleString()+'</td>'+
    '<td><div class="who">'+(r.developer.git_name||r.developer.user_slug)+'</div>'+
        '<div class="email">'+(r.developer.git_email||'')+'</div></td>'+
    '<td><code>'+r.developer.machine+'</code></td>'+
    '<td>'+r.repo+'<br><code>'+r.branch+'</code></td>'+
    '<td class="files">'+files+(r.redacted_paths&&r.redacted_paths.length?'<br>🔒 '+r.redacted_paths.length+' skryto':'')+'</td>'+
    '<td>+'+r.lines_added+' / -'+r.lines_removed+'</td>'+
    '<td class="v-'+(r.verdict||'')+'">'+verdict+'</td>'+
    '<td>'+(r.findings_count||0)+'</td>'+
   '</tr>';
 }).join('');
 document.getElementById('upd').textContent='aktualizováno '+new Date().toLocaleTimeString();
}
load();setInterval(load,4000);
</script></body></html>`

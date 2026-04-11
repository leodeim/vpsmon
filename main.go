package main

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Config ---

var (
	listenAddr = envOr("MONITOR_ADDR", ":8088")
	username   = envOr("MONITOR_USER", "admin")
	password   = envOr("MONITOR_PASS", "changeme")
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- Session store ---

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

var sessions = &sessionStore{sessions: make(map[string]time.Time)}

const sessionTTL = 24 * time.Hour

func (s *sessionStore) create() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return token
}

func (s *sessionStore) valid(token string) bool {
	s.mu.RLock()
	exp, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *sessionStore) destroy(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// --- System metrics ---

type Metrics struct {
	Hostname    string     `json:"hostname"`
	Uptime      string     `json:"uptime"`
	LoadAvg     string     `json:"load_avg"`
	CPUCount    int        `json:"cpu_count"`
	CPUUsage    float64    `json:"cpu_usage"`
	MemTotal    uint64     `json:"mem_total"`
	MemUsed     uint64     `json:"mem_used"`
	MemFree     uint64     `json:"mem_free"`
	MemPercent  float64    `json:"mem_percent"`
	SwapTotal   uint64     `json:"swap_total"`
	SwapUsed    uint64     `json:"swap_used"`
	SwapPercent float64    `json:"swap_percent"`
	Disks       []DiskInfo `json:"disks"`
	NetRx       uint64     `json:"net_rx"`
	NetTx       uint64     `json:"net_tx"`
	Processes   int        `json:"processes"`
	Timestamp   time.Time  `json:"timestamp"`
}

type DiskInfo struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

func collectMetrics() Metrics {
	m := Metrics{
		CPUCount:  runtime.NumCPU(),
		Timestamp: time.Now(),
	}

	// Hostname
	m.Hostname, _ = os.Hostname()

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			if secs, err := strconv.ParseFloat(parts[0], 64); err == nil {
				d := time.Duration(secs) * time.Second
				days := int(d.Hours()) / 24
				hours := int(d.Hours()) % 24
				mins := int(d.Minutes()) % 60
				m.Uptime = fmt.Sprintf("%dd %dh %dm", days, hours, mins)
			}
		}
	}

	// Load average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			m.LoadAvg = strings.Join(parts[:3], " ")
		}
	}

	// CPU usage from /proc/stat
	m.CPUUsage = getCPUUsage()

	// Memory from /proc/meminfo
	meminfo := parseKeyValueFile("/proc/meminfo")
	m.MemTotal = parseKB(meminfo["MemTotal"])
	memFree := parseKB(meminfo["MemFree"])
	buffers := parseKB(meminfo["Buffers"])
	cached := parseKB(meminfo["Cached"])
	m.MemFree = memFree + buffers + cached
	m.MemUsed = m.MemTotal - m.MemFree
	if m.MemTotal > 0 {
		m.MemPercent = float64(m.MemUsed) / float64(m.MemTotal) * 100
	}
	m.SwapTotal = parseKB(meminfo["SwapTotal"])
	swapFree := parseKB(meminfo["SwapFree"])
	m.SwapUsed = m.SwapTotal - swapFree
	if m.SwapTotal > 0 {
		m.SwapPercent = float64(m.SwapUsed) / float64(m.SwapTotal) * 100
	}

	// Disk usage from /proc/mounts + syscall
	m.Disks = getDiskInfo()

	// Network from /proc/net/dev
	m.NetRx, m.NetTx = getNetworkBytes()

	// Process count
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				if _, err := strconv.Atoi(e.Name()); err == nil {
					m.Processes++
				}
			}
		}
	}

	return m
}

func getCPUUsage() float64 {
	read := func() (idle, total uint64) {
		data, err := os.ReadFile("/proc/stat")
		if err != nil {
			return
		}
		lines := strings.Split(string(data), "\n")
		if len(lines) == 0 {
			return
		}
		fields := strings.Fields(lines[0])
		if len(fields) < 5 || fields[0] != "cpu" {
			return
		}
		var vals []uint64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			vals = append(vals, v)
			total += v
		}
		if len(vals) >= 4 {
			idle = vals[3]
		}
		return
	}

	idle1, total1 := read()
	time.Sleep(500 * time.Millisecond)
	idle2, total2 := read()

	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	if totalDelta == 0 {
		return 0
	}
	return (1.0 - idleDelta/totalDelta) * 100
}

func getDiskInfo() []DiskInfo {
	var disks []DiskInfo
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return disks
	}
	defer f.Close()

	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		device, mount := fields[0], fields[1]
		if !strings.HasPrefix(device, "/dev/") {
			continue
		}
		if seen[device] {
			continue
		}
		seen[device] = true

		var stat syscallStatfs
		if err := statfs(mount, &stat); err != nil {
			continue
		}
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bavail * uint64(stat.Bsize)
		used := total - free
		pct := 0.0
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}
		disks = append(disks, DiskInfo{
			Mount:   mount,
			Device:  filepath.Base(device),
			Total:   total,
			Used:    used,
			Free:    free,
			Percent: pct,
		})
	}
	return disks
}

func getNetworkBytes() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return
}

func parseKeyValueFile(path string) map[string]string {
	result := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func parseKB(s string) uint64 {
	s = strings.TrimSuffix(s, " kB")
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseUint(s, 10, 64)
	return v * 1024
}

// --- HTTP handlers ---

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !authenticated(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, dashboardHTML)
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, loginHTML)
			return
		}
		r.ParseForm()
		u := r.FormValue("username")
		p := r.FormValue("password")
		if subtle.ConstantTimeCompare([]byte(u), []byte(username)) == 1 &&
			subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1 {
			token := sessions.create()
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   86400,
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, loginErrorHTML)
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err == nil {
			sessions.destroy(c.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:   "session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		http.Redirect(w, r, "/login", http.StatusFound)
	})

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collectMetrics())
	})

	log.Printf("VPS Monitor starting on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

func authenticated(r *http.Request) bool {
	c, err := r.Cookie("session")
	if err != nil {
		return false
	}
	return sessions.valid(c.Value)
}

// --- HTML templates ---

const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Login - VPS Monitor</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh}
.card{background:#1e293b;border-radius:12px;padding:2rem;width:100%;max-width:380px;box-shadow:0 4px 24px rgba(0,0,0,.4)}
h1{font-size:1.4rem;margin-bottom:1.5rem;text-align:center;color:#38bdf8}
input{width:100%;padding:.75rem 1rem;margin-bottom:1rem;border:1px solid #334155;border-radius:8px;background:#0f172a;color:#e2e8f0;font-size:.95rem}
input:focus{outline:none;border-color:#38bdf8}
button{width:100%;padding:.75rem;border:none;border-radius:8px;background:#38bdf8;color:#0f172a;font-size:1rem;font-weight:600;cursor:pointer}
button:hover{background:#7dd3fc}
</style>
</head>
<body>
<div class="card">
<h1>VPS Monitor</h1>
<form method="post" action="/login">
<input type="text" name="username" placeholder="Username" required autofocus>
<input type="password" name="password" placeholder="Password" required>
<button type="submit">Login</button>
</form>
</div>
</body>
</html>`

const loginErrorHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Login - VPS Monitor</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh}
.card{background:#1e293b;border-radius:12px;padding:2rem;width:100%;max-width:380px;box-shadow:0 4px 24px rgba(0,0,0,.4)}
h1{font-size:1.4rem;margin-bottom:1.5rem;text-align:center;color:#38bdf8}
.error{background:#7f1d1d;color:#fca5a5;padding:.75rem;border-radius:8px;margin-bottom:1rem;text-align:center;font-size:.9rem}
input{width:100%;padding:.75rem 1rem;margin-bottom:1rem;border:1px solid #334155;border-radius:8px;background:#0f172a;color:#e2e8f0;font-size:.95rem}
input:focus{outline:none;border-color:#38bdf8}
button{width:100%;padding:.75rem;border:none;border-radius:8px;background:#38bdf8;color:#0f172a;font-size:1rem;font-weight:600;cursor:pointer}
button:hover{background:#7dd3fc}
</style>
</head>
<body>
<div class="card">
<h1>VPS Monitor</h1>
<div class="error">Invalid username or password</div>
<form method="post" action="/login">
<input type="text" name="username" placeholder="Username" required autofocus>
<input type="password" name="password" placeholder="Password" required>
<button type="submit">Login</button>
</form>
</div>
</body>
</html>`

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>VPS Monitor</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;padding:1.5rem}
.header{display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem}
h1{font-size:1.4rem;color:#38bdf8}
.hostname{color:#94a3b8;font-size:.9rem}
.logout{color:#94a3b8;text-decoration:none;font-size:.85rem;padding:.4rem .8rem;border:1px solid #334155;border-radius:6px}
.logout:hover{color:#e2e8f0;border-color:#64748b}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:1rem}
.card{background:#1e293b;border-radius:10px;padding:1.25rem}
.card h2{font-size:.85rem;color:#94a3b8;text-transform:uppercase;letter-spacing:.05em;margin-bottom:1rem}
.metric{display:flex;justify-content:space-between;align-items:center;margin-bottom:.75rem}
.metric:last-child{margin-bottom:0}
.label{color:#94a3b8;font-size:.9rem}
.value{font-size:.95rem;font-weight:600;font-variant-numeric:tabular-nums}
.bar-wrap{width:100%;height:8px;background:#334155;border-radius:4px;margin-top:.35rem}
.bar{height:100%;border-radius:4px;transition:width .5s ease}
.bar.green{background:#22c55e}
.bar.yellow{background:#eab308}
.bar.red{background:#ef4444}
.wide{grid-column:1/-1}
table{width:100%;border-collapse:collapse;font-size:.85rem}
th{text-align:left;color:#64748b;font-weight:500;padding:.5rem 0;border-bottom:1px solid #334155}
td{padding:.5rem 0;border-bottom:1px solid #1e293b}
.updated{text-align:center;color:#475569;font-size:.75rem;margin-top:1rem}
</style>
</head>
<body>
<div class="header">
<div><h1>VPS Monitor</h1><span class="hostname" id="hostname"></span></div>
<a href="/logout" class="logout">Logout</a>
</div>
<div class="grid">
<div class="card">
<h2>CPU</h2>
<div class="metric"><span class="label">Usage</span><span class="value" id="cpu-usage">--</span></div>
<div class="bar-wrap"><div class="bar" id="cpu-bar"></div></div>
<div class="metric" style="margin-top:.75rem"><span class="label">Cores</span><span class="value" id="cpu-cores">--</span></div>
<div class="metric"><span class="label">Load Average</span><span class="value" id="load-avg">--</span></div>
</div>
<div class="card">
<h2>Memory</h2>
<div class="metric"><span class="label">Used / Total</span><span class="value" id="mem-usage">--</span></div>
<div class="bar-wrap"><div class="bar" id="mem-bar"></div></div>
<div class="metric" style="margin-top:.75rem"><span class="label">Swap</span><span class="value" id="swap-usage">--</span></div>
<div class="bar-wrap"><div class="bar" id="swap-bar"></div></div>
</div>
<div class="card">
<h2>System</h2>
<div class="metric"><span class="label">Uptime</span><span class="value" id="uptime">--</span></div>
<div class="metric"><span class="label">Processes</span><span class="value" id="procs">--</span></div>
<div class="metric"><span class="label">Network RX</span><span class="value" id="net-rx">--</span></div>
<div class="metric"><span class="label">Network TX</span><span class="value" id="net-tx">--</span></div>
</div>
<div class="card wide">
<h2>Disks</h2>
<table>
<thead><tr><th>Device</th><th>Mount</th><th>Used</th><th>Free</th><th>Total</th><th style="width:30%">Usage</th></tr></thead>
<tbody id="disk-table"></tbody>
</table>
</div>
</div>
<div class="updated">Last updated: <span id="updated">--</span></div>
<script>
function fmt(bytes){
  if(bytes===0)return"0 B";
  const k=1024,s=["B","KB","MB","GB","TB"];
  const i=Math.floor(Math.log(bytes)/Math.log(k));
  return(bytes/Math.pow(k,i)).toFixed(1)+" "+s[i];
}
function barClass(pct){return pct>90?"red":pct>70?"yellow":"green"}
function setBar(id,pct){
  const el=document.getElementById(id);
  el.style.width=Math.min(pct,100)+"%";
  el.className="bar "+barClass(pct);
}
async function refresh(){
  try{
    const r=await fetch("/api/metrics");
    if(r.status===401){window.location="/login";return}
    const d=await r.json();
    document.getElementById("hostname").textContent=d.hostname;
    document.getElementById("cpu-usage").textContent=d.cpu_usage.toFixed(1)+"%";
    setBar("cpu-bar",d.cpu_usage);
    document.getElementById("cpu-cores").textContent=d.cpu_count;
    document.getElementById("load-avg").textContent=d.load_avg;
    document.getElementById("mem-usage").textContent=fmt(d.mem_used)+" / "+fmt(d.mem_total);
    setBar("mem-bar",d.mem_percent);
    if(d.swap_total>0){
      document.getElementById("swap-usage").textContent=fmt(d.swap_used)+" / "+fmt(d.swap_total)+" ("+d.swap_percent.toFixed(1)+"%)";
      setBar("swap-bar",d.swap_percent);
    }else{
      document.getElementById("swap-usage").textContent="None";
    }
    document.getElementById("uptime").textContent=d.uptime;
    document.getElementById("procs").textContent=d.processes;
    document.getElementById("net-rx").textContent=fmt(d.net_rx);
    document.getElementById("net-tx").textContent=fmt(d.net_tx);
    const tbody=document.getElementById("disk-table");
    tbody.innerHTML="";
    (d.disks||[]).forEach(dk=>{
      const tr=document.createElement("tr");
      tr.innerHTML="<td>"+dk.device+"</td><td>"+dk.mount+"</td><td>"+fmt(dk.used)+"</td><td>"+fmt(dk.free)+"</td><td>"+fmt(dk.total)+'</td><td><div class="bar-wrap"><div class="bar '+barClass(dk.percent)+'" style="width:'+dk.percent.toFixed(0)+'%"></div></div> '+dk.percent.toFixed(1)+"%</td>";
      tbody.appendChild(tr);
    });
    document.getElementById("updated").textContent=new Date(d.timestamp).toLocaleTimeString();
  }catch(e){console.error(e)}
}
refresh();
setInterval(refresh,5000);
</script>
</body>
</html>`

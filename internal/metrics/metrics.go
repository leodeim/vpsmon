package metrics

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const historySize = 120

var (
	metricsMu      sync.RWMutex
	metricsHistory []Metrics
)

type TopProcess struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
}

type DockerContainer struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
}

type Metrics struct {
	Hostname    string            `json:"hostname"`
	Uptime      string            `json:"uptime"`
	LoadAvg     string            `json:"load_avg"`
	CPUCount    int               `json:"cpu_count"`
	CPUUsage    float64           `json:"cpu_usage"`
	MemTotal    uint64            `json:"mem_total"`
	MemUsed     uint64            `json:"mem_used"`
	MemFree     uint64            `json:"mem_free"`
	MemPercent  float64           `json:"mem_percent"`
	SwapTotal   uint64            `json:"swap_total"`
	SwapUsed    uint64            `json:"swap_used"`
	SwapPercent float64           `json:"swap_percent"`
	Disks       []DiskInfo        `json:"disks"`
	NetRx       uint64            `json:"net_rx"`
	NetTx       uint64            `json:"net_tx"`
	NetRxSpeed  float64           `json:"net_rx_speed"`
	NetTxSpeed  float64           `json:"net_tx_speed"`
	Processes   int               `json:"processes"`
	TopCPU      []TopProcess      `json:"top_cpu"`
	TopMem      []TopProcess      `json:"top_mem"`
	Containers  []DockerContainer `json:"containers"`
	Timestamp   time.Time         `json:"timestamp"`
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

	m.TopCPU = getTopProcesses(false)
	m.TopMem = getTopProcesses(true)
	m.Containers = getDockerContainers()

	return m
}

func getDockerContainers() []DockerContainer {
	var containers []DockerContainer

	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{.Name}}|{{.CPUPerc}}|{{.MemPerc}}")
	out, err := cmd.Output()
	if err != nil {
		return containers
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			continue
		}
		
		name := parts[0]
		
		cpuStr := strings.TrimSuffix(parts[1], "%")
		cpu, _ := strconv.ParseFloat(cpuStr, 64)
		
		memStr := strings.TrimSuffix(parts[2], "%")
		mem, _ := strconv.ParseFloat(memStr, 64)

		containers = append(containers, DockerContainer{
			Name: name,
			CPU:  cpu,
			Mem:  mem,
		})
	}
	return containers
}

func getTopProcesses(sortByMem bool) []TopProcess {
	var procs []TopProcess
	sortArg := "-%cpu"
	if sortByMem {
		sortArg = "-%mem"
	}

	cmd := exec.Command("ps", "-eo", "pcpu,pmem,comm", "--sort="+sortArg, "--no-headers")
	out, err := cmd.Output()
	if err != nil {
		return procs
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[0], 64)
		mem, _ := strconv.ParseFloat(fields[1], 64)
		name := strings.Join(fields[2:], " ")

		name = filepath.Base(name)

		procs = append(procs, TopProcess{
			Name: name,
			CPU:  cpu,
			Mem:  mem,
		})

		if len(procs) >= 5 {
			break
		}
	}
	return procs
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

func StartCollector() {
	collectAndStore()
	go func() {
		for {
			time.Sleep(5 * time.Second)
			collectAndStore()
		}
	}()
}

func collectAndStore() {
	m := collectMetrics()

	metricsMu.Lock()
	defer metricsMu.Unlock()

	if len(metricsHistory) > 0 {
		last := metricsHistory[len(metricsHistory)-1]
		dt := m.Timestamp.Sub(last.Timestamp).Seconds()
		if dt > 0 {
			if m.NetRx >= last.NetRx {
				m.NetRxSpeed = float64(m.NetRx-last.NetRx) / dt
			}
			if m.NetTx >= last.NetTx {
				m.NetTxSpeed = float64(m.NetTx-last.NetTx) / dt
			}
		}
	}

	metricsHistory = append(metricsHistory, m)
	if len(metricsHistory) > historySize {
		metricsHistory = metricsHistory[1:]
	}
}

func GetHistory() []Metrics {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	res := make([]Metrics, len(metricsHistory))
	copy(res, metricsHistory)
	return res
}

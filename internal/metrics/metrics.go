package metrics

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	containerIDRE  = regexp.MustCompile(`^[a-fA-F0-9]{12,64}$`)
)

type TopProcess struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
}

type DockerContainer struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Image        string  `json:"image"`
	Status       string  `json:"status"`
	Health       string  `json:"health"`
	Uptime       string  `json:"uptime"`
	RestartCount int     `json:"restart_count"`
	Ports        string  `json:"ports"`
	CPU          float64 `json:"cpu"`
	Mem          float64 `json:"mem"`
}

type GPUInfo struct {
	Name        string  `json:"name"`
	Utilization float64 `json:"utilization"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	Temperature int     `json:"temperature"`
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
	GPUs        []GPUInfo         `json:"gpus"`
	Listeners   []ListeningSocket `json:"listeners,omitempty"`
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
	m.GPUs = getGPUs()
	m.Listeners = getListeningSockets()

	return m
}

func getGPUs() []GPUInfo {
	return append(getNVIDIAGPUs(), getAMDGPUs()...)
}

func getNVIDIAGPUs() []GPUInfo {
	cmd := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) != 5 {
			continue
		}

		utilization, err := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
		if err != nil {
			continue
		}
		memoryUsed, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			continue
		}
		memoryTotal, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		if err != nil {
			continue
		}
		temperature, err := strconv.Atoi(strings.TrimSpace(fields[4]))
		if err != nil {
			continue
		}

		gpus = append(gpus, GPUInfo{
			Name:        strings.TrimSpace(fields[0]),
			Utilization: utilization,
			MemoryUsed:  memoryUsed * 1024 * 1024,
			MemoryTotal: memoryTotal * 1024 * 1024,
			Temperature: temperature,
		})
	}

	return gpus
}

func getAMDGPUs() []GPUInfo {
	cmd := exec.Command("amd-smi", "monitor", "-tuv", "--csv")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil
	}

	columns := map[string]int{}
	for i, column := range strings.Split(lines[0], ",") {
		columns[strings.TrimSpace(column)] = i
	}

	required := []string{"GPU", "GPU_TEMP", "GFX_UTIL", "VRAM_USED", "VRAM_TOTAL"}
	for _, column := range required {
		if _, ok := columns[column]; !ok {
			return nil
		}
	}

	var gpus []GPUInfo
	for _, line := range lines[1:] {
		fields := strings.Split(line, ",")
		if len(fields) != len(columns) {
			continue
		}

		utilization, ok := parseAMDMetric(fields[columns["GFX_UTIL"]])
		if !ok {
			continue
		}
		memoryUsed, ok := parseAMDMetric(fields[columns["VRAM_USED"]])
		if !ok {
			continue
		}
		memoryTotal, ok := parseAMDMetric(fields[columns["VRAM_TOTAL"]])
		if !ok {
			continue
		}
		temperature, ok := parseAMDMetric(fields[columns["GPU_TEMP"]])
		if !ok {
			continue
		}

		gpus = append(gpus, GPUInfo{
			Name:        "AMD GPU " + strings.TrimSpace(fields[columns["GPU"]]),
			Utilization: utilization,
			MemoryUsed:  uint64(memoryUsed * 1024 * 1024),
			MemoryTotal: uint64(memoryTotal * 1024 * 1024),
			Temperature: int(temperature),
		})
	}

	return gpus
}

func parseAMDMetric(value string) (float64, bool) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return 0, false
	}
	metric, err := strconv.ParseFloat(parts[0], 64)
	return metric, err == nil
}

func getDockerContainers() []DockerContainer {
	cmd := exec.Command("docker", "ps", "-a", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	containersByID := map[string]*DockerContainer{}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 5 {
			continue
		}
		containersByID[parts[0]] = &DockerContainer{
			ID:     parts[0],
			Name:   parts[1],
			Image:  parts[2],
			Status: parts[3],
			Ports:  parts[4],
			Health: "none",
			Uptime: "—",
		}
	}

	if len(containersByID) == 0 {
		return nil
	}

	populateDockerStats(containersByID)
	populateDockerDetails(containersByID)

	containers := make([]DockerContainer, 0, len(containersByID))
	for _, container := range containersByID {
		containers = append(containers, *container)
	}
	return containers
}

func populateDockerStats(containersByID map[string]*DockerContainer) {
	cmd := exec.Command("docker", "stats", "--no-stream", "--no-trunc", "--format", "{{.ID}}\t{{.CPUPerc}}\t{{.MemPerc}}")
	out, err := cmd.Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) != 3 {
			continue
		}
		container, ok := containersByID[parts[0]]
		if !ok {
			continue
		}
		container.CPU, _ = strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
		container.Mem, _ = strconv.ParseFloat(strings.TrimSuffix(parts[2], "%"), 64)
	}
}

func populateDockerDetails(containersByID map[string]*DockerContainer) {
	ids := make([]string, 0, len(containersByID))
	for id := range containersByID {
		ids = append(ids, id)
	}

	args := append([]string{"inspect", "--format", "{{.Id}}\t{{.RestartCount}}\t{{.State.StartedAt}}\t{{with index .State \"Health\"}}{{.Status}}{{else}}none{{end}}"}, ids...)
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) != 4 {
			continue
		}
		container, ok := containersByID[parts[0]]
		if !ok {
			continue
		}
		container.RestartCount, _ = strconv.Atoi(parts[1])
		container.Health = parts[3]
		if startedAt, err := time.Parse(time.RFC3339Nano, parts[2]); err == nil && !startedAt.IsZero() && strings.HasPrefix(container.Status, "Up ") {
			container.Uptime = formatUptime(time.Since(startedAt))
		}
	}
}

func formatUptime(duration time.Duration) string {
	if duration < 0 {
		return "—"
	}
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}

func GetContainerLogs(id string) (string, error) {
	if !containerIDRE.MatchString(id) {
		return "", fmt.Errorf("invalid container ID")
	}

	out, err := exec.Command("docker", "logs", "--tail", "100", "--timestamps", id).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read container logs: %w", err)
	}
	return string(out), nil
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
		lastIndex := len(metricsHistory) - 1
		last := metricsHistory[lastIndex]
		dt := m.Timestamp.Sub(last.Timestamp).Seconds()
		if dt > 0 {
			if m.NetRx >= last.NetRx {
				m.NetRxSpeed = float64(m.NetRx-last.NetRx) / dt
			}
			if m.NetTx >= last.NetTx {
				m.NetTxSpeed = float64(m.NetTx-last.NetTx) / dt
			}
		}

		metricsHistory[lastIndex].Listeners = nil
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

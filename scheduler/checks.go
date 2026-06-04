package scheduler

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/state"
)

type CheckEngine struct {
	cfg    *config.CheckConfig
	exec   *executor.Executor
	db     *state.DB
	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg *config.CheckConfig, exec *executor.Executor, db *state.DB) *CheckEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &CheckEngine{
		cfg:    cfg,
		exec:   exec,
		db:     db,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (e *CheckEngine) Stop() {
	e.cancel()
}

type CheckResult struct {
	Name    string
	Status  string // ok, warning, critical
	Message string
}

func (e *CheckEngine) RunAll() []CheckResult {
	var results []CheckResult

	// --- Threshold checks (existing) ---
	diskResult := e.checkDisk()
	results = append(results, diskResult)
	e.saveResult(diskResult)

	memResult := e.checkMemory()
	results = append(results, memResult)
	e.saveResult(memResult)

	loadResult := e.checkLoad()
	results = append(results, loadResult)
	e.saveResult(loadResult)

	for _, svc := range e.cfg.Services {
		svcResult := e.checkService(svc)
		results = append(results, svcResult)
		e.saveResult(svcResult)
	}

	// --- Anomaly detection (new) ---
	authResult := e.checkAuthFail()
	results = append(results, authResult)
	e.saveResult(authResult)

	growthResult := e.checkDiskGrowth()
	results = append(results, growthResult)
	e.saveResult(growthResult)

	memGrowthResult := e.checkMemGrowth()
	results = append(results, memGrowthResult)
	e.saveResult(memGrowthResult)

	procResult := e.checkProcessAnomaly()
	results = append(results, procResult)
	e.saveResult(procResult)

	// Save current snapshots for next comparison
	e.saveSnapshots()

	return results
}

func (e *CheckEngine) saveResult(r CheckResult) {
	if e.db != nil {
		e.db.SaveResult(r.Name, r.Status, r.Message)
	}
}

// --- Threshold checks (existing) ---

func (e *CheckEngine) checkDisk() CheckResult {
	r, err := e.exec.Run(e.ctx, "df -P / 2>/dev/null | tail -1 | awk '{print $5}' | tr -d '%'")
	if err != nil {
		return CheckResult{Name: "disk", Status: "warning", Message: fmt.Sprintf("error: %v", err)}
	}

	usage := 0
	fmt.Sscanf(r.Stdout, "%d", &usage)

	msg := fmt.Sprintf("Disk usage: %d%%", usage)
	switch {
	case usage >= e.cfg.MemCriticalThreshold:
		return CheckResult{Name: "disk", Status: "critical", Message: msg}
	case usage >= e.cfg.DiskThreshold:
		return CheckResult{Name: "disk", Status: "warning", Message: msg}
	default:
		return CheckResult{Name: "disk", Status: "ok", Message: msg}
	}
}

func (e *CheckEngine) checkMemory() CheckResult {
	r, err := e.exec.Run(e.ctx, "free -m 2>/dev/null | awk '/Mem:/ {printf \"%.0f\", $3/$2 * 100}'")
	if err != nil {
		return CheckResult{Name: "memory", Status: "warning", Message: fmt.Sprintf("error: %v", err)}
	}

	usage := 0
	fmt.Sscanf(r.Stdout, "%d", &usage)

	msg := fmt.Sprintf("Memory usage: %d%%", usage)
	switch {
	case usage >= e.cfg.MemCriticalThreshold:
		return CheckResult{Name: "memory", Status: "critical", Message: msg}
	case usage >= e.cfg.MemWarningThreshold:
		return CheckResult{Name: "memory", Status: "warning", Message: msg}
	default:
		return CheckResult{Name: "memory", Status: "ok", Message: msg}
	}
}

func (e *CheckEngine) checkLoad() CheckResult {
	r, err := e.exec.Run(e.ctx, "cat /proc/loadavg 2>/dev/null | awk '{print $1}'")
	if err != nil {
		return CheckResult{Name: "load", Status: "warning", Message: fmt.Sprintf("error: %v", err)}
	}

	var load1 float64
	fmt.Sscanf(r.Stdout, "%f", &load1)

	ncpu, _ := e.exec.Run(e.ctx, "nproc 2>/dev/null")
	cores := 1
	fmt.Sscanf(ncpu.Stdout, "%d", &cores)

	perCPU := load1 / float64(cores)
	msg := fmt.Sprintf("Load: %.2f (%.2f per CPU)", load1, perCPU)

	if math.IsNaN(perCPU) || perCPU < 0.7 {
		return CheckResult{Name: "load", Status: "ok", Message: msg}
	} else if perCPU < 1.5 {
		return CheckResult{Name: "load", Status: "warning", Message: msg}
	}
	return CheckResult{Name: "load", Status: "critical", Message: msg}
}

func (e *CheckEngine) checkService(name string) CheckResult {
	r, err := e.exec.Run(e.ctx, fmt.Sprintf("systemctl is-active %s 2>/dev/null", name))
	if err != nil {
		return CheckResult{Name: "service:" + name, Status: "critical", Message: fmt.Sprintf("error: %v", err)}
	}
	status := r.Stdout
	msg := fmt.Sprintf("Service %s: %s", name, status)

	switch {
	case status == "":
		return CheckResult{Name: "service:" + name, Status: "critical", Message: "service not found"}
	case status == "active\n" || status == "active":
		return CheckResult{Name: "service:" + name, Status: "ok", Message: msg}
	default:
		return CheckResult{Name: "service:" + name, Status: "critical", Message: msg}
	}
}

// --- Anomaly detection checks ---

// checkAuthFail counts failed SSH login attempts in the lookback window.
func (e *CheckEngine) checkAuthFail() CheckResult {
	window := e.cfg.AnomalyWindow
	if window == "" {
		window = "5m"
	}

	r, err := e.exec.Run(e.ctx,
		fmt.Sprintf("journalctl -u sshd --since \"-%s\" --no-pager 2>/dev/null | grep -c 'Failed password'", window))
	if err != nil {
		// sshd may not be running or journalctl not available — not an error
		return CheckResult{Name: "authfail", Status: "ok", Message: "auth check not available"}
	}

	count := 0
	fmt.Sscanf(strings.TrimSpace(r.Stdout), "%d", &count)

	threshold := e.cfg.AuthFailThreshold
	if threshold <= 0 {
		threshold = 10
	}

	msg := fmt.Sprintf("Failed SSH logins in last %s: %d", window, count)
	switch {
	case count >= threshold*3:
		return CheckResult{Name: "authfail", Status: "critical", Message: msg}
	case count >= threshold:
		return CheckResult{Name: "authfail", Status: "warning", Message: msg}
	default:
		return CheckResult{Name: "authfail", Status: "ok", Message: msg}
	}
}

// checkDiskGrowth alerts if disk usage jumped significantly since last check.
func (e *CheckEngine) checkDiskGrowth() CheckResult {
	r, err := e.exec.Run(e.ctx, "df -P / 2>/dev/null | tail -1 | awk '{print $5}' | tr -d '%'")
	if err != nil {
		return CheckResult{Name: "disk_growth", Status: "ok", Message: "disk check error"}
	}

	current := 0
	fmt.Sscanf(r.Stdout, "%d", &current)

	// Get previous value
	prevStr, err := e.db.GetSnapshot("disk_pct")
	if err != nil {
		// No previous snapshot — first run, nothing to compare
		return CheckResult{Name: "disk_growth", Status: "ok", Message: fmt.Sprintf("Disk: %d%% (baseline)", current)}
	}

	prev, _ := strconv.Atoi(prevStr)
	growth := current - prev

	threshold := e.cfg.DiskGrowthThreshold
	if threshold <= 0 {
		threshold = 5
	}

	msg := fmt.Sprintf("Disk: %d%% (was %d%%, +%d%% in %s)", current, prev, growth, e.cfg.Interval)
	switch {
	case growth >= threshold*2:
		return CheckResult{Name: "disk_growth", Status: "critical", Message: msg}
	case growth >= threshold:
		return CheckResult{Name: "disk_growth", Status: "warning", Message: msg}
	default:
		return CheckResult{Name: "disk_growth", Status: "ok", Message: msg}
	}
}

// checkMemGrowth alerts if memory usage jumped since last check.
func (e *CheckEngine) checkMemGrowth() CheckResult {
	r, err := e.exec.Run(e.ctx, "free -m 2>/dev/null | awk '/Mem:/ {printf \"%.0f\", $3/$2 * 100}'")
	if err != nil {
		return CheckResult{Name: "mem_growth", Status: "ok", Message: "mem check error"}
	}

	current := 0
	fmt.Sscanf(r.Stdout, "%d", &current)

	if e.db != nil {
		e.db.SaveSnapshot("mem_pct", strconv.Itoa(current))
	}

	prevStr, err := e.db.GetSnapshot("mem_pct")
	if err != nil {
		return CheckResult{Name: "mem_growth", Status: "ok", Message: fmt.Sprintf("Memory: %d%% (baseline)", current)}
	}

	prev, _ := strconv.Atoi(prevStr)
	growth := current - prev

	threshold := e.cfg.MemGrowthThreshold
	if threshold <= 0 {
		threshold = 10
	}

	msg := fmt.Sprintf("Memory: %d%% (was %d%%, +%d%% in %s)", current, prev, growth, e.cfg.Interval)
	switch {
	case growth >= threshold*2:
		return CheckResult{Name: "mem_growth", Status: "critical", Message: msg}
	case growth >= threshold:
		return CheckResult{Name: "mem_growth", Status: "warning", Message: msg}
	default:
		return CheckResult{Name: "mem_growth", Status: "ok", Message: msg}
	}
}

// checkProcessAnomaly detects new or memory-doubled processes.
func (e *CheckEngine) checkProcessAnomaly() CheckResult {
	// Snapshot top processes by RSS
	r, err := e.exec.Run(e.ctx, "ps aux --sort=-%mem 2>/dev/null | head -15 | awk 'NR>1 {print $11\"|\"$6}'")
	if err != nil {
		return CheckResult{Name: "process", Status: "ok", Message: "process scan not available"}
	}

	currentProcs := parseProcessList(r.Stdout)

	// Store for next run
	if e.db != nil {
		e.db.SaveProcessSnapshot(currentProcs)
	}

	// Get previous snapshot
	prevProcs, err := e.db.GetProcessSnapshot()
	if err != nil {
		return CheckResult{Name: "process", Status: "ok", Message: fmt.Sprintf("Processes: %d tracked (baseline)", len(currentProcs))}
	}

	if len(prevProcs) == 0 {
		return CheckResult{Name: "process", Status: "ok", Message: fmt.Sprintf("Processes: %d tracked (baseline)", len(currentProcs))}
	}

	mult := e.cfg.ProcGrowthMultiplier
	if mult <= 0 {
		mult = 2.0
	}

	// Build lookup of previous procs by name
	prevByName := make(map[string]int)
	for _, p := range prevProcs {
		prevByName[p.Name] = p.RSS
	}

	var warnings []string
	for _, p := range currentProcs {
		prevRSS, known := prevByName[p.Name]
		if !known && p.RSS > 10240 { // >10MB new process
			warnings = append(warnings, fmt.Sprintf("new process %s (%d MB)", p.Name, p.RSS/1024))
			continue
		}
		if known && prevRSS > 1024 && p.RSS > int(float64(prevRSS)*mult) {
			warnings = append(warnings, fmt.Sprintf("%s RSS %.0f MB (was %.0f MB)", p.Name, float64(p.RSS)/1024, float64(prevRSS)/1024))
		}
	}

	if len(warnings) == 0 {
		return CheckResult{Name: "process", Status: "ok", Message: fmt.Sprintf("Processes: %d running, no anomalies", len(currentProcs))}
	}

	msg := "Process anomalies: " + strings.Join(warnings, "; ")
	if len(warnings) >= 3 {
		return CheckResult{Name: "process", Status: "critical", Message: msg}
	}
	return CheckResult{Name: "process", Status: "warning", Message: msg}
}

// parseProcessList parses "name|rss" lines from ps awk output.
// Example input: "/usr/sbin/mysqld|146468", extracted fields = name, rss_kb
func parseProcessList(output string) []state.ProcessInfo {
	var procs []state.ProcessInfo
	seen := make(map[string]int) // track highest RSS per name
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		// Grab the binary name (last /-separated component)
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}

		rssKB := 0
		fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &rssKB)

		// Keep highest RSS for deduplicated names (multiple workers)
		if existing, ok := seen[name]; ok {
			if rssKB > existing {
				seen[name] = rssKB
			}
		} else {
			seen[name] = rssKB
		}
	}

	for name, rss := range seen {
		procs = append(procs, state.ProcessInfo{
			Name: name,
			RSS:  rss,
		})
	}
	return procs
}

// saveSnapshots stores current state values for next check cycle.
func (e *CheckEngine) saveSnapshots() {
	if e.db == nil {
		return
	}

	// Disk %
	if r, err := e.exec.Run(e.ctx, "df -P / 2>/dev/null | tail -1 | awk '{print $5}' | tr -d '%'"); err == nil {
		e.db.SaveSnapshot("disk_pct", strings.TrimSpace(r.Stdout))
	}

	// Mem %
	if r, err := e.exec.Run(e.ctx, "free -m 2>/dev/null | awk '/Mem:/ {printf \"%.0f\", $3/$2 * 100}'"); err == nil {
		e.db.SaveSnapshot("mem_pct", strings.TrimSpace(r.Stdout))
	}
}

func (e *CheckEngine) SystemHealthSummary() string {
	load, _ := e.exec.Run(e.ctx, "cat /proc/loadavg 2>/dev/null")
	mem, _ := e.exec.Run(e.ctx, "free -h 2>/dev/null | head -2")
	disk, _ := e.exec.Run(e.ctx, "df -h / 2>/dev/null | tail -1")
	uptime, _ := e.exec.Run(e.ctx, "uptime -p 2>/dev/null")
	return fmt.Sprintf("Load: %sMem: %sDisk: %sUptime: %s", load.Stdout, mem.Stdout, disk.Stdout, uptime.Stdout)
}

// AlertFromResults checks if any results need alerting
func AlertFromResults(results []CheckResult) []CheckResult {
	var alerts []CheckResult
	for _, r := range results {
		if r.Status == "warning" || r.Status == "critical" {
			alerts = append(alerts, r)
		}
	}
	return alerts
}

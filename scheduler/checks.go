package scheduler

import (
	"context"
	"fmt"
	"math"

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

	// Disk check
	diskResult := e.checkDisk()
	results = append(results, diskResult)
	if e.db != nil {
		e.db.SaveResult(diskResult.Name, diskResult.Status, diskResult.Message)
	}

	// Memory check
	memResult := e.checkMemory()
	results = append(results, memResult)
	if e.db != nil {
		e.db.SaveResult(memResult.Name, memResult.Status, memResult.Message)
	}

	// Load check
	loadResult := e.checkLoad()
	results = append(results, loadResult)
	if e.db != nil {
		e.db.SaveResult(loadResult.Name, loadResult.Status, loadResult.Message)
	}

	// Service checks
	for _, svc := range e.cfg.Services {
		svcResult := e.checkService(svc)
		results = append(results, svcResult)
		if e.db != nil {
			e.db.SaveResult(svcResult.Name, svcResult.Status, svcResult.Message)
		}
	}

	return results
}

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

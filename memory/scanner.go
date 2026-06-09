package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/syawalqi/oryx/executor"
)

type SystemInfo struct {
	OS        string
	Kernel    string
	Arch      string
	Uptime    string
	Hostname  string
	ScanTime  time.Time
	Sections  map[string]string
}

func Scan(ctx context.Context, exec *executor.Executor) (*SystemInfo, error) {
	info := &SystemInfo{
		ScanTime: time.Now(),
		Sections: make(map[string]string),
	}

	hostname, _ := exec.Run(ctx, "hostname 2>/dev/null")
	info.Hostname = strings.TrimSpace(hostname.Stdout)

	osInfo, _ := exec.Run(ctx, "cat /etc/os-release 2>/dev/null | head -5")
	info.OS = strings.TrimSpace(osInfo.Stdout)

	kernel, _ := exec.Run(ctx, "uname -r 2>/dev/null")
	info.Kernel = strings.TrimSpace(kernel.Stdout)

	arch, _ := exec.Run(ctx, "uname -m 2>/dev/null")
	info.Arch = strings.TrimSpace(arch.Stdout)

	uptime, _ := exec.Run(ctx, "uptime 2>/dev/null")
	info.Uptime = strings.TrimSpace(uptime.Stdout)

	// Disk usage
	disk, _ := exec.Run(ctx, "df -h 2>/dev/null | head -20")
	info.Sections["Disk Usage"] = disk.Stdout

	// Mount points
	lsblk, _ := exec.Run(ctx, "lsblk 2>/dev/null | head -20")
	info.Sections["Block Devices"] = lsblk.Stdout

	// Memory
	mem, _ := exec.Run(ctx, "free -h 2>/dev/null")
	info.Sections["Memory"] = mem.Stdout

	// CPU info
	cpu, _ := exec.Run(ctx, "cat /proc/cpuinfo 2>/dev/null | grep 'model name' | head -1")
	cores, _ := exec.Run(ctx, "nproc 2>/dev/null")
	load, _ := exec.Run(ctx, "cat /proc/loadavg 2>/dev/null")
	info.Sections["CPU"] = fmt.Sprintf("Model: %s\nCores: %s\nLoad: %s",
		strings.TrimSpace(cpu.Stdout),
		strings.TrimSpace(cores.Stdout),
		strings.TrimSpace(load.Stdout))

	// Running services
	svcs, _ := exec.Run(ctx, "systemctl list-units --type=service --state=running --no-pager --no-legend 2>/dev/null | head -30")
	info.Sections["Running Services"] = svcs.Stdout

	// Failed services
	failed, _ := exec.Run(ctx, "systemctl list-units --type=service --state=failed --no-pager --no-legend 2>/dev/null")
	info.Sections["Failed Services"] = failed.Stdout

	// Network
	ipAddr, _ := exec.Run(ctx, "ip addr show 2>/dev/null | grep -E 'inet ' | head -10")
	listen, _ := exec.Run(ctx, "ss -tlnp 2>/dev/null | head -20")
	info.Sections["Network"] = fmt.Sprintf("IP Addresses:\n%s\nListening Ports:\n%s", ipAddr.Stdout, listen.Stdout)

	// Logs
	logs, _ := exec.Run(ctx, "journalctl -n 20 --no-pager --quiet 2>/dev/null")
	info.Sections["Recent Logs"] = logs.Stdout

	// Package count
	pkgs, _ := exec.Run(ctx, "dpkg -l 2>/dev/null | wc -l")
	info.Sections["Installed Packages"] = strings.TrimSpace(pkgs.Stdout)

	// Users
	users, _ := exec.Run(ctx, "cat /etc/passwd 2>/dev/null | grep -E '/home/|/bin/bash' | cut -d: -f1 | head -20")
	info.Sections["Users"] = users.Stdout

	// Docker
	docker, _ := exec.Run(ctx, "docker ps --format 'table {{.Names}}	{{.Status}}' 2>/dev/null | head -20")
	if docker.Stdout != "" {
		info.Sections["Docker Containers"] = docker.Stdout
	}

	// Top processes by RAM
	proc, _ := exec.Run(ctx, "ps aux --sort=-%mem 2>/dev/null | head -20")
	info.Sections["Top Processes"] = proc.Stdout

	return info, nil
}

func (info *SystemInfo) Render() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Server Context\n\n"))
	b.WriteString(fmt.Sprintf("Scanned at: %s\n\n", info.ScanTime.Format(time.RFC3339)))

	b.WriteString("## System\n")
	b.WriteString(fmt.Sprintf("- Hostname: %s\n", info.Hostname))
	b.WriteString(fmt.Sprintf("- OS: %s\n", info.OS))
	b.WriteString(fmt.Sprintf("- Kernel: %s\n", info.Kernel))
	b.WriteString(fmt.Sprintf("- Arch: %s\n", info.Arch))
	b.WriteString(fmt.Sprintf("- Uptime: %s\n", info.Uptime))
	b.WriteString("\n")

	for section, content := range info.Sections {
		if strings.TrimSpace(content) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n", section))
		b.WriteString(content)
		b.WriteString("\n\n")
	}

	return b.String()
}

func Save(path, content string) error {
	dir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

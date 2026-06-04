package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/syawalqi/flare/memory"
)

// SidebarData holds server and session info for the right-side panel.
type SidebarData struct {
	// Server info (populated by /scan or parse)
	Hostname    string
	Uptime      string
	OSName      string
	CPUModel    string
	CPULoad     string
	MemoryTotal string
	MemoryUsed  string
	DiskAvail   string
	DiskUsedPct string
	HasScanData bool
	LastScan    time.Time

	// Session info (populated from model)
	ModelName    string
	MessageCount int
}

// UpdateFromScan populates server fields from a memory.SystemInfo result.
func (s *SidebarData) UpdateFromScan(info *memory.SystemInfo) {
	s.Hostname = info.Hostname
	s.Uptime = parseUptime(info.Uptime)
	s.OSName = parseOSName(info.OS)
	s.CPUModel = parseCPUModel(info.Sections["CPU"])
	s.CPULoad = parseCPULoad(info.Sections["CPU"])
	s.MemoryTotal, s.MemoryUsed = parseMemory(info.Sections["Memory"])
	s.DiskAvail, s.DiskUsedPct = parseDisk(info.Sections["Disk Usage"])
	s.HasScanData = true
	s.LastScan = info.ScanTime
}

// Render builds the unbordered sidebar string for the given width, padded to fullHeight.
func (s *SidebarData) Render(width int, fullHeight int) string {
	innerW := width - 2 // minimal padding on each side

	// ── SERVER SECTION ──
	var serverLines []string

	if !s.HasScanData {
		serverLines = append(serverLines, dimmedStyle.Render("run /scan"))
		serverLines = append(serverLines, dimmedStyle.Render("to populate"))
	} else {
		serverLines = append(serverLines, labelValue("Host", s.Hostname, innerW))
		if s.Uptime != "" {
			serverLines = append(serverLines, labelValue("Up", s.Uptime, innerW))
		}
		if s.OSName != "" {
			serverLines = append(serverLines, labelValue("OS", trimLen(s.OSName, innerW-6), innerW))
		}
		if s.CPULoad != "" {
			serverLines = append(serverLines, labelValue("CPU", s.CPULoad, innerW))
		}
		if s.MemoryUsed != "" && s.MemoryTotal != "" {
			serverLines = append(serverLines, labelValue("RAM", s.MemoryUsed+"/"+s.MemoryTotal, innerW))
		}
		if s.DiskAvail != "" {
			serverLines = append(serverLines, labelValue("Disk", s.DiskAvail, innerW))
			if s.DiskUsedPct != "" {
				serverLines = append(serverLines, barGauge(s.DiskUsedPct, innerW))
			}
		}
	}

	serverBlock := strings.Join(serverLines, "\n")

	// ── SESSION SECTION ──
	sessionLines := []string{}
	if s.ModelName != "" {
		sessionLines = append(sessionLines, labelValue("Model", trimLen(s.ModelName, innerW-8), innerW))
	}
	sessionLines = append(sessionLines, labelValue("Msgs", fmt.Sprintf("%d", s.MessageCount), innerW))
	if s.HasScanData {
		sessionLines = append(sessionLines, labelValue("Scan", s.LastScan.Format("15:04"), innerW))
	}
	sessionBlock := strings.Join(sessionLines, "\n")

	// Combine into sections with headers
	secStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true)
	secSep := dimmedStyle.Render(strings.Repeat("─", innerW))

	content := secStyle.Render("SERVER") + "\n" +
		serverBlock + "\n\n" +
		secSep + "\n" +
		secStyle.Render("SESSION") + "\n" +
		sessionBlock + "\n"

	if !s.HasScanData {
		content += "\n" + dimmedStyle.Render("ctrl+s toggle")
	}

	// Count content lines and pad to viewport height
	contentLines := strings.Count(content, "\n") + 1
	padLines := fullHeight - contentLines
	if padLines > 0 {
		content += strings.Repeat("\n", padLines)
	}

	// Return unbordered content constrained to exact width
	return lipgloss.NewStyle().Width(width).Render(content)
}

// CompactString returns a one-line summary of server status for the header bar.
// Empty string if no scan data is available.
func (s *SidebarData) CompactString() string {
	if !s.HasScanData {
		return ""
	}
	parts := []string{}
	if s.Hostname != "" {
		parts = append(parts, "│ HOST:"+s.Hostname)
	}
	if s.Uptime != "" {
		// Trim uptime to first comma for compactness
		up := s.Uptime
		if idx := strings.Index(up, ","); idx > 0 {
			up = up[:idx]
		}
		parts = append(parts, "UP:"+up)
	}
	if s.CPULoad != "" {
		// Just the 1-minute load
		load := s.CPULoad
		if idx := strings.Index(load, " "); idx > 0 {
			load = load[:idx]
		}
		parts = append(parts, "CPU:"+load)
	}
	if s.MemoryUsed != "" && s.MemoryTotal != "" {
		bar := compactMemBar(s.MemoryUsed, s.MemoryTotal, 6)
		pct := compactMemPct(s.MemoryUsed, s.MemoryTotal)
		parts = append(parts, "RAM:"+bar+" "+pct+"%")
	}
	if s.DiskUsedPct != "" {
		bar := compactBar(s.DiskUsedPct, 6)
		parts = append(parts, "DISK:"+bar+" "+s.DiskUsedPct+"%")
	}
	return strings.Join(parts, " ")
}

// compactBar returns an inline progress bar like "█████░░░" for a percentage string.
func compactBar(pct string, barLen int) string {
	percent := 0
	fmt.Sscanf(pct, "%d", &percent)
	return compactBarInt(percent, barLen)
}

// compactMemBar returns an inline progress bar for memory from used/total strings like "2.4G"/"3.7G".
func compactMemBar(used, total string, barLen int) string {
	usedGB := parseSizeVal(used)
	totalGB := parseSizeVal(total)
	if totalGB <= 0 {
		return used + "/" + total
	}
	pct := int(usedGB / totalGB * 100)
	return compactBarInt(pct, barLen)
}

func compactMemPct(used, total string) string {
	usedGB := parseSizeVal(used)
	totalGB := parseSizeVal(total)
	if totalGB <= 0 {
		return "?"
	}
	return fmt.Sprintf("%d", int(usedGB/totalGB*100))
}

func compactBarInt(percent, barLen int) string {
	filled := percent * barLen / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barLen {
		filled = barLen
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)
}

// parseSizeVal converts a human-readable size like "2.4G" or "512M" to GiB as a float64.
func parseSizeVal(s string) float64 {
	s = strings.ToUpper(s)
	var val float64
	fmt.Sscanf(s, "%f", &val)
	if strings.Contains(s, "T") {
		return val * 1024
	}
	if strings.Contains(s, "M") {
		return val / 1024
	}
	if strings.Contains(s, "K") {
		return val / (1024 * 1024)
	}
	return val // assume GiB or dimensionless
}

// ─── helpers ────────────────────────────────────────────

func labelValue(label, value string, maxW int) string {
	// Truncate value to fit
	avail := maxW - len(label) - 2
	if avail < 3 {
		avail = 3
	}
	if len(value) > avail {
		value = value[:avail-1] + "…"
	}
	return lipgloss.NewStyle().Render(label) + " " + dimmedStyle.Render(value)
}

func barGauge(pct string, width int) string {
	percent := 0
	fmt.Sscanf(pct, "%d", &percent)
	filled := percent * (width - 2) / 100
	if filled < 0 {
		filled = 0
	}
	if filled > width-2 {
		filled = width - 2
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", (width-2)-filled)
	color := lipgloss.Color("#10B981") // green
	if percent > 80 {
		color = lipgloss.Color("#EF4444") // red
	} else if percent > 60 {
		color = lipgloss.Color("#F59E0B") // yellow
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar) +
		dimmedStyle.Render(fmt.Sprintf(" %s%%", pct))
}

func trimLen(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// ─── parsers ────────────────────────────────────────────

func parseUptime(raw string) string {
	// "up 12 days, 3 hours" or "12:34" style
	if raw == "" {
		return ""
	}
	// Just take the interesting part after "up "
	if idx := strings.Index(raw, "up "); idx >= 0 {
		raw = raw[idx+3:]
	}
	// Drop load average
	if idx := strings.Index(raw, ", load"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

func parseOSName(raw string) string {
	// Look for PRETTY_NAME="Ubuntu 24.04 LTS"
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			val = strings.Trim(val, "\"")
			return val
		}
	}
	// Fallback: first line
	lines := strings.SplitN(raw, "\n", 2)
	if len(lines) > 0 && lines[0] != "" {
		return lines[0]
	}
	return ""
}

func parseCPUModel(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func parseCPULoad(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "Load:") || strings.HasPrefix(line, "Load ") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func parseMemory(raw string) (total, used string) {
	// Parse free -h output:
	//               total        used        free      shared  buff/cache   available
	// Mem:           3.8G        1.2G        1.8G
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "Mem:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			return fields[1], fields[2]
		}
	}
	return "", ""
}

func parseDisk(raw string) (avail, usedPct string) {
	// Parse df -h output, look for / or /dev/
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 6 && fields[5] == "/" {
			return fields[3], strings.TrimSuffix(fields[4], "%")
		}
	}
	return "", ""
}

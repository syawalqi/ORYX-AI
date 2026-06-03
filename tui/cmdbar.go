package tui

import (
	"strings"
)

type CmdBarItem struct {
	Label string
	Key   string
}

var CmdBarItems = []CmdBarItem{
	{Label: "/clear", Key: "clear chat"},
	{Label: "/config", Key: "show config"},
	{Label: "/memory", Key: "show server context"},
	{Label: "/model", Key: "change model"},
	{Label: "/scan", Key: "rescan server"},
	{Label: "/skill", Key: "list abilities"},
	{Label: "/help", Key: "show help"},
	{Label: "/quit", Key: "exit"},
}

func RenderCmdBar(width int) string {
	var parts []string
	for _, item := range CmdBarItems {
		parts = append(parts, dimmedStyle.Render(item.Label))
	}
	return cmdBarStyle.Width(width).Render(strings.Join(parts, " │ "))
}

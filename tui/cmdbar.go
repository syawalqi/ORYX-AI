package tui

import (
	"strings"
)

type CmdBarItem struct {
	Label string
	Key   string
	Desc  string
}

var CmdBarItems = []CmdBarItem{
	{Label: "/clear", Key: "clear chat", Desc: "Clear chat history"},
	{Label: "/config", Key: "show config", Desc: "View or edit config (/config edit)"},
	{Label: "/memory", Key: "show memory", Desc: "View or edit server memory (/memory edit)"},
	{Label: "/model", Key: "change model", Desc: "Switch AI model (/model <name>)"},
	{Label: "/scan", Key: "rescan server", Desc: "Rescan system and update context"},
	{Label: "/skill", Key: "list abilities", Desc: "List FLARE's built-in skills"},
	{Label: "/help", Key: "show help", Desc: "Show all commands and usage"},
	{Label: "/quit", Key: "exit", Desc: "Exit FLARE"},
	{Label: "/update", Key: "self-update", Desc: "Check and apply updates"},
}

func RenderCmdBar(width int) string {
	var parts []string
	for _, item := range CmdBarItems {
		parts = append(parts, dimmedStyle.Render(item.Label))
	}
	return cmdBarStyle.Width(width).Render(strings.Join(parts, " │ "))
}

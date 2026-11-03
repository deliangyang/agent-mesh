package main

import (
	"os"
	"strings"
)

// traySettingsPath opens the Platforms settings module from the tray menu.
const traySettingsPath = "/settings/platforms"

// trayStartBalloon returns the Windows tray balloon title and body.
func trayStartBalloon(chinese bool) (title, body string) {
	if chinese {
		return "Agent Mesh", "节点已在托盘运行。左键打开界面。"
	}
	return "Agent Mesh", "Node is running in the tray. Left-click to open the UI."
}

// trayMenuLabels returns Open / Settings / Quit labels for tray menus.
func trayMenuLabels(chinese bool) (open, settings, quit string) {
	if chinese {
		return "打开", "设置", "退出"
	}
	return "Open", "Settings", "Quit"
}

// joinUIPath joins a node UI base URL with a path (defaults to "/").
func joinUIPath(uiURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(uiURL), "/")
	p := strings.TrimSpace(path)
	if p == "" || p == "/" {
		return base + "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

// trayChineseLocale reports zh* from common Unix locale env vars.
func trayChineseLocale() bool {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if strings.HasPrefix(v, "zh") {
			return true
		}
	}
	return false
}

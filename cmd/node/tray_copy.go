package main

import (
	"os"
	"strings"
)

// trayStartBalloon returns the Windows tray balloon title and body.
func trayStartBalloon(chinese bool) (title, body string) {
	if chinese {
		return "Agent Mesh", "节点已在托盘运行。左键打开界面。"
	}
	return "Agent Mesh", "Node is running in the tray. Left-click to open the UI."
}

// trayMenuLabels returns Open / Quit labels for tray menus.
func trayMenuLabels(chinese bool) (open, quit string) {
	if chinese {
		return "打开", "退出"
	}
	return "Open", "Quit"
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

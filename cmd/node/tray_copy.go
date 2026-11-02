package main

// trayStartBalloon returns the Windows tray balloon title and body.
func trayStartBalloon(chinese bool) (title, body string) {
	if chinese {
		return "Agent Mesh", "节点已在托盘运行。左键打开界面。"
	}
	return "Agent Mesh", "Node is running in the tray. Left-click to open the UI."
}

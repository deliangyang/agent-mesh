package main

import (
	"os"
	"strings"

	"agent-mesh/internal/i18n"
)

// traySettingsPath opens the Platforms settings module from the tray menu.
const traySettingsPath = "/settings/platforms"

// trayStartBalloon returns the Windows tray balloon title and body.
func trayStartBalloon(lang string) (title, body string) {
	return i18n.T(lang, "tray.balloonTitle"), i18n.T(lang, "tray.balloonBody")
}

// trayMenuLabels returns Open / Settings / Quit labels for tray menus.
func trayMenuLabels(lang string) (open, settings, quit string) {
	return i18n.T(lang, "tray.open"), i18n.T(lang, "tray.settings"), i18n.T(lang, "tray.quit")
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

// trayLocaleFromEnv reports a mesh locale from common Unix locale env vars.
func trayLocaleFromEnv() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			continue
		}
		// Strip encoding suffix: zh_CN.UTF-8 → zh_CN
		if i := strings.IndexByte(v, '.'); i >= 0 {
			v = v[:i]
		}
		return i18n.Normalize(v)
	}
	return i18n.Default
}

// trayChineseLocale reports zh* from common Unix locale env vars.
// Kept for older call sites; prefer trayLocaleFromEnv.
func trayChineseLocale() bool {
	return trayLocaleFromEnv() == i18n.LangZhCN
}

package main

import "testing"

func TestTrayStartBalloon(t *testing.T) {
	title, body := trayStartBalloon(false)
	if title != "Agent Mesh" || body == "" {
		t.Fatalf("en %#v %#v", title, body)
	}
	title, body = trayStartBalloon(true)
	if title != "Agent Mesh" || body == "" {
		t.Fatalf("zh %#v %#v", title, body)
	}
	_, zhBody := trayStartBalloon(true)
	_, enBody := trayStartBalloon(false)
	if zhBody == enBody {
		t.Fatal("chinese and english balloons should differ")
	}
}

func TestTrayMenuLabels(t *testing.T) {
	open, settings, quit := trayMenuLabels(false)
	if open != "Open" || settings != "Settings" || quit != "Quit" {
		t.Fatalf("en %q %q %q", open, settings, quit)
	}
	open, settings, quit = trayMenuLabels(true)
	if open != "打开" || settings != "设置" || quit != "退出" {
		t.Fatalf("zh %q %q %q", open, settings, quit)
	}
}

func TestJoinUIPath(t *testing.T) {
	if got := joinUIPath("http://127.0.0.1:7700", ""); got != "http://127.0.0.1:7700/" {
		t.Fatalf("root empty: %q", got)
	}
	if got := joinUIPath("http://127.0.0.1:7700/", "/"); got != "http://127.0.0.1:7700/" {
		t.Fatalf("root slash: %q", got)
	}
	if got := joinUIPath("http://127.0.0.1:7700", traySettingsPath); got != "http://127.0.0.1:7700/settings/platforms" {
		t.Fatalf("settings: %q", got)
	}
	if got := joinUIPath("http://127.0.0.1:7700/", "settings/platforms"); got != "http://127.0.0.1:7700/settings/platforms" {
		t.Fatalf("no leading slash: %q", got)
	}
}

func TestTrayChineseLocale(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if trayChineseLocale() {
		t.Fatal("en")
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if !trayChineseLocale() {
		t.Fatal("zh")
	}
}

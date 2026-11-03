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
	open, quit := trayMenuLabels(false)
	if open != "Open" || quit != "Quit" {
		t.Fatalf("en %q %q", open, quit)
	}
	open, quit = trayMenuLabels(true)
	if open != "打开" || quit != "退出" {
		t.Fatalf("zh %q %q", open, quit)
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

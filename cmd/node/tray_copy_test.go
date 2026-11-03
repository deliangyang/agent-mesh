package main

import (
	"testing"

	"agent-mesh/internal/i18n"
)

func TestTrayStartBalloon(t *testing.T) {
	title, body := trayStartBalloon(i18n.LangEn)
	if title != "Agent Mesh" || body == "" {
		t.Fatalf("en %#v %#v", title, body)
	}
	title, body = trayStartBalloon(i18n.LangZhCN)
	if title != "Agent Mesh" || body == "" {
		t.Fatalf("zh %#v %#v", title, body)
	}
	_, jaBody := trayStartBalloon(i18n.LangJa)
	_, enBody := trayStartBalloon(i18n.LangEn)
	_, zhBody := trayStartBalloon(i18n.LangZhCN)
	if zhBody == enBody || jaBody == enBody || jaBody == zhBody {
		t.Fatal("locale balloons should differ")
	}
}

func TestTrayMenuLabels(t *testing.T) {
	open, settings, quit := trayMenuLabels(i18n.LangEn)
	if open != "Open" || settings != "Settings" || quit != "Quit" {
		t.Fatalf("en %q %q %q", open, settings, quit)
	}
	open, settings, quit = trayMenuLabels(i18n.LangZhCN)
	if open != "打开" || settings != "设置" || quit != "退出" {
		t.Fatalf("zh %q %q %q", open, settings, quit)
	}
	open, settings, quit = trayMenuLabels(i18n.LangJa)
	if open != "開く" || settings != "設定" || quit != "終了" {
		t.Fatalf("ja %q %q %q", open, settings, quit)
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

func TestTrayLocaleFromEnv(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := trayLocaleFromEnv(); got != i18n.LangEn {
		t.Fatalf("en: %q", got)
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := trayLocaleFromEnv(); got != i18n.LangZhCN {
		t.Fatalf("zh: %q", got)
	}
	t.Setenv("LANG", "ja_JP.UTF-8")
	if got := trayLocaleFromEnv(); got != i18n.LangJa {
		t.Fatalf("ja: %q", got)
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if !trayChineseLocale() {
		t.Fatal("zh chinese")
	}
	t.Setenv("LANG", "en_US.UTF-8")
	if trayChineseLocale() {
		t.Fatal("en chinese")
	}
}

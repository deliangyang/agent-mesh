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

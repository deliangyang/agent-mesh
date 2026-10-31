package claude

import "testing"

func TestSkipPrintDiagnostic(t *testing.T) {
	cases := []struct {
		line string
		skip bool
	}{
		{`[claude-code:unrecognized_model] {"model":"group/never-stuck[1m]","query_source":"sdk"}`, true},
		{`  [claude-code:unrecognized_model] {"model":"opus"}`, true},
		{`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`, false},
		{`some real stderr`, false},
	}
	for _, c := range cases {
		if got := skipPrintDiagnostic(c.line); got != c.skip {
			t.Fatalf("skipPrintDiagnostic(%q)=%v want %v", c.line, got, c.skip)
		}
	}
}

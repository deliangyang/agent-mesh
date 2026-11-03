package i18n

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadError(t *testing.T) {
	if err := LoadError(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", Default},
		{"en", LangEn},
		{"en-US", LangEn},
		{"zh", LangZhCN},
		{"zh-CN", LangZhCN},
		{"zh_TW", LangZhCN},
		{"ja", LangJa},
		{"ja-JP", LangJa},
		{"fr", Default},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := Normalize(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestParseAcceptLanguage(t *testing.T) {
	cases := []struct {
		name, header, want string
	}{
		{"empty", "", Default},
		{"ja", "ja", LangJa},
		{"jaJP", "ja-JP,en;q=0.8", LangJa},
		{"zhPrefer", "zh-CN,zh;q=0.9,en;q=0.8", LangZhCN},
		{"enPrefer", "fr;q=0.9,en;q=0.8", LangEn},
		{"qWins", "en;q=0.5,ja;q=0.9", LangJa},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseAcceptLanguage(c.header); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestT(t *testing.T) {
	if got := T(LangEn, "tray.open"); got != "Open" {
		t.Fatalf("en open: %q", got)
	}
	if got := T(LangZhCN, "tray.open"); got != "打开" {
		t.Fatalf("zh open: %q", got)
	}
	if got := T(LangJa, "tray.open"); got != "開く" {
		t.Fatalf("ja open: %q", got)
	}
	if got := T(LangJa, "http.modelMustLookLikeEngine"); got == "" || got == "http.modelMustLookLikeEngine" {
		t.Fatalf("ja model: %q", got)
	}
	if got := T(LangEn, "missing.key"); got != "missing.key" {
		t.Fatalf("missing: %q", got)
	}
}

func TestError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "ja")
	rec := httptest.NewRecorder()
	Error(rec, req, http.StatusUnauthorized, "http.wrongPassword")
	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := string(body); got != "パスワードが違います\n" {
		t.Fatalf("body %q", got)
	}
}

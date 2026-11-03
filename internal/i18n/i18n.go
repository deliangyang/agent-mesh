// Package i18n loads embedded locale JSON and resolves strings from Accept-Language.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// Supported language tags (BCP 47 style). Default is English.
const (
	LangEn   = "en"
	LangZhCN = "zh-CN"
	LangJa   = "ja"
)

// Default is the fallback language when nothing matches.
const Default = LangEn

//go:embed locales/*.json
var localeFS embed.FS

var (
	once     sync.Once
	catalogs map[string]map[string]string
	loadErr  error
)

func ensure() {
	once.Do(func() {
		catalogs = map[string]map[string]string{}
		entries, err := fs.ReadDir(localeFS, "locales")
		if err != nil {
			loadErr = fmt.Errorf("i18n: read locales: %w", err)
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".json") {
				continue
			}
			tag := strings.TrimSuffix(name, ".json")
			raw, err := fs.ReadFile(localeFS, "locales/"+name)
			if err != nil {
				loadErr = fmt.Errorf("i18n: read %s: %w", name, err)
				return
			}
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil {
				loadErr = fmt.Errorf("i18n: parse %s: %w", name, err)
				return
			}
			catalogs[tag] = m
		}
		if catalogs[Default] == nil {
			loadErr = fmt.Errorf("i18n: missing default catalog %s", Default)
		}
	})
}

// LoadError returns the init error, if any (mainly for tests).
func LoadError() error {
	ensure()
	return loadErr
}

// Normalize maps a language tag or prefix onto a supported catalog key.
func Normalize(tag string) string {
	t := strings.TrimSpace(strings.ToLower(tag))
	t = strings.ReplaceAll(t, "_", "-")
	if t == "" {
		return Default
	}
	switch {
	case t == "zh-cn" || t == "zh-hans" || strings.HasPrefix(t, "zh"):
		return LangZhCN
	case t == "ja" || strings.HasPrefix(t, "ja-"):
		return LangJa
	case t == "en" || strings.HasPrefix(t, "en-"):
		return LangEn
	default:
		return Default
	}
}

// ParseAcceptLanguage picks the best supported language from an Accept-Language value.
// Quality values are honored; unsupported tags are skipped. Empty → Default.
func ParseAcceptLanguage(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return Default
	}
	type cand struct {
		tag string
		q   float64
	}
	var list []cand
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			rest := part[i+1:]
			for _, p := range strings.Split(rest, ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(strings.ToLower(p), "q=") {
					var v float64
					if _, err := fmt.Sscanf(p[2:], "%f", &v); err == nil {
						q = v
					}
				}
			}
		}
		if tag == "" || tag == "*" {
			continue
		}
		list = append(list, cand{tag: Normalize(tag), q: q})
	}
	bestTag := Default
	bestQ := -1.0
	seen := map[string]bool{}
	for _, c := range list {
		if seen[c.tag] {
			continue
		}
		seen[c.tag] = true
		if c.q > bestQ {
			bestQ = c.q
			bestTag = c.tag
		}
	}
	if bestQ < 0 {
		return Default
	}
	return bestTag
}

// FromRequest resolves the language from the Accept-Language header.
func FromRequest(r *http.Request) string {
	if r == nil {
		return Default
	}
	return ParseAcceptLanguage(r.Header.Get("Accept-Language"))
}

// T looks up key in lang, falling back to Default then the key itself.
// Params replace {name} placeholders.
func T(lang, key string, params ...map[string]string) string {
	ensure()
	lang = Normalize(lang)
	s := lookup(lang, key)
	if len(params) > 0 && params[0] != nil {
		for k, v := range params[0] {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
	}
	return s
}

func lookup(lang, key string) string {
	if catalogs != nil {
		if cat := catalogs[lang]; cat != nil {
			if v, ok := cat[key]; ok {
				return v
			}
		}
		if lang != Default {
			if cat := catalogs[Default]; cat != nil {
				if v, ok := cat[key]; ok {
					return v
				}
			}
		}
	}
	return key
}

// Error writes a localized plain-text HTTP error using Accept-Language.
func Error(w http.ResponseWriter, r *http.Request, code int, key string, params ...map[string]string) {
	http.Error(w, T(FromRequest(r), key, params...), code)
}

package cache

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	cfgpkg "mitm-proxy/internal/config"
)

func TestSaveSkipsNotModifiedResponses(t *testing.T) {
	dir := t.TempDir()
	cfg := &cfgpkg.Config{Cache: cfgpkg.CacheConfig{Enabled: true, Directory: dir, TTL: 3600}}
	c := New(cfg)
	rawURL, err := url.Parse("https://example.test/image.png")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	c.Save(rawURL, &http.Response{StatusCode: http.StatusNotModified, Header: http.Header{}}, nil)
	if _, _, err := c.Load(rawURL); err == nil {
		t.Fatal("expected 304 response to be skipped")
	}

	path, _ := c.path(rawURL)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no cache file, got err %v", err)
	}
}

func TestLoadRemovesBodylessNotModifiedEntry(t *testing.T) {
	dir := t.TempDir()
	cfg := &cfgpkg.Config{Cache: cfgpkg.CacheConfig{Enabled: true, Directory: dir, TTL: 3600}}
	c := New(cfg)
	rawURL, err := url.Parse("https://example.test/image.png")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	path, _ := c.path(rawURL)
	if err := os.WriteFile(path, []byte(`{"url":"https://example.test/image.png","status":304,"header":{},"body":null,"stored_at_unix":1,"expires_at_unix":9999999999}`), 0o644); err != nil {
		t.Fatalf("write stale cache file: %v", err)
	}

	if _, _, err := c.Load(rawURL); err == nil {
		t.Fatal("expected stale 304 entry to be treated as a miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected stale 304 entry to be removed, got err %v", err)
	}
}

func TestSaveThenLoadIsCacheHit(t *testing.T) {
	dir := t.TempDir()
	cfg := &cfgpkg.Config{Cache: cfgpkg.CacheConfig{Enabled: true, Directory: dir, TTL: 3600}}
	c := New(cfg)
	rawURL, err := url.Parse("https://cdn.example.test/logo.png")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	body := []byte("fake png bytes")
	header := http.Header{}
	header.Set("Content-Type", "image/png")
	resp := &http.Response{StatusCode: http.StatusOK, Header: header}

	c.Save(rawURL, resp, body)

	// 第一次 Load：命中
	cr, _, err := c.Load(rawURL)
	if err != nil {
		t.Fatalf("expected cache hit after save, got err %v", err)
	}
	if cr == nil {
		t.Fatal("expected cached response, got nil")
	}
	if cr.Status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", cr.Status)
	}
	if string(cr.Body) != string(body) {
		t.Fatalf("expected body %q, got %q", body, cr.Body)
	}
	if got := cr.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("expected content-type image/png, got %q", got)
	}
	if cr.ExpiresAt <= cr.StoredAt {
		t.Fatalf("expected expiry after stored time, got stored %d expires %d", cr.StoredAt, cr.ExpiresAt)
	}

	// 第二次 Load：仍然命中且内容一致
	again, _, err := c.Load(rawURL)
	if err != nil {
		t.Fatalf("expected second cache hit, got err %v", err)
	}
	if string(again.Body) != string(body) || again.Status != http.StatusOK {
		t.Fatalf("expected stable cache entry, got status %d body %q", again.Status, again.Body)
	}

	// 缓存文件确实落盘
	path, _ := c.path(rawURL)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file on disk: %v", err)
	}
}

func TestLoadIsMissWithoutPriorSave(t *testing.T) {
	dir := t.TempDir()
	cfg := &cfgpkg.Config{Cache: cfgpkg.CacheConfig{Enabled: true, Directory: dir, TTL: 3600}}
	c := New(cfg)
	rawURL, err := url.Parse("https://cdn.example.test/missing.png")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	if _, _, err := c.Load(rawURL); err == nil {
		t.Fatal("expected cache miss for never-saved url")
	}

	path, _ := c.path(rawURL)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no cache file, got err %v", err)
	}
}

func TestLoadRemovesExpiredEntry(t *testing.T) {
	dir := t.TempDir()
	cfg := &cfgpkg.Config{Cache: cfgpkg.CacheConfig{Enabled: true, Directory: dir, TTL: 3600}}
	c := New(cfg)
	rawURL, err := url.Parse("https://cdn.example.test/stale.png")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	// 手工放置一条已过期的缓存（expires_at_unix=1 表示早已过期）
	path, _ := c.path(rawURL)
	stale := []byte(`{"url":"https://cdn.example.test/stale.png","status":200,"header":{"Content-Type":["image/png"]},"body":"c3RhbGU=","stored_at_unix":1,"expires_at_unix":1}`)
	if err := os.WriteFile(path, stale, 0o644); err != nil {
		t.Fatalf("write stale cache file: %v", err)
	}

	if _, _, err := c.Load(rawURL); err == nil {
		t.Fatal("expected expired entry to be treated as a miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected expired entry to be removed, got err %v", err)
	}
}

func TestShouldConsiderFilters(t *testing.T) {
	base := cfgpkg.CacheConfig{Enabled: true, Directory: t.TempDir(), TTL: 3600}

	tests := []struct {
		name   string
		cache  cfgpkg.CacheConfig
		method string
		url    string
		want   bool
	}{
		{"png with include extensions", cfgpkg.CacheConfig{Enabled: true, IncludeExtensions: []string{"jpg", "png", "css", "js"}}, "GET", "https://cdn.example.test/logo.png", true},
		{"html excluded by include extensions", cfgpkg.CacheConfig{Enabled: true, IncludeExtensions: []string{"jpg", "png", "css", "js"}}, "GET", "https://cdn.example.test/page.html", false},
		{"extensionless path with include extensions", cfgpkg.CacheConfig{Enabled: true, IncludeExtensions: []string{"jpg", "png"}}, "GET", "https://cdn.example.test/api/data", false},
		{"png with no extension filters", cfgpkg.CacheConfig{Enabled: true}, "GET", "https://cdn.example.test/logo.png", true},
		{"png excluded by exclude extensions", cfgpkg.CacheConfig{Enabled: true, ExcludeExtensions: []string{"png"}}, "GET", "https://cdn.example.test/logo.png", false},
		{"uppercase extension matches include list", cfgpkg.CacheConfig{Enabled: true, IncludeExtensions: []string{"PNG"}}, "GET", "https://cdn.example.test/logo.PNG", true},
		{"non-GET never considered", cfgpkg.CacheConfig{Enabled: true}, "POST", "https://cdn.example.test/logo.png", false},
		{"disabled never considered", cfgpkg.CacheConfig{Enabled: false}, "GET", "https://cdn.example.test/logo.png", false},
		{"host in include domains", cfgpkg.CacheConfig{Enabled: true, IncludeDomains: []string{"*.example.test"}}, "GET", "https://cdn.example.test/logo.png", true},
		{"host outside include domains", cfgpkg.CacheConfig{Enabled: true, IncludeDomains: []string{"*.example.test"}}, "GET", "https://other.org/logo.png", false},
		{"host in exclude domains", cfgpkg.CacheConfig{Enabled: true, ExcludeDomains: []string{"ads.example.test"}}, "GET", "https://ads.example.test/banner.png", false},
		{"host outside exclude domains", cfgpkg.CacheConfig{Enabled: true, ExcludeDomains: []string{"ads.example.test"}}, "GET", "https://cdn.example.test/logo.png", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &cfgpkg.Config{Cache: tc.cache}
			cfg.Cache.Directory = base.Directory
			c := New(cfg)
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			if got := c.ShouldConsider(req); got != tc.want {
				t.Errorf("ShouldConsider(%s %s) = %v, want %v", tc.method, tc.url, got, tc.want)
			}
		})
	}
}

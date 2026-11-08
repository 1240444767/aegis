package httptransport

import (
	"net/http/httptest"
	"testing"

	appdomain "aegis/internal/domain/app"
)

func TestAbsoluteBannerImages(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/apps/k/banners", nil)
	req.Host = "internal:8088"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "aegis.example.com")
	items := []appdomain.Banner{
		{ID: 1, HeaderDisplayURL: "/api/storage/proxy/abc.sig"},
		{ID: 2, HeaderDisplayURL: "https://cdn.example.com/x.png"},
		{ID: 3, HeaderDisplayURL: "//cdn.example.com/y.png"},
		{ID: 4},
	}
	absoluteBannerImages(req, items)
	want := []string{
		"https://aegis.example.com/api/storage/proxy/abc.sig",
		"https://cdn.example.com/x.png",
		"//cdn.example.com/y.png",
		"",
	}
	for i, item := range items {
		if item.HeaderDisplayURL != want[i] {
			t.Fatalf("banner %d: got %q, want %q", item.ID, item.HeaderDisplayURL, want[i])
		}
	}
}

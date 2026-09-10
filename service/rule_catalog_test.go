package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchRuleCatalogSourceFiltersSRSAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "NovaPanel-rule-catalog" {
			http.Error(w, "unexpected user agent", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"truncated":false,"tree":[{"path":"geosite-netflix.srs","type":"blob","size":42},{"path":"README.md","type":"blob","size":10},{"path":"geosite-empty.srs","type":"tree","size":0}]}`))
	}))
	defer server.Close()

	source := ruleCatalogSource{Kind: "geosite", Repository: "ignored", Prefix: "geosite-", RawBaseURL: "https://rules.example/"}
	client := server.Client()
	transport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return transport.RoundTrip(clone)
	})
	items, err := fetchRuleCatalogSource(context.Background(), client, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "netflix" || items[0].Tag != "geosite-netflix" || items[0].URL != "https://rules.example/geosite-netflix.srs" {
		t.Fatalf("unexpected catalog items: %#v", items)
	}
}

func TestSearchRuleCatalogRanksAndPaginatesCachedEntries(t *testing.T) {
	ruleCatalogCacheMu.Lock()
	original := ruleCatalogCache
	now := time.Now().UTC()
	ruleCatalogCache = map[string]ruleCatalogCacheEntry{
		"geosite": {FetchedAt: now, Items: []RuleCatalogEntry{
			{Kind: "geosite", Name: "xnet", Tag: "geosite-xnet"},
			{Kind: "geosite", Name: "net", Tag: "geosite-net"},
			{Kind: "geosite", Name: "netflix", Tag: "geosite-netflix"},
		}},
		"geoip": {FetchedAt: now, Items: []RuleCatalogEntry{
			{Kind: "geoip", Name: "internet", Tag: "geoip-internet"},
		}},
	}
	ruleCatalogCacheMu.Unlock()
	t.Cleanup(func() {
		ruleCatalogCacheMu.Lock()
		ruleCatalogCache = original
		ruleCatalogCacheMu.Unlock()
	})

	first, err := SearchRuleCatalog(context.Background(), "net", "all", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 4 || len(first.Items) != 2 || first.Items[0].Name != "net" || first.Items[1].Name != "netflix" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := SearchRuleCatalog(context.Background(), "net", "all", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 2 || second.Items[0].Name != "internet" || second.Items[1].Name != "xnet" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if _, err := SearchRuleCatalog(context.Background(), "net", "unsupported", 1, 10); err == nil {
		t.Fatal("expected unsupported catalog kind to fail")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

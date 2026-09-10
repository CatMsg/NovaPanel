package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ruleCatalogCacheTTL = 6 * time.Hour
	ruleCatalogBodyMax  = 8 << 20
)

type RuleCatalogEntry struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Tag  string `json:"tag"`
	URL  string `json:"url"`
	Size int64  `json:"size,omitempty"`
}

type RuleCatalogSearchResult struct {
	Items    []RuleCatalogEntry `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
	CachedAt time.Time          `json:"cachedAt"`
}

type ruleCatalogSource struct {
	Kind       string
	Repository string
	Prefix     string
	RawBaseURL string
}

type ruleCatalogCacheEntry struct {
	Items     []RuleCatalogEntry
	FetchedAt time.Time
}

var (
	ruleCatalogHTTPClient = &http.Client{Timeout: 12 * time.Second}
	ruleCatalogSources    = []ruleCatalogSource{
		{Kind: "geosite", Repository: "SagerNet/sing-geosite", Prefix: "geosite-", RawBaseURL: "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/"},
		{Kind: "geoip", Repository: "SagerNet/sing-geoip", Prefix: "geoip-", RawBaseURL: "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/"},
	}
	ruleCatalogCacheMu sync.RWMutex
	ruleCatalogCache   = make(map[string]ruleCatalogCacheEntry)
)

func SearchRuleCatalog(ctx context.Context, query, kind string, page, pageSize int) (RuleCatalogSearchResult, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "all"
	}
	if kind != "all" && kind != "geosite" && kind != "geoip" {
		return RuleCatalogSearchResult{}, fmt.Errorf("unsupported rule catalog kind: %s", kind)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 48
	}
	if pageSize > 100 {
		pageSize = 100
	}

	selectedSources := make([]ruleCatalogSource, 0, len(ruleCatalogSources))
	for _, source := range ruleCatalogSources {
		if kind != "all" && source.Kind != kind {
			continue
		}
		selectedSources = append(selectedSources, source)
	}
	type sourceResult struct {
		items     []RuleCatalogEntry
		fetchedAt time.Time
		err       error
	}
	results := make(chan sourceResult, len(selectedSources))
	for _, source := range selectedSources {
		source := source
		go func() {
			items, fetchedAt, err := loadRuleCatalogSource(ctx, source)
			results <- sourceResult{items: items, fetchedAt: fetchedAt, err: err}
		}()
	}
	items := make([]RuleCatalogEntry, 0)
	var cachedAt time.Time
	for range selectedSources {
		result := <-results
		if result.err != nil {
			return RuleCatalogSearchResult{}, result.err
		}
		items = append(items, result.items...)
		if cachedAt.IsZero() || result.fetchedAt.Before(cachedAt) {
			cachedAt = result.fetchedAt
		}
	}

	query = strings.ToLower(strings.TrimSpace(query))
	filtered := items[:0]
	for _, item := range items {
		if query == "" || strings.Contains(strings.ToLower(item.Name), query) || strings.Contains(strings.ToLower(item.Tag), query) {
			filtered = append(filtered, item)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		leftScore := ruleCatalogMatchScore(filtered[i], query)
		rightScore := ruleCatalogMatchScore(filtered[j], query)
		if leftScore != rightScore {
			return leftScore < rightScore
		}
		if filtered[i].Kind != filtered[j].Kind {
			return filtered[i].Kind < filtered[j].Kind
		}
		return filtered[i].Name < filtered[j].Name
	})

	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return RuleCatalogSearchResult{
		Items:    append([]RuleCatalogEntry(nil), filtered[start:end]...),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		CachedAt: cachedAt,
	}, nil
}

func ruleCatalogMatchScore(item RuleCatalogEntry, query string) int {
	if query == "" {
		return 3
	}
	name := strings.ToLower(item.Name)
	if name == query {
		return 0
	}
	if strings.HasPrefix(name, query) {
		return 1
	}
	return 2
}

func loadRuleCatalogSource(ctx context.Context, source ruleCatalogSource) ([]RuleCatalogEntry, time.Time, error) {
	ruleCatalogCacheMu.RLock()
	cached, ok := ruleCatalogCache[source.Kind]
	ruleCatalogCacheMu.RUnlock()
	if ok && time.Since(cached.FetchedAt) < ruleCatalogCacheTTL {
		return append([]RuleCatalogEntry(nil), cached.Items...), cached.FetchedAt, nil
	}

	items, err := fetchRuleCatalogSource(ctx, ruleCatalogHTTPClient, source)
	if err != nil {
		if ok && len(cached.Items) > 0 {
			return append([]RuleCatalogEntry(nil), cached.Items...), cached.FetchedAt, nil
		}
		return nil, time.Time{}, err
	}
	fetchedAt := time.Now().UTC()
	ruleCatalogCacheMu.Lock()
	ruleCatalogCache[source.Kind] = ruleCatalogCacheEntry{Items: append([]RuleCatalogEntry(nil), items...), FetchedAt: fetchedAt}
	ruleCatalogCacheMu.Unlock()
	return items, fetchedAt, nil
}

func fetchRuleCatalogSource(ctx context.Context, client *http.Client, source ruleCatalogSource) ([]RuleCatalogEntry, error) {
	endpoint := "https://api.github.com/repos/" + source.Repository + "/git/trees/rule-set"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NovaPanel-rule-catalog")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("load %s rule catalog: %w", source.Kind, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("load %s rule catalog: GitHub returned HTTP %d", source.Kind, resp.StatusCode)
	}
	var payload struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Size int64  `json:"size"`
		} `json:"tree"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, ruleCatalogBodyMax))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s rule catalog: %w", source.Kind, err)
	}
	if payload.Truncated {
		return nil, errors.New("GitHub returned a truncated rule catalog")
	}
	items := make([]RuleCatalogEntry, 0, len(payload.Tree))
	for _, file := range payload.Tree {
		if file.Type != "blob" || !strings.HasPrefix(file.Path, source.Prefix) || !strings.HasSuffix(file.Path, ".srs") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(file.Path, source.Prefix), ".srs")
		if name == "" {
			continue
		}
		items = append(items, RuleCatalogEntry{
			Kind: source.Kind,
			Name: name,
			Tag:  source.Prefix + name,
			URL:  source.RawBaseURL + url.PathEscape(file.Path),
			Size: file.Size,
		})
	}
	return items, nil
}

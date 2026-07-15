package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/moti/moticlaw/internal/security"
)

// SearchResult はWeb検索の結果を表す
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchTool はWeb検索を行うツール
// プライマリ: DuckDuckGo HTMLスクレイピング（APIキー不要）
// フォールバック: Tavily Keyless API（APIキー不要）
type WebSearchTool struct {
	client    *http.Client
	rateLimit *security.RateLimiter
	cache     *URLCache
}

// NewWebSearchTool は新しいWebSearchToolを作成する
// limiter は共有のレート制限インスタンス（Botレベルで管理）
func NewWebSearchTool(limiter *security.RateLimiter) *WebSearchTool {
	return &WebSearchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimit: limiter,
		cache:     NewURLCache(60 * time.Second),
	}
}

// Name はツール名を返す
func (t *WebSearchTool) Name() string {
	return "web_search"
}

// Description はツールの説明を返す
func (t *WebSearchTool) Description() string {
	return "Web検索を行います。DuckDuckGo（プライマリ）+ Tavily（フォールバック）を使用します。APIキーは不要です。"
}

// Execute はWeb検索を行う
// DuckDuckGo HTMLスクレイピングをプライマリとして試行し、
// 失敗した場合はTavily Keyless APIをフォールバックとして使用する。
func (t *WebSearchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	query, ok := params["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query parameter is required and must be a non-empty string")
	}
	query = strings.TrimSpace(query)

	// レート制限を確認
	if !t.rateLimit.Allow() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// キャッシュを確認
	cacheKey := "search:" + query
	if cached, ok := t.cache.Get(cacheKey); ok {
		return cached, nil
	}

	// プライマリ: DuckDuckGo
	results, err := t.searchDuckDuckGo(ctx, query)
	if err != nil {
		// フォールバック: Tavily
		results, err = t.searchTavily(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("all search backends failed: %w", err)
		}
	}

	// 結果を構築
	result := map[string]interface{}{
		"results": results,
	}

	// キャッシュに保存
	t.cache.Set(cacheKey, result)

	return result, nil
}

// searchDuckDuckGo はDuckDuckGoのHTMLエンドポイントを使って検索を行う
func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string) ([]SearchResult, error) {
	formData := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html",
		strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create DuckDuckGo request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; moticlaw/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DuckDuckGo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("DuckDuckGo rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	// レスポンスサイズ制限（512KB）
	limitedReader := io.LimitReader(resp.Body, 512*1024)

	doc, err := goquery.NewDocumentFromReader(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DuckDuckGo HTML: %w", err)
	}

	results := parseDuckDuckGoHTML(doc)
	if len(results) == 0 {
		return nil, fmt.Errorf("DuckDuckGo returned no results")
	}

	return results, nil
}

// parseDuckDuckGoHTML はDuckDuckGoのHTMLレスポンスをパースして検索結果を返す
// テスト容易性のために分離
func parseDuckDuckGoHTML(doc *goquery.Document) []SearchResult {
	var results []SearchResult

	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if i >= 10 {
			return
		}

		// タイトルとURLを抽出
		var title, href string
		titleSel := s.Find(".result__title a.result__a")
		if titleSel.Length() == 0 {
			titleSel = s.Find(".result__title a")
		}
		if titleSel.Length() > 0 {
			title = strings.TrimSpace(titleSel.Text())
			href, _ = titleSel.Attr("href")
		}

		if title == "" || href == "" {
			return
		}

		// DuckDuckGoのリダイレクトURLから実際のURLを抽出
		href = cleanDuckDuckGoURL(href)

		// スニペットを抽出
		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())
		if snippet == "" {
			snippet = strings.TrimSpace(s.Find(".snippet").Text())
		}

		results = append(results, SearchResult{
			Title:   title,
			URL:     href,
			Snippet: snippet,
		})
	})

	return results
}

// cleanDuckDuckGoURL はDuckDuckGoのリダイレクトラッパーURLから実際のURLを抽出する
// DuckDuckGoは検索結果のリンクを `//duckduckgo.com/l/?uddg=<encoded_url>&rut=...` の形式でラップする
func cleanDuckDuckGoURL(rawURL string) string {
	if strings.Contains(rawURL, "uddg=") {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return rawURL
		}
		// uddgパラメータが実際のURL
		if u := parsed.Query().Get("uddg"); u != "" {
			decoded, err := url.QueryUnescape(u)
			if err == nil {
				return decoded
			}
			return u
		}
	}
	return rawURL
}

// searchTavily はTavily Keyless APIを使って検索を行う（フォールバック）
func (t *WebSearchTool) searchTavily(ctx context.Context, query string) ([]SearchResult, error) {
	reqBody := map[string]interface{}{
		"query":       query,
		"max_results": 10,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Tavily request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search",
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create Tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tavily-Access-Mode", "keyless")
	req.Header.Set("User-Agent", "moticlaw/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// エラーレスポンスのボディを読み取る（1KB制限）
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("Tavily returned status %d: %s", resp.StatusCode,
			strings.TrimSpace(string(errBody)))
	}

	results, err := parseTavilyResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("Tavily returned no results")
	}

	return results, nil
}

// tavilyResponse はTavily APIのレスポンス構造
type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

// parseTavilyResponse はTavily APIのJSONレスポンスをパースする
// テスト容易性のために分離
func parseTavilyResponse(r io.Reader) ([]SearchResult, error) {
	var resp tavilyResponse
	if err := json.NewDecoder(io.LimitReader(r, 512*1024)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode Tavily response: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}
	return results, nil
}

// URLCache はURLのキャッシュ
type URLCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
	maxSize int
}

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

// NewURLCache は新しいURLキャッシュを作成する
func NewURLCache(ttl time.Duration) *URLCache {
	return &URLCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		maxSize: 1000,
	}
}

// Get はキャッシュからデータを取得する
func (c *URLCache) Get(url string) (interface{}, bool) {
	c.mu.RLock()
	entry, ok := c.entries[url]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

// Set はキャッシュにデータを保存する
func (c *URLCache) Set(url string, data interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// サイズ制限を超えた場合、期限切れエントリを削除
	if len(c.entries) >= c.maxSize {
		c.evictLocked()
	}

	c.entries[url] = cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// evictLocked は期限切れエントリを削除する（mu.Lock済みであること）
func (c *URLCache) evictLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}



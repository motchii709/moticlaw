package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/moti/moticlaw/internal/security"
)

// WebFetchTool はWebコンテンツを取得するツール
type WebFetchTool struct {
	client     *http.Client
	ssrfGuard  *security.SSRFGuard
	rateLimit  *security.RateLimiter
	cache      *URLCache
}

// NewWebFetchTool は新しいWebFetchToolを作成する
// limiter は共有のレート制限インスタンス（Botレベルで管理）
func NewWebFetchTool(limiter *security.RateLimiter) *WebFetchTool {
	return &WebFetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		ssrfGuard: security.NewSSRFGuard(),
		rateLimit: limiter,
		cache:     NewURLCache(60 * time.Second),
	}
}

// Name はツール名を返す
func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

// Description はツールの説明を返す
func (t *WebFetchTool) Description() string {
	return "Webコンテンツを取得します。レート制限とSSRFガードがあります。"
}

// Execute はWebコンテンツを取得する
func (t *WebFetchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	rawURL, ok := params["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url parameter is required")
	}

	// レート制限を確認
	if !t.rateLimit.Allow() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// キャッシュを確認
	if cached, ok := t.cache.Get(rawURL); ok {
		return cached, nil
	}

	// SSRFガードを確認
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	if !t.ssrfGuard.IsSafeHost(parsedURL.Host) {
		return nil, fmt.Errorf("access to this URL is not allowed")
	}

	// リクエストを作成
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// リクエストを送信
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// レスポンスサイズを制限（1MB）
	limitedReader := io.LimitReader(resp.Body, 1024*1024)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// レスポンスを構築
	result := map[string]interface{}{
		"url":      rawURL,
		"status":   resp.StatusCode,
		"content":  string(body),
		"headers":  resp.Header,
	}

	// キャッシュに保存
	t.cache.Set(rawURL, result)

	return result, nil
}


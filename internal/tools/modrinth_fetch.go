package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ModrinthFetchTool はModrinth APIを叩くツール
type ModrinthFetchTool struct {
	client *http.Client
}

// NewModrinthFetchTool は新しいModrinthFetchToolを作成する
func NewModrinthFetchTool() *ModrinthFetchTool {
	return &ModrinthFetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name はツール名を返す
func (t *ModrinthFetchTool) Name() string {
	return "modrinth_fetch"
}

// Description はツールの説明を返す
func (t *ModrinthFetchTool) Description() string {
	return "Modrinth APIを叩きます。接続先はapi.modrinth.comに固定です。"
}

// Execute はModrinth APIを叩く
func (t *ModrinthFetchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	endpoint, ok := params["endpoint"].(string)
	if !ok {
		return nil, fmt.Errorf("endpoint parameter is required")
	}

	// URLを構築
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	rawURL := fmt.Sprintf("https://api.modrinth.com%s", endpoint)

	// SSRF対策: URLをパースし、Hostが正しいことを確認する
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	if parsedURL.Host != "api.modrinth.com" && !strings.HasPrefix(parsedURL.Host, "api.modrinth.com.") {
		return nil, fmt.Errorf("invalid endpoint: host must be api.modrinth.com, got %q", parsedURL.Host)
	}

	// リクエストを作成（パース済みURLを使うことで再エンコードの安全性を確保）
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// ユーザーエージェントを設定
	req.Header.Set("User-Agent", "moticlaw")

	// リクエストを送信
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// レスポンスボディを読み取り
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// JSONをパース
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return map[string]interface{}{
		"status": resp.StatusCode,
		"data":   result,
	}, nil
}

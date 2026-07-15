package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GithubFetchTool はGitHub APIを叩くツール
type GithubFetchTool struct {
	client *http.Client
}

// NewGithubFetchTool は新しいGithubFetchToolを作成する
func NewGithubFetchTool() *GithubFetchTool {
	return &GithubFetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name はツール名を返す
func (t *GithubFetchTool) Name() string {
	return "github_fetch"
}

// Description はツールの説明を返す
func (t *GithubFetchTool) Description() string {
	return "GitHub APIを叩きます。接続先はapi.github.comに固定です。"
}

// Execute はGitHub APIを叩く。actionが"trending"の場合はGitHub Trendingをスクレイピングする
func (t *GithubFetchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	// action パラメータを確認
	action, _ := params["action"].(string)

	if action == "trending" {
		language, _ := params["language"].(string)
		since, _ := params["since"].(string)

		results, err := scrapeTrending(ctx, language, since)
		if err != nil {
			return nil, fmt.Errorf("trending scrape failed: %w", err)
		}

		return map[string]interface{}{
			"status": 200,
			"data":   results,
		}, nil
	}

	// 従来のREST API動作（actionが空または未指定）
	endpoint, ok := params["endpoint"].(string)
	if !ok {
		return nil, fmt.Errorf("endpoint parameter is required when action is not specified")
	}

	// endpointが/から始まることを保証
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}

	// URLを構築
	rawURL := fmt.Sprintf("https://api.github.com%s", endpoint)

	// SSRF対策: URLをパースし、Hostがapi.github.comであることを確認
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if parsedURL.Host != "api.github.com" && !strings.HasPrefix(parsedURL.Host, "api.github.com.") {
		return nil, fmt.Errorf("endpoint host %q is not allowed; only api.github.com is permitted", parsedURL.Host)
	}

	// リクエストを作成
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// GitHub PATが設定されていれば追加
	if pat := os.Getenv("GITHUB_PAT"); pat != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", pat))
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

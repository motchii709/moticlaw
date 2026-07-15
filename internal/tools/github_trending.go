package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// scrapeTrending はGitHub Trendingページをスクレイピングする
// language: プログラミング言語（例: "go", "python"）、空文字の場合は全言語
// since: 期間（"daily", "weekly", "monthly"）、空文字の場合はdaily扱い
func scrapeTrending(ctx context.Context, language, since string) ([]map[string]interface{}, error) {
	// URLを構築
	url := "https://github.com/trending"
	if language != "" {
		url += "/" + language
	}
	if since != "" {
		url += "?since=" + since
	}

	// HTTPクライアントを作成
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// リクエストを作成
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "moticlaw")
	req.Header.Set("Accept", "text/html")

	// リクエストを送信
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trending page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub Trending returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return parseTrendingHTML(string(body)), nil
}

// パッケージレベルの正規表現 — init時に一度だけコンパイル
var (
	articleRe = regexp.MustCompile(`(?s:<article\s+class="Box-row"[^>]*>(.*?)</article>)`)
	repoNameRe = regexp.MustCompile(`<a\s+href="/([^"/]+/[^"/?]+)`)
	descRe    = regexp.MustCompile(`(?s:<p\s+class="col-9[^"]*"[^>]*>(.*?)</p>)`)
	langRe    = regexp.MustCompile(`itemprop="programmingLanguage">([^<]+)`)
	starsRe   = regexp.MustCompile(`(?s:stargazers[^>]*>.*?>?([\d,]+)\s*<)`)
	periodRe  = regexp.MustCompile(`([\d,]+)\s+stars\s+(today|this week|this month)`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
)

// parseTrendingHTML はGitHub TrendingページのHTMLをパースしてリポジトリ一覧を返す
func parseTrendingHTML(html string) []map[string]interface{} {
	articles := articleRe.FindAllStringSubmatch(html, -1)
	results := make([]map[string]interface{}, 0, len(articles))

	for _, article := range articles {
		content := article[1]
		repo := make(map[string]interface{})

		// リポジトリ名の抽出
		if m := repoNameRe.FindStringSubmatch(content); len(m) >= 2 {
			repo["name"] = m[1]
		} else {
			// nameがない場合はスキップ
			continue
		}

		// 説明文の抽出
		if m := descRe.FindStringSubmatch(content); len(m) >= 2 {
			desc := strings.TrimSpace(m[1])
			// HTMLタグを除去
			desc = tagRe.ReplaceAllString(desc, "")
			desc = strings.TrimSpace(desc)
			if desc != "" {
				repo["description"] = desc
			}
		}

		// 使用言語の抽出
		if m := langRe.FindStringSubmatch(content); len(m) >= 2 {
			repo["language"] = strings.TrimSpace(m[1])
		}

		// スター数の抽出
		if m := starsRe.FindStringSubmatch(content); len(m) >= 2 {
			repo["stars"] = m[1]
		}

		// 期間内スター数の抽出
		if m := periodRe.FindStringSubmatch(content); len(m) >= 3 {
			repo["period_stars"] = m[1]
			repo["period"] = m[2]
		}

		results = append(results, repo)
	}

	return results
}

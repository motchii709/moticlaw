package tools

import (
	"context"
	"testing"
)

func TestParseTrendingHTML(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
		check    func(t *testing.T, results []map[string]interface{})
	}{
		{
			name:     "empty HTML",
			html:     "",
			expected: 0,
			check:    nil,
		},
		{
			name: "no articles",
			html: `<html><body><div>No trending repos</div></body></html>`,
			expected: 0,
			check:    nil,
		},
		{
			name: "single repo with all fields",
			html: `<html>
<body>
  <article class="Box-row">
    <h2 class="h3 lh-condensed">
      <a href="/octocat/hello-world">octocat / <strong>hello-world</strong></a>
    </h2>
    <p class="col-9 color-fg-muted my-1 pr-4">
      A simple hello world repository
    </p>
    <div class="f6 color-fg-muted mt-2">
      <span class="d-inline-block ml-0 mr-3">
        <span class="repo-language-color" style="background-color: #f1e05a"></span>
        <span itemprop="programmingLanguage">Go</span>
      </span>
      <a href="/octocat/hello-world/stargazers">
        <svg class="octicon octicon-star" aria-hidden="true"><path d="..."/></svg>
        1,234
      </a>
      <span class="d-inline-block float-sm-right">
        <svg class="octicon octicon-star" aria-hidden="true"><path d="..."/></svg>
        56 stars today
      </span>
    </div>
  </article>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []map[string]interface{}) {
				r := results[0]
				if r["name"] != "octocat/hello-world" {
					t.Errorf("expected name 'octocat/hello-world', got %v", r["name"])
				}
				if r["description"] != "A simple hello world repository" {
					t.Errorf("expected description 'A simple hello world repository', got %v", r["description"])
				}
				if r["language"] != "Go" {
					t.Errorf("expected language 'Go', got %v", r["language"])
				}
				if r["stars"] != "1,234" {
					t.Errorf("expected stars '1,234', got %v", r["stars"])
				}
				if r["period_stars"] != "56" {
					t.Errorf("expected period_stars '56', got %v", r["period_stars"])
				}
				if r["period"] != "today" {
					t.Errorf("expected period 'today', got %v", r["period"])
				}
			},
		},
		{
			name: "weekly period",
			html: `<html>
<body>
  <article class="Box-row">
    <h2 class="h3 lh-condensed">
      <a href="/user/repo">user / <strong>repo</strong></a>
    </h2>
    <p class="col-9 color-fg-muted my-1 pr-4">
      Weekly trending repo
    </p>
    <div class="f6 color-fg-muted mt-2">
      <span class="d-inline-block ml-0 mr-3">
        <span itemprop="programmingLanguage">Python</span>
      </span>
      <a href="/user/repo/stargazers">
        <svg><path d="..."/></svg>
        5,432
      </a>
      <span class="d-inline-block float-sm-right">
        <svg><path d="..."/></svg>
        789 stars this week
      </span>
    </div>
  </article>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []map[string]interface{}) {
				r := results[0]
				if r["name"] != "user/repo" {
					t.Errorf("expected name 'user/repo', got %v", r["name"])
				}
				if r["language"] != "Python" {
					t.Errorf("expected language 'Python', got %v", r["language"])
				}
				if r["stars"] != "5,432" {
					t.Errorf("expected stars '5,432', got %v", r["stars"])
				}
				if r["period_stars"] != "789" {
					t.Errorf("expected period_stars '789', got %v", r["period_stars"])
				}
				if r["period"] != "this week" {
					t.Errorf("expected period 'this week', got %v", r["period"])
				}
			},
		},
		{
			name: "multiple repos",
			html: `<html>
<body>
  <article class="Box-row">
    <h2 class="h3 lh-condensed">
      <a href="/user/first">user / <strong>first</strong></a>
    </h2>
    <p class="col-9 color-fg-muted my-1 pr-4">First repo</p>
    <div class="f6 color-fg-muted mt-2">
      <a href="/user/first/stargazers"><svg></svg>100</a>
      <span class="d-inline-block float-sm-right"><svg></svg>10 stars today</span>
    </div>
  </article>
  <article class="Box-row">
    <h2 class="h3 lh-condensed">
      <a href="/user/second">user / <strong>second</strong></a>
    </h2>
    <p class="col-9 color-fg-muted my-1 pr-4">Second repo</p>
    <div class="f6 color-fg-muted mt-2">
      <a href="/user/second/stargazers"><svg></svg>200</a>
      <span class="d-inline-block float-sm-right"><svg></svg>20 stars today</span>
    </div>
  </article>
</body>
</html>`,
			expected: 2,
			check: func(t *testing.T, results []map[string]interface{}) {
				if results[0]["name"] != "user/first" {
					t.Errorf("expected first repo 'user/first', got %v", results[0]["name"])
				}
				if results[1]["name"] != "user/second" {
					t.Errorf("expected second repo 'user/second', got %v", results[1]["name"])
				}
			},
		},
		{
			name: "repo without description and language",
			html: `<html>
<body>
  <article class="Box-row">
    <h2 class="h3 lh-condensed">
      <a href="/user/minimal">user / <strong>minimal</strong></a>
    </h2>
    <div class="f6 color-fg-muted mt-2">
      <a href="/user/minimal/stargazers"><svg></svg>42</a>
      <span class="d-inline-block float-sm-right"><svg></svg>5 stars today</span>
    </div>
  </article>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []map[string]interface{}) {
				r := results[0]
				if r["name"] != "user/minimal" {
					t.Errorf("expected name 'user/minimal', got %v", r["name"])
				}
				// description と言語は省略された場合、キーが存在しないことを確認
				if _, ok := r["description"]; ok {
					t.Errorf("expected no description, got %v", r["description"])
				}
				if _, ok := r["language"]; ok {
					t.Errorf("expected no language, got %v", r["language"])
				}
				if r["stars"] != "42" {
					t.Errorf("expected stars '42', got %v", r["stars"])
				}
			},
		},
		{
			name: "repo name with URL params in href",
			html: `<html>
<body>
  <article class="Box-row">
    <h2 class="h3 lh-condensed">
      <a href="/user/repo?from=trending">user / <strong>repo</strong></a>
    </h2>
    <div class="f6 color-fg-muted mt-2">
      <a href="/user/repo/stargazers"><svg></svg>100</a>
      <span class="d-inline-block float-sm-right"><svg></svg>1 star today</span>
    </div>
  </article>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []map[string]interface{}) {
				// href に `?from=trending` が付いている場合、クエリパラメータを除去した名前になること
				if results[0]["name"] != "user/repo" {
					t.Errorf("expected name 'user/repo', got %v", results[0]["name"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := parseTrendingHTML(tt.html)
			if len(results) != tt.expected {
				t.Errorf("expected %d repos, got %d", tt.expected, len(results))
			}
			if tt.check != nil && len(results) > 0 {
				tt.check(t, results)
			}
		})
	}
}

func TestScrapeTrending_CanceledContext(t *testing.T) {
	// キャンセルされたコンテキストでの動作確認
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即座にキャンセル

	_, err := scrapeTrending(ctx, "go", "daily")
	if err == nil {
		t.Error("expected error with canceled context, got nil")
	}
}

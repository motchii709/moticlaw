package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/moti/moticlaw/internal/security"
)

func TestParseDuckDuckGoHTML(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
		check    func(t *testing.T, results []SearchResult)
	}{
		{
			name:     "empty HTML",
			html:     "",
			expected: 0,
			check:    nil,
		},
		{
			name: "no results",
			html: `<html><body><div class="no-results">No results found</div></body></html>`,
			expected: 0,
			check:    nil,
		},
		{
			name: "single result with all fields",
			html: `<html>
<body>
  <div class="results">
    <div class="result">
      <div class="result__body">
        <h2 class="result__title">
          <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&amp;rut=abc123">Example Title</a>
        </h2>
        <div class="result__snippet">This is an example snippet for the search result.</div>
        <div class="result__extras">
          <span class="result__url">example.com</span>
        </div>
      </div>
    </div>
  </div>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				r := results[0]
				if r.Title != "Example Title" {
					t.Errorf("expected title 'Example Title', got %q", r.Title)
				}
				if r.URL != "https://example.com" {
					t.Errorf("expected URL 'https://example.com', got %q", r.URL)
				}
				if r.Snippet != "This is an example snippet for the search result." {
					t.Errorf("expected snippet 'This is an example snippet for the search result.', got %q", r.Snippet)
				}
			},
		},
		{
			name: "multiple results limited to 10",
			html: func() string {
				var sb strings.Builder
				sb.WriteString("<html><body><div class=\"results\">")
				for i := 0; i < 15; i++ {
					sb.WriteString(fmt.Sprintf(`<div class="result">
						<div class="result__body">
							<h2 class="result__title">
								<a class="result__a" href="//duckduckgo.com/l/?uddg=https%%3A%%2F%%2Fexample%d.com">Result %d</a>
							</h2>
							<div class="result__snippet">Snippet %d</div>
						</div>
					</div>`, i, i, i))
				}
				sb.WriteString("</div></body></html>")
				return sb.String()
			}(),
			expected: 10,
			check: func(t *testing.T, results []SearchResult) {
				if len(results) > 10 {
					t.Errorf("expected at most 10 results, got %d", len(results))
				}
			},
		},
		{
			name: "result without snippet",
			html: `<html>
<body>
  <div class="results">
    <div class="result">
      <div class="result__body">
        <h2 class="result__title">
          <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com">Title Only</a>
        </h2>
      </div>
    </div>
  </div>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				r := results[0]
				if r.Title != "Title Only" {
					t.Errorf("expected title 'Title Only', got %q", r.Title)
				}
				if r.URL != "https://example.com" {
					t.Errorf("expected URL 'https://example.com', got %q", r.URL)
				}
				if r.Snippet != "" {
					t.Errorf("expected empty snippet, got %q", r.Snippet)
				}
			},
		},
		{
			name: "direct URL without redirect wrapper",
			html: `<html>
<body>
  <div class="results">
    <div class="result">
      <div class="result__body">
        <h2 class="result__title">
          <a class="result__a" href="https://direct.example.com/page">Direct URL Result</a>
        </h2>
        <div class="result__snippet">Direct URL snippet</div>
      </div>
    </div>
  </div>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				r := results[0]
				if r.Title != "Direct URL Result" {
					t.Errorf("expected title 'Direct URL Result', got %q", r.Title)
				}
				if r.URL != "https://direct.example.com/page" {
					t.Errorf("expected URL 'https://direct.example.com/page', got %q", r.URL)
				}
				if r.Snippet != "Direct URL snippet" {
					t.Errorf("expected snippet 'Direct URL snippet', got %q", r.Snippet)
				}
			},
		},
		{
			name: "missing title stops processing",
			html: `<html>
<body>
  <div class="results">
    <div class="result">
      <div class="result__body">
        <h2 class="result__title">
          <a class="result__a" href=""></a>
        </h2>
        <div class="result__snippet">Empty title</div>
      </div>
    </div>
    <div class="result">
      <div class="result__body">
        <h2 class="result__title">
          <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fvalid.example.com">Valid Result</a>
        </h2>
        <div class="result__snippet">Good result</div>
      </div>
    </div>
  </div>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				if len(results) != 1 {
					t.Fatalf("expected 1 result, got %d", len(results))
				}
				if results[0].Title != "Valid Result" {
					t.Errorf("expected title 'Valid Result', got %q", results[0].Title)
				}
			},
		},
		{
			name: "snippet in .snippet class fallback",
			html: `<html>
<body>
  <div class="results">
    <div class="result">
      <div class="result__body">
        <h2 class="result__title">
          <a class="result__a" href="/l/?uddg=https%3A%2F%2Fsnippet.example.com">Snippet Fallback</a>
        </h2>
        <div class="snippet">Fallback snippet text</div>
      </div>
    </div>
  </div>
</body>
</html>`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				r := results[0]
				if r.Snippet != "Fallback snippet text" {
					t.Errorf("expected snippet 'Fallback snippet text', got %q", r.Snippet)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatalf("failed to parse HTML: %v", err)
			}
			results := parseDuckDuckGoHTML(doc)
			if len(results) != tt.expected {
				t.Errorf("expected %d results, got %d", tt.expected, len(results))
			}
			if tt.check != nil && len(results) > 0 {
				tt.check(t, results)
			}
		})
	}
}

func TestParseTavilyResponse(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected int
		check    func(t *testing.T, results []SearchResult)
		wantErr  bool
	}{
		{
			name:     "empty JSON object",
			json:     `{}`,
			expected: 0,
			check:    nil,
			wantErr:  false,
		},
		{
			name: "single result",
			json: `{
				"results": [
					{
						"title": "Tavily Result",
						"url": "https://tavily.example.com",
						"content": "This is the content from Tavily"
					}
				]
			}`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				r := results[0]
				if r.Title != "Tavily Result" {
					t.Errorf("expected title 'Tavily Result', got %q", r.Title)
				}
				if r.URL != "https://tavily.example.com" {
					t.Errorf("expected URL 'https://tavily.example.com', got %q", r.URL)
				}
				if r.Snippet != "This is the content from Tavily" {
					t.Errorf("expected snippet 'This is the content from Tavily', got %q", r.Snippet)
				}
			},
			wantErr: false,
		},
		{
			name: "multiple results",
			json: `{
				"results": [
					{"title": "Result 1", "url": "https://example.com/1", "content": "Content 1"},
					{"title": "Result 2", "url": "https://example.com/2", "content": "Content 2"},
					{"title": "Result 3", "url": "https://example.com/3", "content": "Content 3"}
				]
			}`,
			expected: 3,
			check: func(t *testing.T, results []SearchResult) {
				if results[0].Title != "Result 1" {
					t.Errorf("expected first title 'Result 1', got %q", results[0].Title)
				}
				if results[2].Title != "Result 3" {
					t.Errorf("expected third title 'Result 3', got %q", results[2].Title)
				}
			},
			wantErr: false,
		},
		{
			name:     "invalid JSON",
			json:     `{broken json`,
			expected: 0,
			check:    nil,
			wantErr:  true,
		},
		{
			name: "result with empty fields",
			json: `{
				"results": [
					{"title": "", "url": "", "content": ""}
				]
			}`,
			expected: 1,
			check: func(t *testing.T, results []SearchResult) {
				if results[0].Title != "" {
					t.Errorf("expected empty title, got %q", results[0].Title)
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := parseTavilyResponse(strings.NewReader(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTavilyResponse() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if len(results) != tt.expected {
				t.Errorf("expected %d results, got %d", tt.expected, len(results))
			}
			if tt.check != nil && len(results) > 0 {
				tt.check(t, results)
			}
		})
	}
}

func TestCleanDuckDuckGoURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "redirect URL with uddg",
			url:  "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage&rut=abc123",
			want: "https://example.com/page",
		},
		{
			name: "redirect URL with encoded special chars",
			url:  "/l/?uddg=https%3A%2F%2Fgithub.com%2Fmoti%2Fmoticlaw%3Ftab%3Dreadme",
			want: "https://github.com/moti/moticlaw?tab=readme",
		},
		{
			name: "direct URL (no redirect)",
			url:  "https://direct.example.com/page",
			want: "https://direct.example.com/page",
		},
		{
			name: "empty URL",
			url:  "",
			want: "",
		},
		{
			name: "URL without uddg param",
			url:  "//duckduckgo.com/l/?other=param",
			want: "//duckduckgo.com/l/?other=param",
		},
		{
			name: "relative path URL",
			url:  "/path/to/page",
			want: "/path/to/page",
		},
		{
			name: "URL with uddg but no scheme",
			url:  "/l/?uddg=example.com",
			want: "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanDuckDuckGoURL(tt.url)
			if got != tt.want {
				t.Errorf("cleanDuckDuckGoURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestWebSearchTool_Execute_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool(security.NewRateLimiter(10))
	defer tool.rateLimit.Stop()
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

func TestWebSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := NewWebSearchTool(security.NewRateLimiter(10))
	defer tool.rateLimit.Stop()
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "",
	})
	if err == nil {
		t.Error("expected error for empty query, got nil")
	}
}

func TestWebSearchTool_Execute_InvalidQueryType(t *testing.T) {
	tool := NewWebSearchTool(security.NewRateLimiter(10))
	defer tool.rateLimit.Stop()
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": 123,
	})
	if err == nil {
		t.Error("expected error for non-string query, got nil")
	}
}

func TestWebSearchTool_Execute_RateLimit(t *testing.T) {
	// Create a tool with rate limit of 1
	limiter := security.NewRateLimiter(1)
	defer limiter.Stop()
	tool := &WebSearchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimit: limiter,
		cache:     NewURLCache(60 * time.Second),
	}

	// Use up the single token
	_ = tool.rateLimit.Allow()

	// Second call should hit rate limit
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "test",
	})
	if err == nil {
		t.Error("expected rate limit error, got nil")
	}
}

func TestSearchResultJSON(t *testing.T) {
	// Verify SearchResult can be marshaled/unmarshaled correctly
	r := SearchResult{
		Title:   "Test Title",
		URL:     "https://example.com",
		Snippet: "Test snippet",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal SearchResult: %v", err)
	}

	var restored SearchResult
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal SearchResult: %v", err)
	}

	if restored.Title != r.Title {
		t.Errorf("title round-trip: expected %q, got %q", r.Title, restored.Title)
	}
	if restored.URL != r.URL {
		t.Errorf("URL round-trip: expected %q, got %q", r.URL, restored.URL)
	}
	if restored.Snippet != r.Snippet {
		t.Errorf("snippet round-trip: expected %q, got %q", r.Snippet, restored.Snippet)
	}
}

func TestDuckDuckGoHTML_MaxResults(t *testing.T) {
	// Generate HTML with more than 10 results to verify the limit
	var sb strings.Builder
	sb.WriteString("<html><body><div class=\"results\">")
	for i := 0; i < 15; i++ {
		sb.WriteString(fmt.Sprintf(`<div class="result">
			<div class="result__body">
				<h2 class="result__title">
					<a class="result__a" href="https://example%d.com">Result %d</a>
				</h2>
				<div class="result__snippet">Snippet %d</div>
			</div>
		</div>`, i, i, i))
	}
	sb.WriteString("</div></body></html>")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatalf("failed to parse HTML: %v", err)
	}

	results := parseDuckDuckGoHTML(doc)
	if len(results) > 10 {
		t.Errorf("expected at most 10 results, got %d", len(results))
	}
}

func TestWebSearchTool_CacheHit(t *testing.T) {
	tool := NewWebSearchTool(security.NewRateLimiter(10))
	defer tool.rateLimit.Stop()

	// Manually set cache entry
	expectedResult := map[string]interface{}{
		"results": []SearchResult{
			{Title: "Cached", URL: "https://cached.example.com", Snippet: "Cached result"},
		},
	}
	tool.cache.Set("search:test", expectedResult)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}

	results, ok := resultMap["results"].([]SearchResult)
	if !ok {
		t.Fatalf("expected []SearchResult, got %T", resultMap["results"])
	}

	if len(results) != 1 || results[0].Title != "Cached" {
		t.Errorf("expected cached result, got %+v", results)
	}
}

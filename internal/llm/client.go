package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// Model は利用可能なモデルを表す
type Model string

const (
	ModelGemma4_26B Model = "gemma-4-26b-a4b-it"
	ModelGemma4_31B Model = "gemma-4-31b-it"
	ModelGeminiLite Model = "gemini-lite"
)

// Client はGemini APIクライアント
type Client struct {
	apiKey     string
	model      Model
	fallbacks  []Model
	httpClient *http.Client
}

// NewClient は新しいLLMクライアントを作成する
func NewClient(model Model) (*Client, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	return &Client{
		apiKey: apiKey,
		model:  model,
		fallbacks: []Model{
			ModelGemma4_31B,
			ModelGemma4_26B,
			ModelGeminiLite,
		},
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// Request はLLMへのリクエスト
type Request struct {
	SystemPrompt string
	Messages     []Message
	Tools        []Tool
	MaxTokens    int
}

// Message はチャットメッセージ
type Message struct {
	Role             string                   `json:"role"`
	Content          string                   `json:"content,omitempty"`
	FunctionCall     *FunctionCallContent     `json:"function_call,omitempty"`
	FunctionResponse *FunctionResponseContent `json:"function_response,omitempty"`
}

// FunctionCallContent はモデルからの関数呼び出し内容
type FunctionCallContent struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// FunctionResponseContent は関数実行結果
type FunctionResponseContent struct {
	Name     string      `json:"name"`
	Response interface{} `json:"response"`
}

// Tool はfunction calling用のツール定義
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// Response はLLMからのレスポンス
type Response struct {
	Content   string
	ToolCalls []ToolCall
}

// ToolCall はモデルからのツール呼び出し
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
}

// geminiRequest はGemini APIリクエスト形式
type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	Tools             []geminiTool     `json:"tools,omitempty"`
	SystemInstruction *geminiSystemInst `json:"systemInstruction,omitempty"`
}

// geminiSystemInst はGemini APIのシステムインストラクション形式
type geminiSystemInst struct {
	Parts []geminiPart `json:"parts"`
}

// geminiContent はGemini APIのコンテンツ形式
type geminiContent struct {
	Role  string         `json:"role"`
	Parts []geminiPart   `json:"parts"`
}

// geminiPart はGemini APIのパート形式
type geminiPart struct {
	Text             string                   `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall      `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse  `json:"functionResponse,omitempty"`
}

// geminiFunctionCall はGemini APIの関数呼び出し形式
type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// geminiFunctionResponse はGemini APIの関数実行結果形式
type geminiFunctionResponse struct {
	Name     string      `json:"name"`
	Response interface{} `json:"response"`
}

// geminiTool はGemini APIのツール形式
type geminiTool struct {
	FunctionDeclarations []geminiFunction `json:"functionDeclarations"`
}

// geminiFunction はGemini APIの関数形式
type geminiFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// geminiResponse はGemini APIレスポンス形式
type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

// geminiCandidate はGemini APIのキャンドレート形式
type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

// Generate は応答を生成する（フォールバック付き）
func (c *Client) Generate(ctx context.Context, req *Request) (*Response, error) {
	// プライマリモデルで試行
	resp, err := c.generateWithModel(ctx, c.model, req)
	if err == nil {
		return resp, nil
	}

	// 429エラーの場合、フォールバックモデルを試行
	if isRateLimitError(err) {
		fmt.Printf("Rate limit hit for model %s, trying fallbacks\n", c.model)
		
		for _, fallbackModel := range c.fallbacks {
			if fallbackModel == c.model {
				continue
			}

			// 指数バックオフ
			time.Sleep(1 * time.Second)

			resp, err := c.generateWithModel(ctx, fallbackModel, req)
			if err == nil {
				return resp, nil
			}

			if !isRateLimitError(err) {
				return nil, err
			}
		}

		return nil, fmt.Errorf("all models rate limited")
	}

	return nil, err
}

// generateWithModel は指定モデルで応答を生成する
func (c *Client) generateWithModel(ctx context.Context, model Model, req *Request) (*Response, error) {
	// Gemini APIリクエストを構築
	geminiReq := c.buildGeminiRequest(req)

	// リクエストボディをJSONに変換
	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// エンドポイントURLを構築
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model,
		c.apiKey,
	)

	// リクエストを作成
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// リクエストを送信
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// レスポンスボディを読み取り
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 429エラーの場合
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{
			RetryAfter: parseRetryAfter(resp.Header),
		}
	}

	// その他のエラー
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// レスポンスをパース
	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// レスポンスを変換
	return c.convertResponse(&geminiResp), nil
}

// buildGeminiRequest はGemini APIリクエストを構築する
func (c *Client) buildGeminiRequest(req *Request) *geminiRequest {
	geminiReq := &geminiRequest{
		Contents: make([]geminiContent, 0, len(req.Messages)),
	}

	// メッセージを変換
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		if msg.Role == "function" {
			// Gemini API uses "function" role for function responses
			role = "function"
		}

		// Build parts from all non-nil message fields
		var parts []geminiPart
		if msg.Content != "" {
			parts = append(parts, geminiPart{Text: msg.Content})
		}
		if msg.FunctionCall != nil {
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{
					Name: msg.FunctionCall.Name,
					Args: msg.FunctionCall.Args,
				},
			})
		}
		if msg.FunctionResponse != nil {
			parts = append(parts, geminiPart{
				FunctionResponse: &geminiFunctionResponse{
					Name:     msg.FunctionResponse.Name,
					Response: msg.FunctionResponse.Response,
				},
			})
		}

		// Fallback: ensure at least one (empty) part
		if len(parts) == 0 {
			parts = []geminiPart{{Text: ""}}
		}

		geminiReq.Contents = append(geminiReq.Contents, geminiContent{
			Role:  role,
			Parts: parts,
		})
	}

	// システムプロンプトがある場合
	if req.SystemPrompt != "" {
		geminiReq.SystemInstruction = &geminiSystemInst{
			Parts: []geminiPart{{Text: req.SystemPrompt}},
		}
	}

	// ツールがある場合
	if len(req.Tools) > 0 {
		tools := geminiTool{
			FunctionDeclarations: make([]geminiFunction, 0, len(req.Tools)),
		}

		for _, tool := range req.Tools {
			tools.FunctionDeclarations = append(tools.FunctionDeclarations, geminiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			})
		}

		geminiReq.Tools = []geminiTool{tools}
	}

	return geminiReq
}

// convertResponse はGemini APIレスポンスを変換する
func (c *Client) convertResponse(geminiResp *geminiResponse) *Response {
	if len(geminiResp.Candidates) == 0 {
		return &Response{}
	}

	candidate := geminiResp.Candidates[0]
	resp := &Response{}

	// 全パートを走査してテキストと関数呼び出しを抽出
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			resp.Content = part.Text
		}
		if part.FunctionCall != nil {
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:        generateToolCallID(),
				Name:      part.FunctionCall.Name,
				Arguments: part.FunctionCall.Args,
			})
		}
	}

	return resp
}

// toolCallCounter はツール呼び出しID生成用のアトミックカウンター
var toolCallCounter uint64

// generateToolCallID は一意なツール呼び出しIDを生成する
func generateToolCallID() string {
	n := atomic.AddUint64(&toolCallCounter, 1)
	return fmt.Sprintf("tool_%d", n)
}

// parseRetryAfter はRetry-Afterヘッダをパースする
func parseRetryAfter(header http.Header) time.Duration {
	retryAfter := header.Get("Retry-After")
	if retryAfter == "" {
		return 1 * time.Second // デフォルト
	}

	// 秒数としてパース
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		return time.Duration(seconds) * time.Second
	}

	return 1 * time.Second
}

// isRateLimitError は429エラーか確認する
func isRateLimitError(err error) bool {
	_, ok := err.(*RateLimitError)
	return ok
}

// RateLimitError は429エラー
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited, retry after %v", e.RetryAfter)
}

package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// loadEnvFile は .env ファイルを読み込んで os.Setenv する簡易ローダー
func loadEnvFile(t *testing.T) {
	t.Helper()

	// プロジェクトルートを探す (go.mod があるディレクトリ)
	// テストファイルからの相対パス: internal/llm/ -> ../../
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// 上に辿って go.mod を探す
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}

	envPath := filepath.Join(dir, ".env")
	f, err := os.Open(envPath)
	if err != nil {
		t.Logf("Warning: could not open .env file at %s: %v", envPath, err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 空行・コメントをスキップ
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// KEY=VALUE 形式をパース（最初の = で分割）
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		// 値が空ならスキップ
		if value == "" {
			continue
		}
		// 既に環境変数がセットされている場合は上書きしない
		if os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, value)
	}
}

// resetToolCallCounter はテスト間でツール呼び出しカウンターをリセットする
func resetToolCallCounter() {
	atomic.StoreUint64(&toolCallCounter, 0)
}

// newTestClient はテスト用の最小限のClientを作成する（APIキー不要）
func newTestClient() *Client {
	return &Client{}
}

// --- convertResponse tests ---

func TestConvertResponse_TextOnly(t *testing.T) {
	resetToolCallCounter()
	client := newTestClient()

	geminiResp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Parts: []geminiPart{
						{Text: "Hello, world!"},
					},
				},
			},
		},
	}

	resp := client.convertResponse(geminiResp)

	if resp.Content != "Hello, world!" {
		t.Errorf("expected Content %q, got %q", "Hello, world!", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected 0 ToolCalls, got %d", len(resp.ToolCalls))
	}
}

func TestConvertResponse_FunctionCall(t *testing.T) {
	resetToolCallCounter()
	client := newTestClient()

	geminiResp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Parts: []geminiPart{
						{
							FunctionCall: &geminiFunctionCall{
								Name: "get_weather",
								Args: map[string]interface{}{"location": "Tokyo"},
							},
						},
					},
				},
			},
		},
	}

	resp := client.convertResponse(geminiResp)

	if resp.Content != "" {
		t.Errorf("expected empty Content, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 ToolCall, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected ToolCall name %q, got %q", "get_weather", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Arguments["location"] != "Tokyo" {
		t.Errorf("expected Arguments[location]=%q, got %q", "Tokyo", resp.ToolCalls[0].Arguments["location"])
	}
	if resp.ToolCalls[0].ID == "" {
		t.Error("expected non-empty ToolCall ID")
	}
}

func TestConvertResponse_TextAndFunctionCall(t *testing.T) {
	resetToolCallCounter()
	client := newTestClient()

	geminiResp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Parts: []geminiPart{
						{Text: "Let me check the weather for you."},
						{
							FunctionCall: &geminiFunctionCall{
								Name: "get_weather",
								Args: map[string]interface{}{"location": "Tokyo"},
							},
						},
					},
				},
			},
		},
	}

	resp := client.convertResponse(geminiResp)

	if resp.Content != "Let me check the weather for you." {
		t.Errorf("expected Content %q, got %q", "Let me check the weather for you.", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 ToolCall, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected ToolCall name %q, got %q", "get_weather", resp.ToolCalls[0].Name)
	}
}

func TestConvertResponse_NoCandidates(t *testing.T) {
	resetToolCallCounter()
	client := newTestClient()

	geminiResp := &geminiResponse{
		Candidates: []geminiCandidate{},
	}

	resp := client.convertResponse(geminiResp)

	if resp.Content != "" {
		t.Errorf("expected empty Content, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected 0 ToolCalls, got %d", len(resp.ToolCalls))
	}
}

func TestConvertResponse_MultipleFunctionCalls(t *testing.T) {
	resetToolCallCounter()
	client := newTestClient()

	geminiResp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Parts: []geminiPart{
						{
							FunctionCall: &geminiFunctionCall{
								Name: "get_weather",
								Args: map[string]interface{}{"location": "Tokyo"},
							},
						},
						{
							FunctionCall: &geminiFunctionCall{
								Name: "get_time",
								Args: map[string]interface{}{"timezone": "Asia/Tokyo"},
							},
						},
					},
				},
			},
		},
	}

	resp := client.convertResponse(geminiResp)

	if resp.Content != "" {
		t.Errorf("expected empty Content, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 ToolCalls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected ToolCall[0].Name %q, got %q", "get_weather", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[1].Name != "get_time" {
		t.Errorf("expected ToolCall[1].Name %q, got %q", "get_time", resp.ToolCalls[1].Name)
	}
	// IDs should be unique
	if resp.ToolCalls[0].ID == resp.ToolCalls[1].ID {
		t.Error("expected unique ToolCall IDs, but both are the same")
	}
}

// --- Message omitempty test ---

func TestMessage_Omitempty(t *testing.T) {
	// Message with only Content should NOT marshal function_call or function_response fields
	msg := Message{
		Role:    "user",
		Content: "hello",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)

	if !strings.Contains(jsonStr, `"content":"hello"`) {
		t.Errorf("expected JSON to contain content field, got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "function_call") {
		t.Errorf("expected JSON to NOT contain function_call field, got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "function_response") {
		t.Errorf("expected JSON to NOT contain function_response field, got: %s", jsonStr)
	}

	// Message with FunctionCall should include function_call
	msgWithFC := Message{
		Role: "assistant",
		FunctionCall: &FunctionCallContent{
			Name: "get_weather",
			Args: map[string]interface{}{"location": "Tokyo"},
		},
	}

	data, err = json.Marshal(msgWithFC)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr = string(data)
	if !strings.Contains(jsonStr, "function_call") {
		t.Errorf("expected JSON to contain function_call field, got: %s", jsonStr)
	}

	// Message with FunctionResponse should include function_response
	msgWithFR := Message{
		Role: "function",
		FunctionResponse: &FunctionResponseContent{
			Name:     "get_weather",
			Response: map[string]interface{}{"temperature": 25},
		},
	}

	data, err = json.Marshal(msgWithFR)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr = string(data)
	if !strings.Contains(jsonStr, "function_response") {
		t.Errorf("expected JSON to contain function_response field, got: %s", jsonStr)
	}
}

// --- buildGeminiRequest tests ---

func TestBuildGeminiRequest_FunctionCallMessage(t *testing.T) {
	client := newTestClient()

	req := &Request{
		Messages: []Message{
			{
				Role: "assistant",
				FunctionCall: &FunctionCallContent{
					Name: "get_weather",
					Args: map[string]interface{}{"location": "Tokyo"},
				},
			},
		},
	}

	geminiReq := client.buildGeminiRequest(req)

	if len(geminiReq.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(geminiReq.Contents))
	}

	content := geminiReq.Contents[0]
	if content.Role != "model" {
		t.Errorf("expected role %q, got %q", "model", content.Role)
	}

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}

	if content.Parts[0].FunctionCall == nil {
		t.Fatal("expected FunctionCall part, got nil")
	}
	if content.Parts[0].FunctionCall.Name != "get_weather" {
		t.Errorf("expected FunctionCall.Name %q, got %q", "get_weather", content.Parts[0].FunctionCall.Name)
	}
	if content.Parts[0].FunctionCall.Args["location"] != "Tokyo" {
		t.Errorf("expected FunctionCall.Args[location]=%q, got %q", "Tokyo", content.Parts[0].FunctionCall.Args["location"])
	}
}

func TestBuildGeminiRequest_FunctionResponseMessage(t *testing.T) {
	client := newTestClient()

	req := &Request{
		Messages: []Message{
			{
				Role: "function",
				FunctionResponse: &FunctionResponseContent{
					Name:     "get_weather",
					Response: map[string]interface{}{"temperature": 25},
				},
			},
		},
	}

	geminiReq := client.buildGeminiRequest(req)

	if len(geminiReq.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(geminiReq.Contents))
	}

	content := geminiReq.Contents[0]
	if content.Role != "function" {
		t.Errorf("expected role %q, got %q", "function", content.Role)
	}

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}

	if content.Parts[0].FunctionResponse == nil {
		t.Fatal("expected FunctionResponse part, got nil")
	}
	if content.Parts[0].FunctionResponse.Name != "get_weather" {
		t.Errorf("expected FunctionResponse.Name %q, got %q", "get_weather", content.Parts[0].FunctionResponse.Name)
	}
}

func TestBuildGeminiRequest_MixedMessages(t *testing.T) {
	client := newTestClient()

	req := &Request{
		Messages: []Message{
			{
				Role:    "user",
				Content: "What's the weather in Tokyo?",
			},
			{
				Role: "assistant",
				FunctionCall: &FunctionCallContent{
					Name: "get_weather",
					Args: map[string]interface{}{"location": "Tokyo"},
				},
			},
			{
				Role: "function",
				FunctionResponse: &FunctionResponseContent{
					Name:     "get_weather",
					Response: map[string]interface{}{"temperature": 25},
				},
			},
		},
	}

	geminiReq := client.buildGeminiRequest(req)

	if len(geminiReq.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(geminiReq.Contents))
	}

	// Message 0: user text
	c0 := geminiReq.Contents[0]
	if c0.Role != "user" {
		t.Errorf("expected content[0].role %q, got %q", "user", c0.Role)
	}
	if len(c0.Parts) != 1 {
		t.Fatalf("expected content[0] to have 1 part, got %d", len(c0.Parts))
	}
	if c0.Parts[0].Text != "What's the weather in Tokyo?" {
		t.Errorf("expected content[0].Parts[0].Text %q, got %q", "What's the weather in Tokyo?", c0.Parts[0].Text)
	}

	// Message 1: assistant function call
	c1 := geminiReq.Contents[1]
	if c1.Role != "model" {
		t.Errorf("expected content[1].role %q, got %q", "model", c1.Role)
	}
	if len(c1.Parts) != 1 {
		t.Fatalf("expected content[1] to have 1 part, got %d", len(c1.Parts))
	}
	if c1.Parts[0].FunctionCall == nil {
		t.Fatal("expected content[1].Parts[0].FunctionCall to be non-nil")
	}
	if c1.Parts[0].FunctionCall.Name != "get_weather" {
		t.Errorf("expected FunctionCall.Name %q, got %q", "get_weather", c1.Parts[0].FunctionCall.Name)
	}

	// Message 2: function response
	c2 := geminiReq.Contents[2]
	if c2.Role != "function" {
		t.Errorf("expected content[2].role %q, got %q", "function", c2.Role)
	}
	if len(c2.Parts) != 1 {
		t.Fatalf("expected content[2] to have 1 part, got %d", len(c2.Parts))
	}
	if c2.Parts[0].FunctionResponse == nil {
		t.Fatal("expected content[2].Parts[0].FunctionResponse to be non-nil")
	}
	if c2.Parts[0].FunctionResponse.Name != "get_weather" {
		t.Errorf("expected FunctionResponse.Name %q, got %q", "get_weather", c2.Parts[0].FunctionResponse.Name)
	}
}

func TestBuildGeminiRequest_SystemPrompt(t *testing.T) {
	client := newTestClient()

	req := &Request{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
	}

	geminiReq := client.buildGeminiRequest(req)

	if geminiReq.SystemInstruction == nil {
		t.Fatal("expected SystemInstruction to be non-nil")
	}
	if len(geminiReq.SystemInstruction.Parts) != 1 {
		t.Fatalf("expected 1 system instruction part, got %d", len(geminiReq.SystemInstruction.Parts))
	}
	if geminiReq.SystemInstruction.Parts[0].Text != "You are a helpful assistant." {
		t.Errorf("expected system instruction text %q, got %q", "You are a helpful assistant.", geminiReq.SystemInstruction.Parts[0].Text)
	}
}

func TestBuildGeminiRequest_Tools(t *testing.T) {
	client := newTestClient()

	req := &Request{
		Messages: []Message{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []Tool{
			{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
	}

	geminiReq := client.buildGeminiRequest(req)

	if len(geminiReq.Tools) != 1 {
		t.Fatalf("expected 1 tool entry, got %d", len(geminiReq.Tools))
	}
	if len(geminiReq.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(geminiReq.Tools[0].FunctionDeclarations))
	}

	fd := geminiReq.Tools[0].FunctionDeclarations[0]
	if fd.Name != "get_weather" {
		t.Errorf("expected function name %q, got %q", "get_weather", fd.Name)
	}
	if fd.Description != "Get the weather for a location" {
		t.Errorf("expected function description %q, got %q", "Get the weather for a location", fd.Description)
	}
}

// --- ToolCall ID generation test ---

func TestGenerateToolCallID(t *testing.T) {
	resetToolCallCounter()

	// Generate multiple IDs and verify pattern and uniqueness
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		ids[i] = generateToolCallID()
	}

	// Check pattern: tool_N where N is a positive integer
	for i, id := range ids {
		if !strings.HasPrefix(id, "tool_") {
			t.Errorf("ids[%d] = %q does not start with 'tool_'", i, id)
		}
		suffix := strings.TrimPrefix(id, "tool_")
		if suffix == "" {
			t.Errorf("ids[%d] = %q has empty suffix after 'tool_'", i, id)
		}
	}

	// Check uniqueness
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}

	// Verify sequential numbering starting from 1
	if ids[0] != "tool_1" {
		t.Errorf("expected first ID to be 'tool_1', got %q", ids[0])
	}
	if ids[4] != "tool_5" {
		t.Errorf("expected fifth ID to be 'tool_5', got %q", ids[4])
	}
}

// --- Integration test (existing) ---

func TestGenerate_Integration(t *testing.T) {
	// .env ファイルから環境変数を読み込む（既にセットされていれば上書きしない）
	loadEnvFile(t)

	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("Skipping integration test: GEMINI_API_KEY is not set")
	}

	client, err := NewClient(ModelGemma4_26B)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	req := &Request{
		Messages: []Message{
			{
				Role:    "user",
				Content: "Say hello in one sentence",
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}

	t.Logf("Response content: %s", resp.Content)

	if resp.Content == "" {
		t.Fatal("Generate() returned empty response content")
	}
}
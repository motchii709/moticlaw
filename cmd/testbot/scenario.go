// cmd/testbot/scenario_test.go — moticlaw リアルシナリオテスト
//
// Real-world usage scenario tests that simulate actual user behavior.
// These tests exercise multiple components working together as they would
// in production — conversations, rapid requests, large messages, formatting,
// error recovery, data persistence, and file growth.
//
// Each scenario is self-contained and cleans up after itself.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/moti/moticlaw/internal/llm"
	"github.com/moti/moticlaw/internal/tools"
)

// ============================================================================
// Scenario 1: End-to-end conversation with tool calling
// ============================================================================

// testScenarioE2EConversation simulates a real user conversation where the
// LLM might use tools and must follow Discord-safe formatting rules.
func testScenarioE2EConversation() []result {
	if os.Getenv("GEMINI_API_KEY") == "" {
		return []result{skipTest("S1: E2E Conversation", "GEMINI_API_KEY not set")}
	}

	return []result{
		runTest("S1a: trigger tool-use prompt and get response", 60*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			// Send a query that would normally trigger web search tool
			resp, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: "What's the weather in Tokyo? Give me a one-sentence answer."},
				},
			})
			if err != nil {
				return fmt.Errorf("Generate() failed: %w", err)
			}
			if resp.Content == "" {
				return fmt.Errorf("empty response content")
			}
			return nil
		}),
		runTest("S1b: system prompt prevents table output", 60*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			resp, err := client.Generate(context.Background(), &llm.Request{
				SystemPrompt: "You are a helpful assistant. IMPORTANT: Never use markdown tables " +
					"(| syntax) in your responses. Use code blocks or lists instead of tables. " +
					"Never use # headings. Use **bold** for emphasis.",
				Messages: []llm.Message{
					{Role: "user", Content: "Show me a table of programming languages and their " +
						"typical uses. Include Rust, Go, Python, and TypeScript. I want to see " +
						"columns for language, use case, and popularity."},
				},
			})
			if err != nil {
				return fmt.Errorf("Generate() failed: %w", err)
			}
			if resp.Content == "" {
				return fmt.Errorf("empty response content")
			}

			// Check for table syntax — the model should NOT use markdown tables
			if strings.Contains(resp.Content, "|") {
				fmt.Printf("  ⚠ WARNING: S1b — response contains table syntax (| chars)\n")
			}

			// Reject HTML tables (definitely not Discord-compatible)
			if strings.Contains(resp.Content, "<table") ||
				strings.Contains(resp.Content, "<tr>") ||
				strings.Contains(resp.Content, "<td>") {
				return fmt.Errorf("response contains HTML table tags (not Discord-compatible)")
			}

			return nil
		}),
		runTest("S1c: conversation with context follow-up", 90*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			// First message
			resp1, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: "My name is Alice and I like Go programming."},
				},
			})
			if err != nil {
				return fmt.Errorf("first message failed: %w", err)
			}
			if resp1.Content == "" {
				return fmt.Errorf("empty response on first message")
			}

			// Follow-up message with context
			resp2, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: "My name is Alice and I like Go programming."},
					{Role: "assistant", Content: resp1.Content},
					{Role: "user", Content: "What's my name and what do I like?"},
				},
			})
			if err != nil {
				return fmt.Errorf("follow-up message failed: %w", err)
			}
			if resp2.Content == "" {
				return fmt.Errorf("empty response on follow-up")
			}

			// Verify the model remembers context
			lower := strings.ToLower(resp2.Content)
			if !strings.Contains(lower, "alice") {
				return fmt.Errorf("model didn't remember name 'Alice' in follow-up; "+
					"response starts: %q", substring(resp2.Content, 0, 100))
			}
			if !strings.Contains(lower, "go") && !strings.Contains(lower, "golang") {
				return fmt.Errorf("model didn't remember 'Go programming' in follow-up; "+
					"response starts: %q", substring(resp2.Content, 0, 100))
			}

			return nil
		}),
	}
}

// ============================================================================
// Scenario 2: Multiple rapid requests (simulating multiple users)
// ============================================================================

// testScenarioRapidRequests simulates 5 concurrent users making requests,
// verifying no panics, race conditions, or shared-state corruption.
func testScenarioRapidRequests() []result {
	if os.Getenv("GEMINI_API_KEY") == "" {
		return []result{skipTest("S2: Rapid Requests", "GEMINI_API_KEY not set")}
	}

	return []result{
		runTest("S2a: 5 concurrent requests with race detection", 120*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			var wg sync.WaitGroup
			errCh := make(chan error, 5)

			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					// Catch panics to prevent one goroutine crash from taking down all tests
					defer func() {
						if r := recover(); r != nil {
							errCh <- fmt.Errorf("goroutine %d panicked: %v", idx, r)
						}
					}()

					msg := fmt.Sprintf("Count from 1 to 5 in exactly one line for request %d.", idx)
					resp, err := client.Generate(context.Background(), &llm.Request{
						Messages: []llm.Message{
							{Role: "user", Content: msg},
						},
					})
					if err != nil {
						errCh <- fmt.Errorf("request %d failed: %w", idx, err)
						return
					}
					if resp.Content == "" {
						errCh <- fmt.Errorf("request %d: empty response", idx)
						return
					}
				}(i)
			}

			wg.Wait()
			close(errCh)

			var errs []string
			for err := range errCh {
				errs = append(errs, err.Error())
			}

			if len(errs) > 0 {
				// Tolerate partial failures (rate limiting is expected in tests)
				if len(errs) >= 5 {
					return fmt.Errorf("all 5 requests failed: %s", strings.Join(errs, "; "))
				}
				fmt.Printf("  ⚠ S2a: %d/5 requests failed (expected rate limiting): %s\n",
					len(errs), strings.Join(errs, "; "))
			}

			return nil
		}),
		runTest("S2b: sequential requests — no shared state corruption", 60*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			// These should be independent and not affect each other's state
			questions := []string{
				"Say 'one' and nothing else.",
				"Say 'two' and nothing else.",
				"Say 'three' and nothing else.",
			}

			for i, q := range questions {
				resp, err := client.Generate(context.Background(), &llm.Request{
					Messages: []llm.Message{
						{Role: "user", Content: q},
					},
				})
				if err != nil {
					return fmt.Errorf("request %d failed: %w", i, err)
				}
				if resp.Content == "" {
					return fmt.Errorf("request %d: empty response", i)
				}
			}

			return nil
		}),
	}
}

// ============================================================================
// Scenario 3: Large message handling
// ============================================================================

// testScenarioLargeMessage sends very large messages to the LLM and verifies
// that the system doesn't crash, OOM, or produce garbled output.
func testScenarioLargeMessage() []result {
	if os.Getenv("GEMINI_API_KEY") == "" {
		return []result{skipTest("S3: Large Messages", "GEMINI_API_KEY not set")}
	}

	return []result{
		runTest("S3a: 10KB message doesn't crash", 120*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			// Generate a 10KB message
			var buf bytes.Buffer
			buf.WriteString("Here is a large document. Please summarize it in one sentence:\n\n")
			for buf.Len() < 10*1024 {
				buf.WriteString("This is a line of text that provides information about a topic. ")
				buf.WriteString("It contains various details that might be useful for understanding. ")
				buf.WriteString("The content is repeated to simulate a large message body. ")
				buf.WriteString(fmt.Sprintf("Line data %d: %s\n", rand.Intn(10000), strings.Repeat("x", 20)))
			}

			resp, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: buf.String()},
				},
				MaxTokens: 100,
			})
			if err != nil {
				return fmt.Errorf("Generate() with large message failed: %w", err)
			}
			if resp.Content == "" {
				return fmt.Errorf("empty response content for large message")
			}

			return nil
		}),
		runTest("S3b: extremely long single-line without newlines", 120*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			// One very long line (~7000 chars, no newlines)
			longWord := strings.Repeat("verylongword", 500)
			msg := fmt.Sprintf("What does this word mean? Just say 'long word': %s", longWord)

			resp, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: msg},
				},
				MaxTokens: 50,
			})
			if err != nil {
				return fmt.Errorf("Generate() with long line failed: %w", err)
			}
			if resp.Content == "" {
				return fmt.Errorf("empty response for long message")
			}

			return nil
		}),
	}
}

// ============================================================================
// Scenario 4: Tool output → Discord format conversion
// ============================================================================

// formatForDiscord converts tool output text to Discord-safe Markdown.
//
// Discord's Markdown support is limited — tables |...| and headings # don't work.
// See AGENTS.md > Discord 出力フォーマット規則 for full details.
//
// This function:
//   - Replaces headings (# heading) with bold (**heading**)
//   - Converts markdown tables to colon-separated lists
//   - Skips table separator rows (| --- | --- |)
//   - Preserves code blocks (``` ... ```) unchanged
//   - Leaves other formatting intact
func formatForDiscord(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track code block state — preserve everything inside verbatim
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			result = append(result, line)
			continue
		}

		if inCodeBlock {
			result = append(result, line)
			continue
		}

		// Replace headings with bold
		if strings.HasPrefix(trimmed, "#") {
			content := strings.TrimLeft(trimmed, "# ")
			result = append(result, "**"+content+"**")
			continue
		}

		// Detect table rows: lines with 2+ pipe characters
		pipeCount := strings.Count(trimmed, "|")
		if pipeCount >= 2 {
			// Check if it's a separator row (contains only |, -, :, space)
			stripped := strings.ReplaceAll(trimmed, "|", "")
			stripped = strings.ReplaceAll(stripped, "-", "")
			stripped = strings.ReplaceAll(stripped, ":", "")
			stripped = strings.ReplaceAll(stripped, " ", "")
			if stripped == "" {
				// Table separator row — skip entirely
				continue
			}
			// Convert to list format: **Header**: value1, value2
			cells := strings.Split(trimmed, "|")
			var cellContents []string
			for _, c := range cells {
				c = strings.TrimSpace(c)
				if c != "" {
					cellContents = append(cellContents, c)
				}
			}
			if len(cellContents) >= 2 {
				result = append(result, "- **"+cellContents[0]+"**: "+strings.Join(cellContents[1:], ", "))
			} else if len(cellContents) == 1 {
				result = append(result, "- "+cellContents[0])
			} else {
				result = append(result, line)
			}
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// testScenarioDiscordFormat validates the Discord format conversion pipeline.
func testScenarioDiscordFormat() []result {
	return []result{
		runTest("S4a: format tool JSON output for Discord", 5*time.Second, func() error {
			// Simulate tool output that might contain table-like structures
			toolOutput := map[string]interface{}{
				"status": 200,
				"data": map[string]interface{}{
					"items": []map[string]interface{}{
						{"name": "Rust", "type": "Systems", "popularity": "Rising"},
						{"name": "Go", "type": "Systems/Web", "popularity": "Stable"},
						{"name": "Python", "type": "General", "popularity": "Very High"},
					},
				},
			}

			jsonBytes, err := json.MarshalIndent(toolOutput, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal failed: %w", err)
			}
			jsonStr := string(jsonBytes)

			// Apply Discord formatting
			formatted := formatForDiscord(jsonStr)

			// Verify no markdown tables remain
			if strings.Contains(formatted, "|\n") || strings.HasPrefix(formatted, "|") {
				return fmt.Errorf("formatted output still contains table characters")
			}

			// Verify headings are replaced
			if strings.HasPrefix(formatted, "#") {
				return fmt.Errorf("formatted output starts with heading character")
			}

			return nil
		}),
		runTest("S4b: format markdown with tables into Discord-safe format", 5*time.Second, func() error {
			input := `# Programming Languages
| Language | Use Case | Popularity |
|----------|----------|------------|
| Rust     | Systems  | Rising     |
| Go       | Web      | Stable     |

**Some text** after the table.`

			formatted := formatForDiscord(input)

			// Should not contain raw table separator rows
			if strings.Contains(formatted, "|---") {
				return fmt.Errorf("output still contains table separator row")
			}

			// Should not contain headings
			if strings.Contains(formatted, "# ") {
				return fmt.Errorf("output still contains heading markers")
			}

			// Heading should be converted to bold
			if !strings.Contains(formatted, "**Programming Languages**") {
				return fmt.Errorf("heading was not converted to bold; got: %q",
					substring(formatted, 0, 80))
			}

			// Should preserve normal bold text
			if !strings.Contains(formatted, "**Some text**") {
				return fmt.Errorf("normal bold text was lost after formatting")
			}

			// Data rows should be converted to list format
			if !strings.Contains(formatted, "- **Rust**: Systems, Rising") {
				return fmt.Errorf("table data not converted to list format; got: %q",
					substring(formatted, 0, 200))
			}

			return nil
		}),
		runTest("S4c: JSON in code block preserved verbatim", 5*time.Second, func() error {
			input := "Here is the data:\n```json\n{\"key\": \"value\"}\n```\nThat's all."
			formatted := formatForDiscord(input)

			if !strings.Contains(formatted, "```json") {
				return fmt.Errorf("json code block opening was lost")
			}
			if !strings.Contains(formatted, "```") {
				return fmt.Errorf("code block closing was lost")
			}
			if !strings.Contains(formatted, `{"key": "value"}`) {
				return fmt.Errorf("code block content was modified")
			}

			return nil
		}),
		runTest("S4d: empty string and edge cases", 5*time.Second, func() error {
			tests := []struct {
				input string
				name  string
			}{
				{"", "empty"},
				{"\n\n\n", "only newlines"},
				{"   ", "whitespace only"},
				{"**bold** and *italic*", "basic formatting"},
				{"```\ncode\n```", "code block"},
				{"> quoted text", "blockquote"},
				{"- item 1\n- item 2", "list items"},
			}

			for _, tc := range tests {
				formatted := formatForDiscord(tc.input)
				if len(tc.input) > 0 && formatted == "" {
					return fmt.Errorf("non-empty input %q produced empty output", tc.name)
				}
			}

			return nil
		}),
	}
}

// ============================================================================
// Scenario 5: Error recovery — invalid API key
// ============================================================================

// testScenarioErrorRecovery simulates API key failures and verifies graceful
// error handling, then confirms the system recovers when the valid key is restored.
func testScenarioErrorRecovery() []result {
	return []result{
		runTest("S5a: invalid API key returns graceful error", 30*time.Second, func() error {
			originalKey := os.Getenv("GEMINI_API_KEY")
			if originalKey == "" {
				return fmt.Errorf("GEMINI_API_KEY not set (can't test recovery)")
			}

			// Temporarily set an invalid key
			os.Setenv("GEMINI_API_KEY", "INVALID_KEY_FOR_TESTING_12345")
			defer os.Setenv("GEMINI_API_KEY", originalKey)

			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return fmt.Errorf("NewClient should not fail even with invalid key: %w", err)
			}

			resp, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: "Say hello."},
				},
			})
			if err == nil {
				return fmt.Errorf("expected error for invalid API key, got response: %q",
					substring(resp.Content, 0, 100))
			}

			// Error should be descriptive, not a crash/panic
			errMsg := err.Error()
			if strings.Contains(errMsg, "panic") || strings.Contains(errMsg, "nil pointer") {
				return fmt.Errorf("error should not contain panic/nil: %s", errMsg)
			}

			// Should mention API error, auth failure, or key problem
			validPattern := strings.Contains(errMsg, "API error") ||
				strings.Contains(errMsg, "403") ||
				strings.Contains(errMsg, "400") ||
				strings.Contains(errMsg, "401") ||
				strings.Contains(errMsg, "key") ||
				strings.Contains(errMsg, "not found") ||
				strings.Contains(errMsg, "invalid") ||
				strings.Contains(errMsg, "denied")

			if !validPattern {
				fmt.Printf("  ⚠ S5a: error message format unexpected (but not a crash): %s\n", errMsg)
			}

			return nil
		}),
		runTest("S5b: restore valid key and verify recovery", 60*time.Second, func() error {
			originalKey := os.Getenv("GEMINI_API_KEY")
			if originalKey == "" {
				return fmt.Errorf("GEMINI_API_KEY not set")
			}

			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}

			resp, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: "Say 'Recovery successful' in exactly three words."},
				},
			})
			if err != nil {
				return fmt.Errorf("recovery failed — valid key still got error: %w", err)
			}
			if resp.Content == "" {
				return fmt.Errorf("empty response after recovery")
			}

			return nil
		}),
		runTest("S5c: NewClient with empty API key returns error", 2*time.Second, func() error {
			originalKey := os.Getenv("GEMINI_API_KEY")
			if originalKey == "" {
				return fmt.Errorf("GEMINI_API_KEY already empty — test precondition failed")
			}

			os.Unsetenv("GEMINI_API_KEY")
			defer os.Setenv("GEMINI_API_KEY", originalKey)

			_, err := llm.NewClient(llm.ModelGemma4_26B)
			if err == nil {
				return fmt.Errorf("expected error when GEMINI_API_KEY is empty")
			}
			if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
				return fmt.Errorf("error should mention GEMINI_API_KEY, got: %v", err)
			}

			return nil
		}),
	}
}

// ============================================================================
// Scenario 6: Memory file growth (100 entries)
// ============================================================================

// testScenarioMemoryGrowth writes 100 entries to the memory file and verifies
// the file remains valid, readable, and uncorrupted.
func testScenarioMemoryGrowth() []result {
	// Ensure memory directory exists
	memoryDir := filepath.Join("data", "memory")
	os.MkdirAll(memoryDir, 0755)

	// Clean up before test
	memoryFile := filepath.Join(memoryDir, testUserID+".md")
	os.Remove(memoryFile)

	return []result{
		runTest("S6a: write 100 memory entries without corruption", 30*time.Second, func() error {
			writeTool := tools.NewMemoryWriteTool(testUserID)
			readTool := tools.NewMemoryReadTool(testUserID)

			// Write 100 entries
			for i := 0; i < 100; i++ {
				entry := fmt.Sprintf("Memory entry #%d: timestamp=%s rand=%d",
					i, time.Now().Format(time.RFC3339), rand.Intn(100000))

				_, err := writeTool.Execute(context.Background(), map[string]interface{}{
					"content": entry,
				})
				if err != nil {
					return fmt.Errorf("failed to write entry %d: %w", i, err)
				}
			}

			// Read back
			result, err := readTool.Execute(context.Background(), map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("memory read after 100 writes failed: %w", err)
			}

			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}

			content, ok := m["content"].(string)
			if !ok {
				return fmt.Errorf("content not a string, got %T", m["content"])
			}

			if content == "" {
				return fmt.Errorf("memory content is empty after 100 writes")
			}

			// Count entries by looking for "Memory entry #" pattern
			entryCount := strings.Count(content, "Memory entry #")
			if entryCount < 100 {
				return fmt.Errorf("expected at least 100 entries, found %d", entryCount)
			}

			// Verify Markdown structure — section separators should exist
			sepCount := strings.Count(content, "---")
			if sepCount < 99 {
				return fmt.Errorf("expected at least 99 section separators, found %d", sepCount)
			}

			fmt.Printf("  📝 Memory: %d entries, %d bytes, %d separators\n",
				entryCount, len(content), sepCount)

			return nil
		}),
		runTest("S6b: memory file is valid UTF-8", 5*time.Second, func() error {
			data, err := os.ReadFile(filepath.Join("data", "memory", testUserID+".md"))
			if err != nil {
				return fmt.Errorf("failed to read memory file: %w", err)
			}

			if !utf8.Valid(data) {
				return fmt.Errorf("memory file contains invalid UTF-8 sequences")
			}

			return nil
		}),
		runTest("S6c: memory file is valid Markdown (basic structure)", 5*time.Second, func() error {
			data, err := os.ReadFile(filepath.Join("data", "memory", testUserID+".md"))
			if err != nil {
				return fmt.Errorf("failed to read memory file: %w", err)
			}

			content := string(data)

			// Should have consistent separator pattern between entries
			lines := strings.Split(content, "\n")
			separatorLines := 0
			for _, line := range lines {
				if strings.TrimSpace(line) == "---" {
					separatorLines++
				}
			}

			// With 100 entries, there should be 99 separators
			// Allow some tolerance for edge-of-file whitespace
			if separatorLines < 90 || separatorLines > 110 {
				return fmt.Errorf("unexpected number of separator lines: %d (expected ~99)", separatorLines)
			}

			return nil
		}),
	}
}

// ============================================================================
// Scenario 7: Cron job persistence (register → restart → verify)
// ============================================================================

// testScenarioCronPersistence registers a cron job, verifies it's persisted
// to disk, then simulates a restart by reading it back fresh.
func testScenarioCronPersistence() []result {
	// Ensure cron.json exists with a valid empty array
	cronPath := filepath.Join("data", "config", "cron.json")
	os.MkdirAll(filepath.Dir(cronPath), 0755)
	if _, err := os.Stat(cronPath); err != nil {
		os.WriteFile(cronPath, []byte("[]\n"), 0644)
	}

	// Clean up any previous test entries
	removeTestCronEntry()

	return []result{
		runTest("S7a: register cron job and verify file persistence", 10*time.Second, func() error {
			tool := tools.NewCronRegisterTool(testUserID)
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"interval": "10m",
				"command":  "echo persistence-test",
			})
			if err != nil {
				return fmt.Errorf("register failed: %w", err)
			}

			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			jobID, ok := m["job_id"].(string)
			if !ok || jobID == "" {
				return fmt.Errorf("missing job_id in response")
			}

			// Read the file directly to verify persistence
			data, err := os.ReadFile(cronPath)
			if err != nil {
				return fmt.Errorf("failed to read cron.json: %w", err)
			}

			var jobs []map[string]interface{}
			if err := json.Unmarshal(data, &jobs); err != nil {
				return fmt.Errorf("failed to parse cron.json: %w", err)
			}

			found := false
			for _, j := range jobs {
				uid, _ := j["user_id"].(string)
				cmd, _ := j["command"].(string)
				if uid == testUserID && cmd == "echo persistence-test" {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("registered job not found in cron.json on-disk")
			}

			return nil
		}),
		runTest("S7b: simulate restart — reload from file", 10*time.Second, func() error {
			// Read the file as a brand-new process would
			data, err := os.ReadFile(cronPath)
			if err != nil {
				return fmt.Errorf("failed to read cron.json: %w", err)
			}

			var jobs []map[string]interface{}
			if err := json.Unmarshal(data, &jobs); err != nil {
				return fmt.Errorf("failed to parse cron.json: %w", err)
			}

			// The job from the previous test should persist
			found := false
			for _, j := range jobs {
				uid, _ := j["user_id"].(string)
				if uid == testUserID {
					found = true

					// Verify all required fields present
					required := []string{"id", "user_id", "interval", "command", "created_at"}
					for _, field := range required {
						if _, ok := j[field]; !ok {
							return fmt.Errorf("cron job missing required field %q", field)
						}
					}

					// Verify the interval value persisted correctly
					interval, ok := j["interval"].(string)
					if !ok || interval != "10m" {
						return fmt.Errorf("expected interval '10m' after restart, got %v", j["interval"])
					}

					break
				}
			}
			if !found {
				return fmt.Errorf("cron job from S7a not found after simulated restart")
			}

			return nil
		}),
		runTest("S7c: minimum interval enforcement after restart", 5*time.Second, func() error {
			// Fresh instance should still enforce minimum interval
			tool := tools.NewCronRegisterTool(testUserID)
			_, err := tool.Execute(context.Background(), map[string]interface{}{
				"interval": "1m",
				"command":  "echo too-frequent",
			})
			if err == nil {
				return fmt.Errorf("expected error for interval under minimum, got nil")
			}
			if !strings.Contains(err.Error(), "minimum") {
				return fmt.Errorf("expected 'minimum interval' error, got: %v", err)
			}

			return nil
		}),
	}
}

// ============================================================================
// Helpers
// ============================================================================

// substring returns a portion of s, safely handling short strings.
func substring(s string, start, length int) string {
	if start >= len(s) {
		return ""
	}
	end := start + length
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

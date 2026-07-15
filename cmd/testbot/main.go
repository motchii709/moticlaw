// cmd/testbot — moticlaw 統合テストハーネス
//
// This program tests moticlaw's core functionality WITHOUT Discord.
// It exercises all internal packages: llm, tools, sandbox, security, store.
//
// Run with: go run ./cmd/testbot/
// (from the project root where .env and data/ live)

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/moti/moticlaw/internal/llm"
	"github.com/moti/moticlaw/internal/security"
	"github.com/moti/moticlaw/internal/tools"
)

const (
	testUserID = "testbot-integration"
	workDir    = "data/workdir"
)

// sandboxQueue is the global semaphore for serializing sandbox execution.
var sandboxQueue = make(chan struct{}, 1)

// Shared rate limiters for web tools (created once, shared across all tests).
var (
	webSearchLimiter = security.NewRateLimiter(10)
	webFetchLimiter  = security.NewRateLimiter(10)
)

// result tracks a single test outcome.
type result struct {
	name   string
	status string // PASS, FAIL, SKIP
	err    string
	dur    time.Duration
}

func (r result) String() string {
	icon := map[string]string{
		"PASS": "✓",
		"FAIL": "✗",
		"SKIP": "–",
	}[r.status]
	msg := fmt.Sprintf("  %s %s  [%s]", icon, r.name, r.dur.Round(time.Millisecond))
	if r.err != "" {
		msg += "\n        " + r.err
	}
	return msg
}

// testGroup is a group of related tests.
type testGroup struct {
	name  string
	tests []result
}

var (
	passed int
	failed int
	skipped int
	allResults []testGroup
)

// runTest executes a test function with a timeout and captures the result.
func runTest(name string, timeout time.Duration, fn func() error) result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	var err error

	start := time.Now()
	go func() {
		err = fn()
		close(done)
	}()

	select {
	case <-done:
		dur := time.Since(start)
		if err != nil {
			failed++
			return result{name: name, status: "FAIL", err: err.Error(), dur: dur}
		}
		passed++
		return result{name: name, status: "PASS", dur: dur}
	case <-ctx.Done():
		dur := time.Since(start)
		failed++
		return result{name: name, status: "FAIL", err: fmt.Sprintf("timeout after %v", timeout), dur: dur}
	}
}

// skipTest returns a SKIP result without executing anything.
func skipTest(name, reason string) result {
	skipped++
	return result{name: name, status: "SKIP", err: reason, dur: 0}
}

// group adds a group of tests to the global results.
func group(name string, tests []result) {
	allResults = append(allResults, testGroup{name: name, tests: tests})
}

// ============================================================================
// Environment
// ============================================================================

// loadEnv parses the .env file manually (KEY=VALUE lines, # comments, empty lines skipped).
func loadEnv() error {
	envPath := filepath.Join(".env")
	f, err := os.Open(envPath)
	if err != nil {
		return fmt.Errorf("cannot open .env: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if value == "" {
			continue
		}
		// Don't override already-set env vars
		if os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, value)
	}
	return scanner.Err()
}

// envSummary returns a human-readable summary of relevant env vars.
func envSummary() string {
	var lines []string
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		lines = append(lines, "GEMINI_API_KEY: set ("+maskKey(v)+")")
	} else {
		lines = append(lines, "GEMINI_API_KEY: NOT SET")
	}
	if v := os.Getenv("DISCORD_BOT_TOKEN"); v != "" {
		lines = append(lines, "DISCORD_BOT_TOKEN: set ("+maskKey(v)+")")
	} else {
		lines = append(lines, "DISCORD_BOT_TOKEN: not set")
	}
	if v := os.Getenv("GITHUB_PAT"); v != "" {
		lines = append(lines, "GITHUB_PAT: set ("+maskKey(v)+")")
	} else {
		lines = append(lines, "GITHUB_PAT: not set")
	}
	return strings.Join(lines, "\n         ")
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return strings.Repeat("*", len(k))
	}
	return k[:4] + strings.Repeat("*", len(k)-8) + k[len(k)-4:]
}

// cleanupTestData removes files/directories created during testing.
func cleanupTestData() {
	// Memory file
	os.Remove(filepath.Join("data", "memory", testUserID+".md"))
	// Workdir files
	os.RemoveAll(filepath.Join(workDir, testUserID))
	// Remove any test cron entry
	removeTestCronEntry()
}

func removeTestCronEntry() {
	cronPath := filepath.Join("data", "config", "cron.json")
	data, err := os.ReadFile(cronPath)
	if err != nil {
		return
	}
	var jobs []map[string]interface{}
	if err := json.Unmarshal(data, &jobs); err != nil {
		return
	}
	kept := make([]map[string]interface{}, 0)
	for _, j := range jobs {
		if uid, ok := j["user_id"].(string); ok && uid == testUserID {
			continue
		}
		kept = append(kept, j)
	}
	out, _ := json.MarshalIndent(kept, "", "  ")
	os.WriteFile(cronPath+".tmp", out, 0644)
	os.Rename(cronPath+".tmp", cronPath)
}

// ============================================================================
// Tests
// ============================================================================

// testLLMClient sends a simple message to Gemini API and verifies a response.
func testLLMClient() []result {
	if os.Getenv("GEMINI_API_KEY") == "" {
		return []result{skipTest("LLM Client", "GEMINI_API_KEY not set")}
	}

	results := []result{
		runTest("LLM: simple response generation", 60*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}
			resp, err := client.Generate(context.Background(), &llm.Request{
				Messages: []llm.Message{
					{Role: "user", Content: "Say hello in exactly one short sentence."},
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
		runTest("LLM: response with system prompt", 60*time.Second, func() error {
			client, err := llm.NewClient(llm.ModelGemma4_26B)
			if err != nil {
				return err
			}
			resp, err := client.Generate(context.Background(), &llm.Request{
				SystemPrompt: "You are a helpful assistant. Always respond in Japanese.",
				Messages: []llm.Message{
					{Role: "user", Content: "Say hello in one word."},
				},
			})
			if err != nil {
				return fmt.Errorf("Generate() with system prompt failed: %w", err)
			}
			if resp.Content == "" {
				return fmt.Errorf("empty response content")
			}
			return nil
		}),
	}
	return results
}

// testShellExec runs a simple command inside the sandbox.
func testShellExec() []result {
	// Check if bwrap is available
	if _, err := execLookPath("bwrap"); err != nil {
		return []result{skipTest("shell_exec", "bwrap not available on this machine")}
	}

	return []result{
		runTest("shell_exec: echo", 15*time.Second, func() error {
			tool := tools.NewShellExecTool(workDir, testUserID, sandboxQueue)
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"command": "echo",
			})
			if err != nil {
				return fmt.Errorf("Execute failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			if _, exists := m["output"]; !exists {
				return fmt.Errorf("missing 'output' key in result")
			}
			return nil
		}),
		runTest("shell_exec: missing command", 5*time.Second, func() error {
			tool := tools.NewShellExecTool(workDir, testUserID, sandboxQueue)
			_, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err == nil {
				return fmt.Errorf("expected error for missing command, got nil")
			}
			return nil
		}),
	}
}

func execLookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// ensureWorkdirExists creates the test user's workdir if it doesn't exist.
// Needed because the file tools' path validation walks up to the deepest
// existing ancestor, and if the user dir doesn't exist it incorrectly
// flags paths as escaping.
func ensureWorkdirExists() {
	userDir := filepath.Join(workDir, testUserID)
	os.MkdirAll(userDir, 0755)
}

func testFileWriteRead() []result {
	// Pre-create the user workdir because the path validation walks up
	// to the deepest existing ancestor — if the user dir doesn't exist,
	// it walks up to "data/workdir" which is above the allowed base.
	ensureWorkdirExists()

	return []result{
		runTest("file_write + file_read: round-trip", 10*time.Second, func() error {
			writeTool := tools.NewFileWriteTool(workDir, testUserID)
			readTool := tools.NewFileReadTool(workDir, testUserID)

			content := "Hello, moticlaw! " + time.Now().Format(time.RFC3339)
			path := "test_integration.txt"

			// Write
			wres, err := writeTool.Execute(context.Background(), map[string]interface{}{
				"path":    path,
				"content": content,
			})
			if err != nil {
				return fmt.Errorf("Write failed: %w", err)
			}
			wm, ok := wres.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected write result type: %T", wres)
			}
			if wm["success"] != true {
				return fmt.Errorf("write success flag not true")
			}

			// Read
			rres, err := readTool.Execute(context.Background(), map[string]interface{}{
				"path": path,
			})
			if err != nil {
				return fmt.Errorf("Read failed: %w", err)
			}
			rm, ok := rres.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected read result type: %T", rres)
			}
			got, ok := rm["content"].(string)
			if !ok {
				return fmt.Errorf("content not a string, got %T", rm["content"])
			}
			if got != content {
				return fmt.Errorf("content mismatch:\n  want: %q\n  got:  %q", content, got)
			}
			return nil
		}),
		runTest("file_read: non-existent file", 5*time.Second, func() error {
			readTool := tools.NewFileReadTool(workDir, testUserID)
			_, err := readTool.Execute(context.Background(), map[string]interface{}{
				"path": "nonexistent_file_12345.txt",
			})
			if err == nil {
				return fmt.Errorf("expected error for non-existent file, got nil")
			}
			return nil
		}),
		runTest("file_write: path traversal blocked", 5*time.Second, func() error {
			writeTool := tools.NewFileWriteTool(workDir, testUserID)
			_, err := writeTool.Execute(context.Background(), map[string]interface{}{
				"path":    "../../etc/passwd",
				"content": "evil",
			})
			if err == nil {
				return fmt.Errorf("expected error for path traversal, got nil")
			}
			return nil
		}),
		runTest("file_write: absolute path blocked", 5*time.Second, func() error {
			writeTool := tools.NewFileWriteTool(workDir, testUserID)
			_, err := writeTool.Execute(context.Background(), map[string]interface{}{
				"path":    "/tmp/evil.txt",
				"content": "evil",
			})
			if err == nil {
				return fmt.Errorf("expected error for absolute path, got nil")
			}
			return nil
		}),
	}
}

func testFileDelete() []result {
	// Create the workdir directly (the tool's path validation has issues
	// with non-existent parent directories)
	ensureWorkdirExists()

	// Create a file directly in the workdir for subsequent delete test
	userDir := filepath.Join(workDir, testUserID)
	if err := os.WriteFile(filepath.Join(userDir, "to_be_deleted.txt"), []byte("delete me"), 0644); err != nil {
		return []result{skipTest("file_delete", fmt.Sprintf("setup failed: %v", err))}
	}

	return []result{
		runTest("file_delete: moves to .trash", 10*time.Second, func() error {
			delTool := tools.NewFileDeleteTool(workDir, testUserID)
			result, err := delTool.Execute(context.Background(), map[string]interface{}{
				"path": "to_be_deleted.txt",
			})
			if err != nil {
				return fmt.Errorf("Delete failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			if m["success"] != true {
				return fmt.Errorf("success flag not true")
			}
			trashed, ok := m["trashed"].(string)
			if !ok {
				return fmt.Errorf("trashed path not a string")
			}
			if !strings.HasPrefix(trashed, ".trash/") {
				return fmt.Errorf("trashed path doesn't start with .trash/: %s", trashed)
			}

			// Verify the file is gone from original location
			readTool := tools.NewFileReadTool(workDir, testUserID)
			_, err = readTool.Execute(context.Background(), map[string]interface{}{
				"path": "to_be_deleted.txt",
			})
			if err == nil {
				return fmt.Errorf("file still exists after delete")
			}
			return nil
		}),
	}
}

func testWebFetch() []result {
	return []result{
		runTest("web_fetch: public URL", 30*time.Second, func() error {
			tool := tools.NewWebFetchTool(webFetchLimiter)
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"url": "https://httpbin.org/get",
			})
			if err != nil {
				return fmt.Errorf("Fetch failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			status, ok := m["status"].(int)
			if !ok {
				return fmt.Errorf("status not an int, got %T", m["status"])
			}
			if status != 200 {
				return fmt.Errorf("expected status 200, got %d", status)
			}
			content, ok := m["content"].(string)
			if !ok || content == "" {
				return fmt.Errorf("empty content in response")
			}
			return nil
		}),
		runTest("web_fetch: missing URL param", 5*time.Second, func() error {
			tool := tools.NewWebFetchTool(webFetchLimiter)
			_, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err == nil {
				return fmt.Errorf("expected error for missing URL, got nil")
			}
			return nil
		}),
		runTest("web_fetch: SSRF blocks private IP", 10*time.Second, func() error {
			tool := tools.NewWebFetchTool(webFetchLimiter)
			_, err := tool.Execute(context.Background(), map[string]interface{}{
				"url": "http://127.0.0.1:22",
			})
			if err == nil {
				return fmt.Errorf("expected SSRF block for private IP, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "not allowed") {
				return fmt.Errorf("expected 'not allowed' error, got: %v", err)
			}
			return nil
		}),
	}
}

func testWebSearch() []result {
	return []result{
		runTest("web_search: DuckDuckGo or Tavily", 30*time.Second, func() error {
			tool := tools.NewWebSearchTool(webSearchLimiter)
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"query": "moticlaw Go Discord bot",
			})
			if err != nil {
				return fmt.Errorf("Search failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			results, ok := m["results"]
			if !ok {
				return fmt.Errorf("missing 'results' key")
			}
			// Check results has items (could be []SearchResult or []interface{})
			switch v := results.(type) {
			case []interface{}:
				if len(v) == 0 {
					return fmt.Errorf("empty search results")
				}
			case []tools.SearchResult:
				if len(v) == 0 {
					return fmt.Errorf("empty search results")
				}
			default:
				return fmt.Errorf("unexpected results type: %T", results)
			}
			return nil
		}),
		runTest("web_search: missing query", 5*time.Second, func() error {
			tool := tools.NewWebSearchTool(webSearchLimiter)
			_, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err == nil {
				return fmt.Errorf("expected error for missing query, got nil")
			}
			return nil
		}),
	}
}

func testGithubFetch() []result {
	return []result{
		runTest("github_fetch: public endpoint", 30*time.Second, func() error {
			tool := tools.NewGithubFetchTool()
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"endpoint": "/repos/moti/moticlaw",
			})
			if err != nil {
				return fmt.Errorf("Fetch failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			status, ok := m["status"].(int)
			if !ok {
				return fmt.Errorf("status not an int, got %T", m["status"])
			}
			// 404 is OK (repo doesn't exist), just check it's a valid response
			if status < 200 || status >= 500 {
				return fmt.Errorf("unexpected status: %d", status)
			}
			return nil
		}),
		runTest("github_fetch: missing endpoint", 5*time.Second, func() error {
			tool := tools.NewGithubFetchTool()
			_, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err == nil {
				return fmt.Errorf("expected error for missing endpoint, got nil")
			}
			return nil
		}),
	}
}

func testGithubTrending() []result {
	return []result{
		runTest("github_trending: scrape trending", 30*time.Second, func() error {
			tool := tools.NewGithubFetchTool()
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"action":   "trending",
				"language": "go",
				"since":    "daily",
			})
			if err != nil {
				return fmt.Errorf("Trending scrape failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			status, ok := m["status"].(int)
			if !ok {
				return fmt.Errorf("status not an int, got %T", m["status"])
			}
			if status != 200 {
				return fmt.Errorf("expected status 200, got %d", status)
			}
			data, ok := m["data"]
			if !ok {
				return fmt.Errorf("missing 'data' key")
			}
			repos, ok := data.([]map[string]interface{})
			if !ok {
				return fmt.Errorf("data not a []map[string]interface{}, got %T", data)
			}
			if len(repos) == 0 {
				return fmt.Errorf("no trending repos returned")
			}
			return nil
		}),
	}
}

func testModrinthFetch() []result {
	return []result{
		runTest("modrinth_fetch: public endpoint", 30*time.Second, func() error {
			tool := tools.NewModrinthFetchTool()
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"endpoint": "/v2/project/sodium",
			})
			if err != nil {
				return fmt.Errorf("Fetch failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			status, ok := m["status"].(int)
			if !ok {
				return fmt.Errorf("status not an int, got %T", m["status"])
			}
			if status != 200 {
				return fmt.Errorf("expected status 200, got %d", status)
			}
			return nil
		}),
		runTest("modrinth_fetch: missing endpoint", 5*time.Second, func() error {
			tool := tools.NewModrinthFetchTool()
			_, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err == nil {
				return fmt.Errorf("expected error for missing endpoint, got nil")
			}
			return nil
		}),
	}
}

func testCurseForgeFetch() []result {
	return []result{
		runTest("curseforge_fetch: blocked with message", 5*time.Second, func() error {
			tool := tools.NewCurseForgeFetchTool()
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"query": "test",
			})
			if err != nil {
				return fmt.Errorf("Execute should not error, got: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			msg, ok := m["message"]
			if !ok {
				return fmt.Errorf("missing 'message' key")
			}
			if !strings.Contains(msg.(string), "ブロック") {
				return fmt.Errorf("expected block message, got: %v", msg)
			}
			alt, ok := m["alternative"]
			if !ok || alt != "modrinth_fetch" {
				return fmt.Errorf("expected alternative 'modrinth_fetch', got %v", alt)
			}
			return nil
		}),
	}
}

func testMemoryWriteRead() []result {
	return []result{
		runTest("memory_write + memory_read: round-trip", 10*time.Second, func() error {
			writeTool := tools.NewMemoryWriteTool(testUserID)
			readTool := tools.NewMemoryReadTool(testUserID)

			testContent := "Test memory entry at " + time.Now().Format(time.RFC3339)

			// Write
			wres, err := writeTool.Execute(context.Background(), map[string]interface{}{
				"content": testContent,
			})
			if err != nil {
				return fmt.Errorf("Write failed: %w", err)
			}
			wm, ok := wres.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected write result type: %T", wres)
			}
			if wm["success"] != true {
				return fmt.Errorf("write success flag not true")
			}

			// Read
			rres, err := readTool.Execute(context.Background(), map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("Read failed: %w", err)
			}
			rm, ok := rres.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected read result type: %T", rres)
			}
			content, ok := rm["content"].(string)
			if !ok {
				return fmt.Errorf("content not a string, got %T", rm["content"])
			}
			if !strings.Contains(content, testContent) {
				return fmt.Errorf("read content doesn't contain written content:\n  want substring: %q\n  got: %q", testContent, content)
			}
			return nil
		}),
	}
}

func testPiStatus() []result {
	return []result{
		runTest("pi_status: memory and uptime", 10*time.Second, func() error {
			tool := tools.NewPiStatusTool()
			result, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("Execute failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			// memory and uptime should always work on Linux
			if _, ok := m["memory"]; !ok {
				// Might have error info
				if errMsg, hasErr := m["memory_error"]; hasErr {
					return fmt.Errorf("memory error: %v", errMsg)
				}
				return fmt.Errorf("missing 'memory' key")
			}
			if _, ok := m["uptime"]; !ok {
				if errMsg, hasErr := m["uptime_error"]; hasErr {
					return fmt.Errorf("uptime error: %v", errMsg)
				}
				return fmt.Errorf("missing 'uptime' key")
			}
			// temperature may fail on non-Pi (vcgencmd not found)
			return nil
		}),
	}
}

func testCronRegister() []result {
	// Ensure cron.json exists with valid empty JSON array
	cronPath := filepath.Join("data", "config", "cron.json")
	if _, err := os.Stat(cronPath); err != nil {
		// File doesn't exist or error — write empty array
		os.MkdirAll(filepath.Dir(cronPath), 0755)
		os.WriteFile(cronPath, []byte("[]\n"), 0644)
	} else {
		// File exists but might be empty — ensure valid JSON
		data, _ := os.ReadFile(cronPath)
		if len(data) == 0 {
			os.WriteFile(cronPath, []byte("[]\n"), 0644)
		}
	}

	return []result{
		runTest("cron_register: register valid job", 10*time.Second, func() error {
			tool := tools.NewCronRegisterTool(testUserID, "")
			result, err := tool.Execute(context.Background(), map[string]interface{}{
				"interval": "10m",
				"command":  "echo test",
			})
			if err != nil {
				return fmt.Errorf("Register failed: %w", err)
			}
			m, ok := result.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result)
			}
			if m["success"] != true {
				return fmt.Errorf("success flag not true")
			}
			if _, ok := m["job_id"]; !ok {
				return fmt.Errorf("missing 'job_id' key")
			}
			if m["interval"] != "10m" {
				return fmt.Errorf("expected interval '10m', got %v", m["interval"])
			}
			return nil
		}),
		runTest("cron_register: interval too short", 5*time.Second, func() error {
			tool := tools.NewCronRegisterTool(testUserID, "")
			_, err := tool.Execute(context.Background(), map[string]interface{}{
				"interval": "1m",
				"command":  "echo test",
			})
			if err == nil {
				return fmt.Errorf("expected error for too-short interval, got nil")
			}
			if !strings.Contains(err.Error(), "minimum") {
				return fmt.Errorf("expected 'minimum interval' error, got: %v", err)
			}
			return nil
		}),
		runTest("cron_register: missing params", 5*time.Second, func() error {
			tool := tools.NewCronRegisterTool(testUserID, "")
			_, err := tool.Execute(context.Background(), map[string]interface{}{})
			if err == nil {
				return fmt.Errorf("expected error for missing params, got nil")
			}
			return nil
		}),
	}
}

func testSSRFGuard() []result {
	return []result{
		runTest("SSRF: private IPs are blocked", 10*time.Second, func() error {
			g := security.NewSSRFGuard()
			tests := []struct {
				host   string
				safe   bool
			}{
				{"127.0.0.1", false},
				{"localhost", false},
				{"10.0.0.1", false},
				{"192.168.1.1", false},
				{"172.16.0.1", false},
				{"172.31.255.255", false},
				{"169.254.1.1", false},
				{"google.com", true},
				{"github.com", true},
				{"1.1.1.1", true},
			}
			for _, tc := range tests {
				got := g.IsSafeHost(tc.host)
				if got != tc.safe {
					return fmt.Errorf("IsSafeHost(%q) = %v; want %v", tc.host, got, tc.safe)
				}
			}
			return nil
		}),
	}
}

func testRateLimiter() []result {
	return []result{
		runTest("RateLimiter: token bucket behavior", 5*time.Second, func() error {
			rl := security.NewRateLimiter(3)
			defer rl.Stop()

			// First 3 should succeed
			for i := 0; i < 3; i++ {
				if !rl.Allow() {
					return fmt.Errorf("Allow() returned false on iteration %d; want true", i)
				}
			}
			// 4th should fail (no refill yet)
			if rl.Allow() {
				return fmt.Errorf("Allow() returned true after consuming all tokens; want false")
			}
			return nil
		}),
		runTest("RateLimiter: zero capacity rejects all", 2*time.Second, func() error {
			rl := security.NewRateLimiter(0)
			defer rl.Stop()
			if rl.Allow() {
				return fmt.Errorf("Allow() returned true for zero-capacity limiter; want false")
			}
			return nil
		}),
	}
}

func testToolsRegistry() []result {
	return []result{
		runTest("Registry: DefaultRegistry has all tools", 2*time.Second, func() error {
			reg, err := tools.DefaultRegistry(workDir, testUserID, "", sandboxQueue, webSearchLimiter, webFetchLimiter, nil)
			if err != nil {
				return fmt.Errorf("DefaultRegistry failed: %w", err)
			}
			expectedTools := []string{
				"shell_exec", "file_read", "file_write", "file_delete",
				"web_search", "web_fetch",
				"memory_read", "memory_write",
				"cron_register", "discord_fetch",
				"github_fetch", "modrinth_fetch",
				"curseforge_fetch", "pi_status",
			}
			for _, name := range expectedTools {
				if _, ok := reg.Get(name); !ok {
					return fmt.Errorf("missing tool: %s", name)
				}
			}
			return nil
		}),
		runTest("Registry: GetDefinitions returns valid JSON schemas", 2*time.Second, func() error {
			reg, err := tools.DefaultRegistry(workDir, testUserID, "", sandboxQueue, webSearchLimiter, webFetchLimiter, nil)
			if err != nil {
				return fmt.Errorf("DefaultRegistry failed: %w", err)
			}
			defs := reg.GetDefinitions()
			if len(defs) < 10 {
				return fmt.Errorf("expected at least 10 tool definitions, got %d", len(defs))
			}
			for _, d := range defs {
				if d.Name == "" {
					return fmt.Errorf("tool definition with empty name")
				}
				if d.Description == "" {
					return fmt.Errorf("tool %q has empty description", d.Name)
				}
				if d.Parameters == nil {
					return fmt.Errorf("tool %q has nil parameters", d.Name)
				}
			}
			return nil
		}),
	}
}

// ============================================================================
// Main
// ============================================================================

func main() {
	rand.Seed(time.Now().UnixNano())

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              moticlaw Integration Test Harness              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// ---- Environment ----
	fmt.Println("── Environment ──────────────────────────────────────────────")
	envErr := loadEnv()
	if envErr != nil {
		fmt.Printf("  ⚠ %v\n", envErr)
		fmt.Println("  Some environment-dependent tests may fail or be skipped.")
	}
	fmt.Println("  " + envSummary())
	fmt.Println()

	// ---- LLM Client ----
	fmt.Println("── LLM Client ───────────────────────────────────────────────")
	group("LLM Client", testLLMClient())

	// ---- Scenario Tests (Real-World Usage) ----
	fmt.Println("── Scenario Tests ───────────────────────────────────────────")
	fmt.Println("  S1: End-to-end conversation with tool calling")
	group("S1: E2E Conversation", testScenarioE2EConversation())
	fmt.Println("  S2: Multiple rapid requests")
	group("S2: Rapid Requests", testScenarioRapidRequests())
	fmt.Println("  S3: Large message handling")
	group("S3: Large Messages", testScenarioLargeMessage())
	fmt.Println("  S5: Error recovery")
	group("S5: Error Recovery", testScenarioErrorRecovery())

	// ---- Tools ----
	fmt.Println("── Tools ────────────────────────────────────────────────────")
	group("File Operations", testFileWriteRead())
	group("File Delete", testFileDelete())
	group("Shell Exec", testShellExec())
	group("Web Fetch", testWebFetch())
	group("Web Search", testWebSearch())
	group("GitHub Fetch", testGithubFetch())
	group("GitHub Trending", testGithubTrending())
	group("Modrinth Fetch", testModrinthFetch())
	group("CurseForge Fetch", testCurseForgeFetch())
	group("Memory", testMemoryWriteRead())
	group("Pi Status", testPiStatus())
	group("Cron Register", testCronRegister())
	group("Tools Registry", testToolsRegistry())
	group("S4: Discord Format", testScenarioDiscordFormat())
	group("S6: Memory Growth", testScenarioMemoryGrowth())
	group("S7: Cron Persistence", testScenarioCronPersistence())

	// ---- Security ----
	fmt.Println("── Security ─────────────────────────────────────────────────")
	group("SSRF Guard", testSSRFGuard())
	group("Rate Limiter", testRateLimiter())

	// ---- Results ----
	fmt.Println()
	fmt.Println("── Results ──────────────────────────────────────────────────")

	for _, g := range allResults {
		if len(g.tests) == 0 {
			continue
		}
		fmt.Printf("  %s:\n", g.name)
		for _, r := range g.tests {
			fmt.Println(r.String())
		}
	}

	fmt.Println()
	fmt.Println("── Summary ──────────────────────────────────────────────────")
	fmt.Printf("  Total:  %d\n", passed+failed+skipped)
	fmt.Printf("  Passed: %d\n", passed)
	fmt.Printf("  Failed: %d\n", failed)
	fmt.Printf("  Skipped: %d\n", skipped)
	fmt.Println()

	// Cleanup test data
	cleanupTestData()

	if failed > 0 {
		fmt.Println("  Overall: FAIL")
		os.Exit(1)
	}
	fmt.Println("  Overall: PASS")
}

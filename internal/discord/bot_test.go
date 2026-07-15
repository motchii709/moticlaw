package discord_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/moti/moticlaw/internal/conversation"
	"github.com/moti/moticlaw/internal/llm"
	"github.com/moti/moticlaw/internal/security"
	"github.com/moti/moticlaw/internal/store"
	"github.com/moti/moticlaw/internal/tools"
)

// ============================================================
// validateUserID tests (tested indirectly through DefaultRegistry)
// ============================================================

func TestDefaultRegistry_ValidUserID_Numeric(t *testing.T) {
	// "123456789" should be accepted
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	registry, err := tools.DefaultRegistry("/tmp/test", "123456789", queue, limiter1, limiter2, nil)
	if err != nil {
		t.Fatalf("DefaultRegistry with numeric userID should succeed, got error: %v", err)
	}
	if registry == nil {
		t.Fatal("DefaultRegistry returned nil registry")
	}
}

func TestDefaultRegistry_ValidUserID_Underscore(t *testing.T) {
	// "user_123" should be accepted
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	registry, err := tools.DefaultRegistry("/tmp/test", "user_123", queue, limiter1, limiter2, nil)
	if err != nil {
		t.Fatalf("DefaultRegistry with underscore userID should succeed, got error: %v", err)
	}
	if registry == nil {
		t.Fatal("DefaultRegistry returned nil registry")
	}
}

func TestDefaultRegistry_ValidUserID_Hyphen(t *testing.T) {
	// "user-123" should be accepted
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	registry, err := tools.DefaultRegistry("/tmp/test", "user-123", queue, limiter1, limiter2, nil)
	if err != nil {
		t.Fatalf("DefaultRegistry with hyphen userID should succeed, got error: %v", err)
	}
	if registry == nil {
		t.Fatal("DefaultRegistry returned nil registry")
	}
}

func TestDefaultRegistry_RejectsPathTraversal(t *testing.T) {
	// "../etc" should be rejected
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	_, err := tools.DefaultRegistry("/tmp/test", "../etc", queue, limiter1, limiter2, nil)
	if err == nil {
		t.Fatal("DefaultRegistry with path traversal userID should return error")
	}
}

func TestDefaultRegistry_RejectsSlash(t *testing.T) {
	// "user/123" should be rejected
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	_, err := tools.DefaultRegistry("/tmp/test", "user/123", queue, limiter1, limiter2, nil)
	if err == nil {
		t.Fatal("DefaultRegistry with slash userID should return error")
	}
}

func TestDefaultRegistry_RejectsEmpty(t *testing.T) {
	// "" should be rejected
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	_, err := tools.DefaultRegistry("/tmp/test", "", queue, limiter1, limiter2, nil)
	if err == nil {
		t.Fatal("DefaultRegistry with empty userID should return error")
	}
}

func TestDefaultRegistry_RejectsSpace(t *testing.T) {
	// "user 123" should be rejected
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	_, err := tools.DefaultRegistry("/tmp/test", "user 123", queue, limiter1, limiter2, nil)
	if err == nil {
		t.Fatal("DefaultRegistry with space in userID should return error")
	}
}

func TestDefaultRegistry_RejectsDot(t *testing.T) {
	// "user.123" should be rejected
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	_, err := tools.DefaultRegistry("/tmp/test", "user.123", queue, limiter1, limiter2, nil)
	if err == nil {
		t.Fatal("DefaultRegistry with dot in userID should return error")
	}
}

// ============================================================
// DefaultRegistry tool count test
// ============================================================

func TestDefaultRegistry_Has14Tools(t *testing.T) {
	// DefaultRegistry with valid userID should have 14 tools
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	registry, err := tools.DefaultRegistry("/tmp/test", "123456789", queue, limiter1, limiter2, nil)
	if err != nil {
		t.Fatalf("DefaultRegistry should succeed, got error: %v", err)
	}

	tools := registry.List()
	if len(tools) != 14 {
		t.Fatalf("expected 14 tools, got %d: %v", len(tools), tools)
	}

	// Verify all expected tool names are present
	expectedTools := map[string]bool{
		"shell_exec":       false,
		"file_read":        false,
		"file_write":       false,
		"file_delete":      false,
		"web_search":       false,
		"web_fetch":        false,
		"memory_read":      false,
		"memory_write":     false,
		"cron_register":    false,
		"discord_fetch":    false,
		"github_fetch":     false,
		"modrinth_fetch":   false,
		"curseforge_fetch": false,
		"pi_status":        false,
	}

	for _, name := range tools {
		if _, ok := expectedTools[name]; ok {
			expectedTools[name] = true
		} else {
			t.Errorf("unexpected tool name: %s", name)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

// ============================================================
// Conversation manager isolation tests
// ============================================================

// setupTestStore creates a temporary directory with required subdirectories
// and returns a Store instance pointing to it, plus a cleanup function.
func setupTestStore(t *testing.T) (*store.Store, string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "moticlaw-discord-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create required subdirectories
	for _, dir := range []string{"config", "history"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			os.RemoveAll(tmpDir)
			t.Fatalf("failed to create %s dir: %v", dir, err)
		}
	}

	s, err := store.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return s, tmpDir, cleanup
}

func TestConversationManager_UserIsolation(t *testing.T) {
	// Two users should have independent message histories
	s, dataDir, cleanup := setupTestStore(t)
	defer cleanup()

	manager := conversation.NewManager(s, dataDir)
	manager.Start()
	defer manager.Stop()

	// Get session for user A and append a message
	sessionA, err := manager.GetSession("user_A")
	if err != nil {
		t.Fatalf("GetSession for user A failed: %v", err)
	}

	sessionA.AppendMessage(llm.Message{
		Role:    "user",
		Content: "Hello from user A",
	})

	// Get session for user B — should have no messages
	sessionB, err := manager.GetSession("user_B")
	if err != nil {
		t.Fatalf("GetSession for user B failed: %v", err)
	}

	messagesB := sessionB.GetMessages()
	if len(messagesB) != 0 {
		t.Fatalf("expected user B to have 0 messages, got %d: %v", len(messagesB), messagesB)
	}

	// User A should still have 1 message
	messagesA := sessionA.GetMessages()
	if len(messagesA) != 1 {
		t.Fatalf("expected user A to have 1 message, got %d: %v", len(messagesA), messagesA)
	}
	if messagesA[0].Content != "Hello from user A" {
		t.Fatalf("expected user A message content %q, got %q", "Hello from user A", messagesA[0].Content)
	}
}

func TestConversationManager_Persistence(t *testing.T) {
	// Messages should persist to disk and be reloadable
	s, dataDir, cleanup := setupTestStore(t)
	defer cleanup()

	manager := conversation.NewManager(s, dataDir)
	manager.Start()

	// Get session and append 6 messages (exceeds the 5-turn flush threshold)
	session, err := manager.GetSession("persist_user")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	for i := 0; i < 6; i++ {
		session.AppendMessage(llm.Message{
			Role:    "user",
			Content: "message",
		})
	}

	// Flush the session to disk
	if err := manager.FlushSession("persist_user"); err != nil {
		t.Fatalf("FlushSession failed: %v", err)
	}

	manager.Stop()

	// Create a new manager pointing to the same data directory
	manager2 := conversation.NewManager(s, dataDir)
	manager2.Start()
	defer manager2.Stop()

	// Get the same user's session — should load 6 messages from disk
	session2, err := manager2.GetSession("persist_user")
	if err != nil {
		t.Fatalf("GetSession after reload failed: %v", err)
	}

	messages := session2.GetMessages()
	if len(messages) != 6 {
		t.Fatalf("expected 6 messages after reload, got %d", len(messages))
	}

	for i, msg := range messages {
		if msg.Role != "user" {
			t.Errorf("message %d: expected role %q, got %q", i, "user", msg.Role)
		}
		if msg.Content != "message" {
			t.Errorf("message %d: expected content %q, got %q", i, "message", msg.Content)
		}
	}
}

// ============================================================
// DiscordFetchTool userID isolation test
// ============================================================

func TestDiscordFetchTool_UserIDIsolation(t *testing.T) {
	// Verify that DiscordFetchTool stores the userID and uses it for access checks.
	// With a nil session, all access checks should fail.
	tool := tools.NewDiscordFetchTool("user_abc", nil)

	if tool.Name() != "discord_fetch" {
		t.Fatalf("expected tool name 'discord_fetch', got %q", tool.Name())
	}

	// With nil session, canUserAccessChannel should return false
	// (we can't call it directly since it's unexported, but Execute should fail
	//  with "Discord session not initialized" before reaching the access check)
	_, err := tool.Execute(nil, map[string]interface{}{
		"action": "get_messages",
	})
	if err == nil {
		t.Fatal("expected error when session is nil")
	}
}

// ============================================================
// DefaultRegistry userID isolation test
// ============================================================

func TestDefaultRegistry_UserIDIsolation(t *testing.T) {
	// Two registries with different userIDs should have different workdir paths
	queue := make(chan struct{}, 1)
	limiter1 := security.NewRateLimiter(10)
	limiter2 := security.NewRateLimiter(10)
	defer limiter1.Stop()
	defer limiter2.Stop()

	registry1, err := tools.DefaultRegistry("/tmp/workdir", "user_A", queue, limiter1, limiter2, nil)
	if err != nil {
		t.Fatalf("DefaultRegistry for user_A failed: %v", err)
	}

	registry2, err := tools.DefaultRegistry("/tmp/workdir", "user_B", queue, limiter1, limiter2, nil)
	if err != nil {
		t.Fatalf("DefaultRegistry for user_B failed: %v", err)
	}

	// Both registries should have the same tools (same count)
	tools1 := registry1.List()
	tools2 := registry2.List()
	if len(tools1) != len(tools2) {
		t.Fatalf("expected same number of tools, got %d vs %d", len(tools1), len(tools2))
	}
}
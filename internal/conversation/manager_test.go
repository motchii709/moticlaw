package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moti/moticlaw/internal/llm"
	"github.com/moti/moticlaw/internal/store"
)

// setupTestManager creates a temporary directory and returns a Manager with Store.
func setupTestManager(t *testing.T) (*Manager, *store.Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "moticlaw-conv-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create required subdirectories
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create config dir: %v", err)
	}

	st, err := store.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}

	mgr := NewManager(st, tmpDir)

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return mgr, st, cleanup
}

func TestNewManager(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.sessions == nil {
		t.Fatal("sessions map is nil")
	}

	if cap(mgr.sandboxQueue) != 1 {
		t.Fatalf("expected sandboxQueue buffer size 1, got %d", cap(mgr.sandboxQueue))
	}
}

func TestGetSession_New(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	if session == nil {
		t.Fatal("GetSession returned nil")
	}

	if session.userID != "user123" {
		t.Fatalf("expected userID 'user123', got %q", session.userID)
	}

	// New session should have empty messages
	msgs := session.GetMessages()
	if len(msgs) != 0 {
		t.Fatalf("expected empty messages, got %d", len(msgs))
	}
}

func TestGetSession_Cached(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	s1, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("first GetSession returned error: %v", err)
	}

	s2, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("second GetSession returned error: %v", err)
	}

	if s1 != s2 {
		t.Fatal("GetSession returned different pointers for same user (expected cached)")
	}
}

func TestGetSession_MultipleUsers(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	s1, err := mgr.GetSession("userA")
	if err != nil {
		t.Fatalf("GetSession(userA) returned error: %v", err)
	}

	s2, err := mgr.GetSession("userB")
	if err != nil {
		t.Fatalf("GetSession(userB) returned error: %v", err)
	}

	if s1 == s2 {
		t.Fatal("different users should have different session pointers")
	}

	if len(mgr.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(mgr.sessions))
	}
}

func TestAppendMessage(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	msg := llm.Message{Role: "user", Content: "hello"}
	session.AppendMessage(msg)

	msgs := session.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}

	if session.DirtyCount() != 1 {
		t.Fatalf("expected dirtyCount=1, got %d", session.DirtyCount())
	}
}

func TestGetMessages_Cap(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	// Add 110 messages (55 turns)
	for i := 0; i < 110; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		session.AppendMessage(llm.Message{Role: role, Content: "msg"})
	}

	msgs := session.GetMessages()
	if len(msgs) != 100 {
		t.Fatalf("expected 100 messages (capped), got %d", len(msgs))
	}

	// Verify dirtyCount (should be 110 since each AppendMessage increments it)
	if session.DirtyCount() != 110 {
		t.Fatalf("expected dirtyCount=110, got %d", session.DirtyCount())
	}
}

func TestResetDirty(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	session.AppendMessage(llm.Message{Role: "user", Content: "hi"})
	session.AppendMessage(llm.Message{Role: "assistant", Content: "hello"})

	if session.DirtyCount() != 2 {
		t.Fatalf("expected dirtyCount=2, got %d", session.DirtyCount())
	}

	session.ResetDirty()
	if session.DirtyCount() != 0 {
		t.Fatalf("expected dirtyCount=0 after reset, got %d", session.DirtyCount())
	}
}

func TestFlushSession(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	session.AppendMessage(llm.Message{Role: "user", Content: "hello"})
	session.AppendMessage(llm.Message{Role: "assistant", Content: "world"})

	if err := mgr.FlushSession("user123"); err != nil {
		t.Fatalf("FlushSession returned error: %v", err)
	}

	// After flush, dirtyCount should be 0
	if session.DirtyCount() != 0 {
		t.Fatalf("expected dirtyCount=0 after flush, got %d", session.DirtyCount())
	}

	// Verify the file was written
	entries, err := mgr.store.LoadHistory("user123")
	if err != nil {
		t.Fatalf("LoadHistory returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(entries))
	}
	if entries[0].Role != "user" || entries[0].Content != "hello" {
		t.Fatalf("unexpected entry[0]: %+v", entries[0])
	}
	if entries[1].Role != "assistant" || entries[1].Content != "world" {
		t.Fatalf("unexpected entry[1]: %+v", entries[1])
	}
}

func TestFlushSession_Nonexistent(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Should not error for non-existent session
	if err := mgr.FlushSession("nonexistent"); err != nil {
		t.Fatalf("FlushSession on nonexistent user returned error: %v", err)
	}
}

func TestEvictSession(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	session.AppendMessage(llm.Message{Role: "user", Content: "test"})

	// Evict — should flush and remove from map
	mgr.EvictSession("user123")

	// Check session is no longer in map
	mgr.mu.RLock()
	_, exists := mgr.sessions["user123"]
	mgr.mu.RUnlock()
	if exists {
		t.Fatal("session should not exist after eviction")
	}

	// But file should exist
	entries, err := mgr.store.LoadHistory("user123")
	if err != nil {
		t.Fatalf("LoadHistory returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry after eviction, got %d", len(entries))
	}
}

func TestGetSession_LoadsFromDisk(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	// First, create a session and flush it
	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	session.AppendMessage(llm.Message{Role: "user", Content: "saved message"})
	if err := mgr.FlushSession("user123"); err != nil {
		t.Fatalf("FlushSession returned error: %v", err)
	}

	// Evict to remove from cache
	mgr.EvictSession("user123")

	// Now get session again — should load from disk
	session2, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession after eviction returned error: %v", err)
	}

	msgs := session2.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message from disk, got %d", len(msgs))
	}
	if msgs[0].Content != "saved message" {
		t.Fatalf("expected 'saved message', got %q", msgs[0].Content)
	}
}

func TestSandboxQueue(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	q := mgr.SandboxQueue()
	if q == nil {
		t.Fatal("SandboxQueue returned nil")
	}

	// Should be able to acquire (buffer 1)
	select {
	case q <- struct{}{}:
	default:
		t.Fatal("should be able to acquire sandbox queue")
	}

	// Second acquire should block (buffer full)
	select {
	case q <- struct{}{}:
		t.Fatal("should NOT be able to acquire already-held queue")
	default:
		// Expected — buffer is full
	}

	// Release
	<-q
}

func TestStartStop(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Start goroutines
	mgr.Start()

	// Give them a moment to start
	time.Sleep(10 * time.Millisecond)

	// Stop should not block
	done := make(chan struct{})
	go func() {
		mgr.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop timed out (goroutines not stopping)")
	}
}

func TestStartStop_StopsOnlyOnce(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	mgr.Start()

	// Calling Stop twice should be safe
	mgr.Stop()
	mgr.Stop() // should not panic
}

func TestConcurrentAccess(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	var wg sync.WaitGroup
	n := 20

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			session, err := mgr.GetSession(userID)
			if err != nil {
				t.Errorf("GetSession(%s) error: %v", userID, err)
				return
			}
			session.AppendMessage(llm.Message{Role: "user", Content: "concurrent test"})
		}(fmt.Sprintf("user%d", i))
	}

	wg.Wait()

	mgr.mu.RLock()
	count := len(mgr.sessions)
	mgr.mu.RUnlock()
	if count != n {
		t.Fatalf("expected %d sessions, got %d", n, count)
	}
}

func TestFlushSession_Concurrent(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	session.AppendMessage(llm.Message{Role: "user", Content: "test"})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.FlushSession("user123")
		}()
	}
	wg.Wait()

	// Should not panic or race
	entries, err := mgr.store.LoadHistory("user123")
	if err != nil {
		t.Fatalf("LoadHistory error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestMemoryEstimate(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("user123")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	// Add a message with known content length
	content := "hello world"
	session.AppendMessage(llm.Message{Role: "user", Content: content})

	session.mu.Lock()
	estimate := session.memoryEstimate()
	session.mu.Unlock()

	expected := len(content) + 100
	if estimate != expected {
		t.Fatalf("expected memory estimate %d, got %d", expected, estimate)
	}
}

func TestGetSession_LoadFromDiskWithHistory(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Manually save history to disk
	entries := []store.HistoryEntry{
		{Role: "user", Content: "stored1"},
		{Role: "assistant", Content: "stored2"},
	}
	if err := mgr.store.SaveHistory("existing_user", entries); err != nil {
		t.Fatalf("SaveHistory error: %v", err)
	}

	// Get session — should load from disk
	session, err := mgr.GetSession("existing_user")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	msgs := session.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages loaded from disk, got %d", len(msgs))
	}
	if msgs[0].Content != "stored1" || msgs[1].Content != "stored2" {
		t.Fatalf("unexpected loaded messages: %+v", msgs)
	}
}

func TestGetSession_HistFileNotExist(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Getting a session for a user without a history file should return empty messages, not error
	session, err := mgr.GetSession("brand_new_user")
	if err != nil {
		t.Fatalf("GetSession for new user returned error: %v", err)
	}

	msgs := session.GetMessages()
	if len(msgs) != 0 {
		t.Fatalf("expected empty messages for new user, got %d", len(msgs))
	}
}

func TestIdleSweeper(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("idle_user")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	// Manually set lastAccess to 31 minutes ago
	session.mu.Lock()
	session.lastAccess = time.Now().Add(-31 * time.Minute)
	session.mu.Unlock()

	// Run sweeper
	mgr.sweepIdleSessions()

	// Should be evicted
	mgr.mu.RLock()
	_, exists := mgr.sessions["idle_user"]
	mgr.mu.RUnlock()
	if exists {
		t.Fatal("idle session should have been evicted")
	}
}

func TestFlushChecker(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("dirty_user")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	// Mark dirtyCount = 5
	session.mu.Lock()
	session.dirtyCount = 5
	session.mu.Unlock()

	// Run flush checker
	mgr.flushDirtySessions()

	// Should be flushed and dirtyCount reset
	if session.DirtyCount() != 0 {
		t.Fatalf("expected dirtyCount=0 after flush, got %d", session.DirtyCount())
	}

	// Verify file was written
	entries, err := mgr.store.LoadHistory("dirty_user")
	if err != nil {
		t.Fatalf("LoadHistory error: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil entries after flush")
	}
}

func TestFlushChecker_RAMBudget(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	session, err := mgr.GetSession("big_user")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}

	// Add enough messages to exceed 100MB estimate
	// Each message: ~1MB content + 100 bytes overhead ≈ 1,000,100 bytes
	// Need ~101 messages to exceed 100MB
	bigContent := make([]byte, 1024*1024) // 1MB
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	content := string(bigContent)

	for i := 0; i < 110; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		session.AppendMessage(llm.Message{Role: role, Content: content})
	}

	// Verify we're over budget
	session.mu.Lock()
	estimate := session.memoryEstimate()
	session.mu.Unlock()
	if estimate <= 100*1024*1024 {
		t.Fatalf("expected memory estimate > 100MB, got %d bytes", estimate)
	}

	// Run flush checker
	mgr.flushDirtySessions()

	// Should have trimmed to 10 messages
	msgs := session.GetMessages()
	if len(msgs) > 10 {
		t.Fatalf("expected at most 10 messages after RAM budget trim, got %d", len(msgs))
	}
	if len(msgs) == 0 {
		t.Fatal("expected some messages after trim (kept last 10), got 0")
	}
}

func TestToHistoryEntries(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}

	entries := toHistoryEntries(messages)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Role != "user" || entries[0].Content != "hello" {
		t.Fatalf("unexpected entry[0]: %+v", entries[0])
	}
	if entries[1].Role != "assistant" || entries[1].Content != "world" {
		t.Fatalf("unexpected entry[1]: %+v", entries[1])
	}
}

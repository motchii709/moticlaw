package conversation

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/moti/moticlaw/internal/llm"
	"github.com/moti/moticlaw/internal/store"
)

// Manager は会話セッションを管理する
type Manager struct {
	store        *store.Store
	dataDir      string
	sessions     map[string]*Session
	mu           sync.RWMutex
	sandboxQueue chan struct{} // グローバルセマフォ、buffer=1
	stopCh       chan struct{}
	stopOnce     sync.Once
}

// NewManager は新しいManagerを作成する
func NewManager(st *store.Store, dataDir string) *Manager {
	return &Manager{
		store:        st,
		dataDir:      dataDir,
		sessions:     make(map[string]*Session),
		sandboxQueue: make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
	}
}

// Start はバックグラウンドgoroutine（アイドルスイーパー、フラッシュチェッカー）を起動する
func (m *Manager) Start() {
	// Idle sweeper: 30分以上アイドル状態のセッションを排除
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.sweepIdleSessions()
			}
		}
	}()

	// Flush checker: dirtyCount >= 5 のセッションをディスクに書き出す
	// 同時にRAM予算超過チェックも行う
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.flushDirtySessions()
			}
		}
	}()
}

// Stop はバックグラウンドgoroutineを停止し、未保存のセッションを全てディスクに書き出す
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})

	// 全てのセッションをフラッシュ
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		if err := m.FlushSession(id); err != nil {
			log.Printf("Failed to flush session %s during Stop: %v", id, err)
		}
	}
}

// GetSession は指定ユーザーのセッションを取得する（なければ新規作成する）
func (m *Manager) GetSession(userID string) (*Session, error) {
	// Fast path: 既にキャッシュされているか確認
	m.mu.RLock()
	session, exists := m.sessions[userID]
	m.mu.RUnlock()
	if exists {
		return session, nil
	}

	// ディスクから履歴を読み込む
	entries, err := m.store.LoadHistory(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load history for user %s: %w", userID, err)
	}

	messages := make([]llm.Message, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, llm.Message{
			Role:    entry.Role,
			Content: entry.Content,
		})
	}

	session = &Session{
		userID:     userID,
		messages:   messages,
		lastAccess: time.Now(),
	}

	// マップに保存（ダブルチェックロッキング）
	m.mu.Lock()
	if existing, exists := m.sessions[userID]; exists {
		m.mu.Unlock()
		return existing, nil
	}
	m.sessions[userID] = session
	m.mu.Unlock()

	return session, nil
}

// SandboxQueue はグローバルなsandbox実行キューを返す
func (m *Manager) SandboxQueue() chan struct{} {
	return m.sandboxQueue
}

// FlushSession は特定のユーザーセッションをディスクに書き出す
func (m *Manager) FlushSession(userID string) error {
	m.mu.RLock()
	session, exists := m.sessions[userID]
	m.mu.RUnlock()
	if !exists {
		return nil
	}

	session.mu.Lock()
	messages := make([]llm.Message, len(session.messages))
	copy(messages, session.messages)
	session.dirtyCount = 0
	session.mu.Unlock()

	// llm.Message → store.HistoryEntry に変換
	entries := toHistoryEntries(messages)

	if err := m.store.SaveHistory(userID, entries); err != nil {
		return fmt.Errorf("failed to save history for user %s: %w", userID, err)
	}

	return nil
}

// EvictSession はセッションを排除する（フラッシュ後、マップから削除）
func (m *Manager) EvictSession(userID string) {
	if err := m.FlushSession(userID); err != nil {
		log.Printf("Failed to flush session %s during eviction: %v", userID, err)
	}

	m.mu.Lock()
	delete(m.sessions, userID)
	m.mu.Unlock()
}

// sweepIdleSessions は30分以上アイドル状態のセッションを排除する
func (m *Manager) sweepIdleSessions() {
	m.mu.RLock()
	ids := make([]string, 0)
	for id, session := range m.sessions {
		session.mu.Lock()
		idle := time.Since(session.lastAccess)
		session.mu.Unlock()
		if idle > 30*time.Minute {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range ids {
		m.EvictSession(id)
	}
}

// flushDirtySessions はdirtyCount >= 5 またはRAM予算超過のセッションをディスクに書き出す
func (m *Manager) flushDirtySessions() {
	type sessionAction struct {
		id         string
		overBudget bool
	}

	m.mu.RLock()
	var actions []sessionAction
	for id, session := range m.sessions {
		session.mu.Lock()
		dirty := session.dirtyCount >= 5
		overBudget := session.memoryEstimate() > 100*1024*1024 // 100MB
		session.mu.Unlock()

		if dirty || overBudget {
			actions = append(actions, sessionAction{id: id, overBudget: overBudget})
		}
	}
	m.mu.RUnlock()

	for _, a := range actions {
		if err := m.FlushSession(a.id); err != nil {
			log.Printf("Failed to flush session %s: %v", a.id, err)
			continue
		}

		if a.overBudget {
			m.trimSession(a.id)
		}
	}
}

// trimSession はセッションのメモリ使用量を削減する（最後の10メッセージのみ保持）
func (m *Manager) trimSession(userID string) {
	m.mu.RLock()
	session, exists := m.sessions[userID]
	m.mu.RUnlock()
	if !exists {
		return
	}

	session.mu.Lock()
	if len(session.messages) > 10 {
		kept := make([]llm.Message, 10)
		copy(kept, session.messages[len(session.messages)-10:])
		session.messages = kept
	}
	session.mu.Unlock()
}

// toHistoryEntries はllm.Messageスライスをstore.HistoryEntryスライスに変換する
// テキストメッセージのみを保存対象とする（FunctionCall/FunctionResponseはスキップ）
func toHistoryEntries(messages []llm.Message) []store.HistoryEntry {
	entries := make([]store.HistoryEntry, 0, len(messages))
	for _, msg := range messages {
		// 現在のllm.Messageはrole+contentのみだが、将来FunctionCall/FunctionResponse
		// フィールドが追加された場合に備えて、テキストメッセージのみを保存する構造にしている
		entries = append(entries, store.HistoryEntry{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return entries
}

// ============================================================
// Session
// ============================================================

// Session は1ユーザーの会話セッションを表す
type Session struct {
	userID     string
	messages   []llm.Message
	mu         sync.Mutex
	lastAccess time.Time
	dirtyCount int
}

// GetMessages はメッセージ一覧を返す（最大100件 = 50ターン分）
func (s *Session) GetMessages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.messages) > 100 {
		cp := make([]llm.Message, 100)
		copy(cp, s.messages[len(s.messages)-100:])
		return cp
	}
	cp := make([]llm.Message, len(s.messages))
	copy(cp, s.messages)
	return cp
}

// AppendMessage はメッセージを追加する
func (s *Session) AppendMessage(msg llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	s.lastAccess = time.Now()
	s.dirtyCount++
}

// LastAccess は最終アクセス時刻を返す
func (s *Session) LastAccess() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAccess
}

// DirtyCount は未保存ターン数を返す
func (s *Session) DirtyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirtyCount
}

// ResetDirty は未保存カウンタをリセットする
func (s *Session) ResetDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirtyCount = 0
}

// memoryEstimate はセッションのメモリ使用量の概算を返す
func (s *Session) memoryEstimate() int {
	total := 0
	for _, msg := range s.messages {
		total += len(msg.Content) + 100 // オーバーヘッド100バイト
	}
	return total
}

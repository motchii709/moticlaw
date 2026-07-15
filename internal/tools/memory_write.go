package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// MemoryWriteTool は永続メモリに書き込むツール
type MemoryWriteTool struct {
	userID string
}

// NewMemoryWriteTool は新しいMemoryWriteToolを作成する
func NewMemoryWriteTool(userID string) *MemoryWriteTool {
	return &MemoryWriteTool{
		userID: userID,
	}
}

// Name はツール名を返す
func (t *MemoryWriteTool) Name() string {
	return "memory_write"
}

// Description はツールの説明を返す
func (t *MemoryWriteTool) Description() string {
	return "永続メモリに内容を書き込みます。"
}

// Execute は永続メモリに書き込む
func (t *MemoryWriteTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content parameter is required")
	}

	// メモリファイルのパス
	memoryPath := filepath.Join("data", "memory", fmt.Sprintf("%s.md", t.userID))

	// ディレクトリが存在することを確認
	dir := filepath.Dir(memoryPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// 既存のメモリを読み取り
	var existingContent string
	if data, err := os.ReadFile(memoryPath); err == nil {
		existingContent = string(data)
	}

	// 新しい内容を追加（セクション区切りを追加）
	newContent := existingContent
	if existingContent != "" {
		newContent += "\n\n---\n\n"
	}
	newContent += content

	// ファイルに書き込み
	if err := os.WriteFile(memoryPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write memory: %w", err)
	}

	return map[string]interface{}{
		"success": true,
	}, nil
}

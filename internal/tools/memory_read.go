package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// MemoryReadTool は永続メモリを読み取るツール
type MemoryReadTool struct {
	userID string
}

// NewMemoryReadTool は新しいMemoryReadToolを作成する
func NewMemoryReadTool(userID string) *MemoryReadTool {
	return &MemoryReadTool{
		userID: userID,
	}
}

// Name はツール名を返す
func (t *MemoryReadTool) Name() string {
	return "memory_read"
}

// Description はツールの説明を返す
func (t *MemoryReadTool) Description() string {
	return "永続メモリを読み取ります。"
}

// Execute は永続メモリを読み取る
func (t *MemoryReadTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	// メモリファイルのパス
	memoryPath := filepath.Join("data", "memory", fmt.Sprintf("%s.md", t.userID))

	// ファイルが存在するか確認
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		return map[string]interface{}{
			"content": "",
		}, nil
	}

	// ファイルを読み取り
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory: %w", err)
	}

	return map[string]interface{}{
		"content": string(data),
	}, nil
}

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileWriteTool はファイルに書き込むツール
type FileWriteTool struct {
	workDir string
	userID  string
}

// NewFileWriteTool は新しいFileWriteToolを作成する
func NewFileWriteTool(workDir, userID string) *FileWriteTool {
	return &FileWriteTool{
		workDir: workDir,
		userID:  userID,
	}
}

// Name はツール名を返す
func (t *FileWriteTool) Name() string {
	return "file_write"
}

// Description はツールの説明を返す
func (t *FileWriteTool) Description() string {
	return "ファイルに内容を書き込みます。"
}

// Execute はファイルに書き込む
func (t *FileWriteTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, ok := params["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	content, ok := params["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content parameter is required")
	}

	// パストラバーサル攻撃の防止
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path traversal is not allowed")
	}

	// 絶対パスの拒否
	if strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("absolute path is not allowed")
	}

	// ファイルパスを解決
	fullPath := filepath.Join(t.workDir, t.userID, path)

	// シンボリックリンクによるパストラバーサルを防止
	if err := validateSymlinkPath(t.workDir, t.userID, path); err != nil {
		return nil, err
	}

	// ディレクトリが存在することを確認
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// ファイルに書き込み
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"path":    path,
	}, nil
}

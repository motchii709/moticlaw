package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileReadTool はファイルを読み取るツール
type FileReadTool struct {
	workDir string
	userID  string
}

// NewFileReadTool は新しいFileReadToolを作成する
func NewFileReadTool(workDir, userID string) *FileReadTool {
	return &FileReadTool{
		workDir: workDir,
		userID:  userID,
	}
}

// Name はツール名を返す
func (t *FileReadTool) Name() string {
	return "file_read"
}

// Description はツールの説明を返す
func (t *FileReadTool) Description() string {
	return "ファイルの内容を読み取ります。"
}

// Execute はファイルを読み取る
func (t *FileReadTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path, ok := params["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
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

	// ファイルが存在するか確認
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// ディレクトリは拒否
	if info.IsDir() {
		return nil, fmt.Errorf("cannot read directory: %s", path)
	}

	// ファイルを読み取り
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return map[string]interface{}{
		"content": string(data),
		"size":    info.Size(),
	}, nil
}

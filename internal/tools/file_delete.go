package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileDeleteTool はファイルを削除するツール（.trash/に退避）
type FileDeleteTool struct {
	workDir string
	userID  string
}

// NewFileDeleteTool は新しいFileDeleteToolを作成する
func NewFileDeleteTool(workDir, userID string) *FileDeleteTool {
	return &FileDeleteTool{
		workDir: workDir,
		userID:  userID,
	}
}

// Name はツール名を返す
func (t *FileDeleteTool) Name() string {
	return "file_delete"
}

// Description はツールの説明を返す
func (t *FileDeleteTool) Description() string {
	return "ファイルを削除します（.trash/に退避します）。物理削除は行いません。"
}

// Execute はファイルを削除する（.trash/に退避）
func (t *FileDeleteTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
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

	// ディレクトリは削除不可
	if info.IsDir() {
		return nil, fmt.Errorf("cannot delete directory: %s", path)
	}

	// .trashディレクトリを作成
	trashDir := filepath.Join(t.workDir, t.userID, ".trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create trash directory: %w", err)
	}

	// タイムスタンプ付きのファイル名を生成
	timestamp := time.Now().Format("20060102_150405")
	fileName := filepath.Base(path)
	trashPath := filepath.Join(trashDir, fmt.Sprintf("%s_%s", timestamp, fileName))

	// ファイルを移動
	if err := os.Rename(fullPath, trashPath); err != nil {
		return nil, fmt.Errorf("failed to move file to trash: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"original": path,
		"trashed":  fmt.Sprintf(".trash/%s_%s", timestamp, fileName),
	}, nil
}

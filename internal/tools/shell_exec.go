package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/moti/moticlaw/internal/sandbox"
)

// ShellExecTool はサンドボックス内でコマンドを実行するツール
type ShellExecTool struct {
	sandbox *sandbox.Sandbox
}

// NewShellExecTool は新しいShellExecToolを作成する
func NewShellExecTool(workDir, userID string, queue chan struct{}) *ShellExecTool {
	return &ShellExecTool{
		sandbox: sandbox.New(workDir, userID, queue),
	}
}

// Name はツール名を返す
func (t *ShellExecTool) Name() string {
	return "shell_exec"
}

// Description はツールの説明を返す
func (t *ShellExecTool) Description() string {
	return "サンドボックス内でコマンドを実行します。ネットワークは遮断されています。"
}

// Execute はコマンドを実行する
func (t *ShellExecTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	command, ok := params["command"].(string)
	if !ok {
		return nil, fmt.Errorf("command parameter is required")
	}

	// 30秒のタイムアウトを設定
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// サンドボックス内でコマンドを実行
	output, err := t.sandbox.Exec(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	return map[string]interface{}{
		"output": output,
	}, nil
}

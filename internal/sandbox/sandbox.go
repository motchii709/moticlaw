package sandbox

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Sandbox はbubblewrapラッパー
type Sandbox struct {
	workDir string
	userID  string
	queue   chan struct{} // global semaphore (buffer=1) for serializing sandbox exec
}

// New は新しいサンドボックスを作成する
func New(workDir, userID string, queue chan struct{}) *Sandbox {
	return &Sandbox{
		workDir: workDir,
		userID:  userID,
		queue:   queue,
	}
}

// validateUserID はユーザーIDがパストラバーサル攻撃に使えないことを検証する
func validateUserID(userID string) error {
	if userID == "" {
		return fmt.Errorf("userID must not be empty")
	}
	if strings.Contains(userID, "/") || strings.Contains(userID, "\\") {
		return fmt.Errorf("userID must not contain path separators")
	}
	if userID == ".." {
		return fmt.Errorf("userID must not be '..'")
	}
	return nil
}

// Exec はサンドボックス内でコマンドを実行する
func (s *Sandbox) Exec(ctx context.Context, command string) (string, error) {
	// Validate userID before anything else (security: prevent path traversal)
	if err := validateUserID(s.userID); err != nil {
		return "", fmt.Errorf("invalid userID: %w", err)
	}

	// Reject empty commands
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	// Acquire global queue semaphore (serialize all sandbox execution)
	select {
	case s.queue <- struct{}{}:
		defer func() { <-s.queue }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// ユーザーのworkdirパス
	userWorkDir := filepath.Join(s.workDir, s.userID)

	// bubblewrapのコマンドラインを構築
	bwrapArgs := []string{
		// 全ネームスペースの分離
		"--unshare-all",
		// 全capabilityの剥奪
		"--cap-drop", "ALL",
		// 親プロセス死亡時に終了
		"--die-with-parent",
		// 新しいセッション
		"--new-session",
		// 書き込み可能なディレクトリのバインド
		"--bind", userWorkDir, "/workdir",
		// 作業ディレクトリの設定
		"--chdir", "/workdir",
		// システム領域のread-onlyバインド
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/etc", "/etc",
		// /procのマウント
		"--proc", "/proc",
		// /devのマウント
		"--dev", "/dev",
		// tmpfs
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
		// 環境変数
		"--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin",
		"--setenv", "HOME", "/workdir",
		"--setenv", "LANG", "en_US.UTF-8",
		"--setenv", "TERM", "dumb",
	}

	// 条件付きマウント: /lib64（動的リンカに必要）
	if _, err := os.Stat("/lib64"); err == nil {
		bwrapArgs = append(bwrapArgs, "--ro-bind", "/lib64", "/lib64")
	}

	// 条件付きマウント: /opt
	if _, err := os.Stat("/opt"); err == nil {
		bwrapArgs = append(bwrapArgs, "--ro-bind", "/opt", "/opt")
	}

	// 実行するコマンド (bash -c で pipe/redirect をサポート)
	bwrapArgs = append(bwrapArgs, "--", "bash", "-c", command)

	// フルコマンドラインを構築
	var cmdName string
	var cmdArgs []string

	if commandExists("systemd-run") {
		cmdName = "systemd-run"
		cmdArgs = []string{
			"--user", "--scope",
			"--property=MemoryMax=128M",
			"--property=CPUQuota=50%",
			"--property=TasksMax=32",
			"bwrap",
		}
		cmdArgs = append(cmdArgs, bwrapArgs...)
	} else {
		log.Printf("sandbox: systemd-run not found, falling back to plain bwrap (no cgroup limits)")
		cmdName = "bwrap"
		cmdArgs = bwrapArgs
	}

	// タイムアウト対応のため CommandContext を使用
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)

	// Execute with panic recovery and output size limit
	var output string
	var execErr error

	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("sandbox panicked: %v", r)
				log.Printf("sandbox.Exec panic: %v", r)
			}
		}()

		// Set up output pipe with size limit
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw

		// Start command
		if err := cmd.Start(); err != nil {
			pr.Close()
			pw.Close()
			execErr = fmt.Errorf("failed to start command: %w", err)
			return
		}

		// Read output with 1MB limit in goroutine
		const maxOutputSize = 1 << 20 // 1MB
		type readResult struct {
			data []byte
			err  error
		}
		ch := make(chan readResult, 1)
		go func() {
			defer pw.Close()
			data, err := io.ReadAll(io.LimitReader(pr, maxOutputSize+1))
			ch <- readResult{data: data, err: err}
		}()

		waitErr := cmd.Wait()
		// Close the pipe writer to signal EOF to the reader goroutine.
		// This is necessary because Go's exec.Cmd does not close an io.PipeWriter
		// when the command finishes (it only closes OS pipes).
		pw.Close()
		result := <-ch
		pr.Close()

		outData := result.data
		// Truncate if exceeded limit
		if len(outData) > maxOutputSize {
			outData = append(outData[:maxOutputSize], []byte("\n... [output truncated at 1MB]")...)
			log.Printf("sandbox output truncated at %d bytes", maxOutputSize)
		}

		// Combine errors
		if result.err != nil && result.err != io.EOF {
			execErr = fmt.Errorf("output read error: %w", result.err)
			return
		}
		if waitErr != nil {
			execErr = fmt.Errorf("sandbox execution failed: %w\nOutput: %s", waitErr, string(outData))
			return
		}

		output = string(outData)
	}()

	if execErr != nil {
		return "", execErr
	}
	return output, nil
}

// commandExists はコマンドが利用可能かどうかを確認する
func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

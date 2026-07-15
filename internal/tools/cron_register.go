package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CronRegisterTool はcronジョブを登録するツール
type CronRegisterTool struct {
	userID    string
	channelID string
}

// NewCronRegisterTool は新しいCronRegisterToolを作成する
func NewCronRegisterTool(userID, channelID string) *CronRegisterTool {
	return &CronRegisterTool{
		userID:    userID,
		channelID: channelID,
	}
}

// Name はツール名を返す
func (t *CronRegisterTool) Name() string {
	return "cron_register"
}

// Description はツールの説明を返す
func (t *CronRegisterTool) Description() string {
	return "cronジョブを登録します。通知先は登録時のチャンネル/ユーザーに固定されます。"
}

// CronJob はcronジョブ
type CronJob struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ChannelID string    `json:"channel_id"`
	Interval  string    `json:"interval"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
	LastRunAt time.Time `json:"last_run_at"`
}

// Execute はcronジョブを登録する
func (t *CronRegisterTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	interval, ok := params["interval"].(string)
	if !ok {
		return nil, fmt.Errorf("interval parameter is required")
	}

	command, ok := params["command"].(string)
	if !ok {
		return nil, fmt.Errorf("command parameter is required")
	}

	// 間隔をパース
	duration, err := time.ParseDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval format: %w", err)
	}

	// 最短間隔を強制（5分未満は拒否）
	if duration < 5*time.Minute {
		return nil, fmt.Errorf("minimum interval is 5 minutes")
	}

	// cronジョブを作成
	job := CronJob{
		ID:        fmt.Sprintf("cron_%d", time.Now().UnixNano()),
		UserID:    t.userID,
		ChannelID: t.channelID,
		Interval:  interval,
		Command:   command,
		CreatedAt: time.Now(),
	}

	// cron.jsonに保存
	if err := t.saveCronJob(job); err != nil {
		return nil, fmt.Errorf("failed to save cron job: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"job_id":   job.ID,
		"interval": interval,
	}, nil
}

// saveCronJob はcronジョブを保存する
func (t *CronRegisterTool) saveCronJob(job CronJob) error {
	cronPath := filepath.Join("data", "config", "cron.json")

	// 既存のcronジョブを読み取り
	var jobs []CronJob
	if data, err := os.ReadFile(cronPath); err == nil {
		if err := json.Unmarshal(data, &jobs); err != nil {
			return fmt.Errorf("failed to parse cron.json: %w", err)
		}
	}

	// ジョブを追加
	jobs = append(jobs, job)

	// 一時ファイルに書き込み
	tmpPath := cronPath + ".tmp"
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cron jobs: %w", err)
	}

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cron.json: %w", err)
	}

	// アトミックにリネーム
	if err := os.Rename(tmpPath, cronPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename cron.json: %w", err)
	}

	return nil
}

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PiStatusTool はRaspberry Piの状態を取得するツール
type PiStatusTool struct{}

// NewPiStatusTool は新しいPiStatusToolを作成する
func NewPiStatusTool() *PiStatusTool {
	return &PiStatusTool{}
}

// Name はツール名を返す
func (t *PiStatusTool) Name() string {
	return "pi_status"
}

// Description はツールの説明を返す
func (t *PiStatusTool) Description() string {
	return "Raspberry Piの状態を取得します。CPU温度、メモリ使用量、uptimeを返します。"
}

// Execute はRaspberry Piの状態を取得する
func (t *PiStatusTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	result := make(map[string]interface{})

	// CPU温度を取得
	temp, err := t.getTemperature()
	if err != nil {
		result["temperature_error"] = err.Error()
	} else {
		result["temperature"] = temp
	}

	// メモリ使用量を取得
	mem, err := t.getMemory()
	if err != nil {
		result["memory_error"] = err.Error()
	} else {
		result["memory"] = mem
	}

	// uptimeを取得
	uptime, err := t.getUptime()
	if err != nil {
		result["uptime_error"] = err.Error()
	} else {
		result["uptime"] = uptime
	}

	return result, nil
}

// getTemperature はCPU温度を取得する
func (t *PiStatusTool) getTemperature() (string, error) {
	// 固定コマンドのみ実行（ユーザー入力を渡さない）
	cmd := exec.Command("vcgencmd", "measure_temp")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get temperature: %w", err)
	}

	// 出力をパース
	temp := strings.TrimSpace(string(output))
	temp = strings.TrimPrefix(temp, "temp=")
	temp = strings.TrimSuffix(temp, "'C")

	return temp + "°C", nil
}

// getMemory はメモリ使用量を取得する
func (t *PiStatusTool) getMemory() (string, error) {
	// /proc/meminfoを読み取り
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "", fmt.Errorf("failed to read memory info: %w", err)
	}
	output := string(data)

	// 出力をパース
	lines := strings.Split(output, "\n")
	memInfo := make(map[string]string)

	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			memInfo[key] = value
		}
	}

	// 使用可能なメモリを計算
	total, ok := memInfo["MemTotal"]
	if !ok {
		return "", fmt.Errorf("MemTotal not found")
	}

	available, ok := memInfo["MemAvailable"]
	if !ok {
		return "", fmt.Errorf("MemAvailable not found")
	}

	return fmt.Sprintf("Total: %s, Available: %s", total, available), nil
}

// getUptime はuptimeを取得する
func (t *PiStatusTool) getUptime() (string, error) {
	// /proc/uptimeを読み取り
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", fmt.Errorf("failed to read uptime: %w", err)
	}
	output := string(data)

	// 出力をパース
	parts := strings.Fields(output)
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid uptime format")
	}

	// uptimeを秒数としてパース
	var uptimeSeconds float64
	_, err = fmt.Sscanf(parts[0], "%f", &uptimeSeconds)
	if err != nil {
		return "", fmt.Errorf("failed to parse uptime: %w", err)
	}

	// 日時に変換
	days := int(uptimeSeconds) / 86400
	hours := (int(uptimeSeconds) % 86400) / 3600
	minutes := (int(uptimeSeconds) % 3600) / 60

	return fmt.Sprintf("%d days, %d hours, %d minutes", days, hours, minutes), nil
}

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store は永続化ストア
type Store struct {
	dataDir string
	mu      sync.RWMutex
}

// New は新しいストアを作成する
func New(dataDir string) (*Store, error) {
	// dataディレクトリの存在確認
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("data directory does not exist: %s", dataDir)
	}

	return &Store{
		dataDir: dataDir,
	}, nil
}

// ChannelConfig はチャンネル設定
type ChannelConfig struct {
	Channels []string `json:"channels"`
}

// GetChannels は登録済みチャンネルの一覧を取得する
func (s *Store) GetChannels() (*ChannelConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dataDir, "config", "channels.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ChannelConfig{Channels: []string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read channels config: %w", err)
	}

	var config ChannelConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse channels config: %w", err)
	}

	return &config, nil
}

// AddChannel はチャンネルを追加する
func (s *Store) AddChannel(channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.getChannelsUnsafe()
	if err != nil {
		return err
	}

	// 重複チェック
	for _, ch := range config.Channels {
		if ch == channelID {
			return nil // 既に登録済み
		}
	}

	config.Channels = append(config.Channels, channelID)
	return s.saveChannelsUnsafe(config)
}

// RemoveChannel はチャンネルを削除する
func (s *Store) RemoveChannel(channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.getChannelsUnsafe()
	if err != nil {
		return err
	}

	// チャンネルを検索して削除
	for i, ch := range config.Channels {
		if ch == channelID {
			config.Channels = append(config.Channels[:i], config.Channels[i+1:]...)
			return s.saveChannelsUnsafe(config)
		}
	}

	return nil // 登録されていない
}

// IsChannelRegistered はチャンネルが登録されているか確認する
func (s *Store) IsChannelRegistered(channelID string) bool {
	config, err := s.GetChannels()
	if err != nil {
		return false
	}

	for _, ch := range config.Channels {
		if ch == channelID {
			return true
		}
	}
	return false
}

// UserModelConfig is stored at data/config/users/<user_id>/model.json
type UserModelConfig struct {
	Model string `json:"model"`
}

// SaveUserModel はユーザーのモデル設定を保存する
func (s *Store) SaveUserModel(userID, modelName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dataDir, "config", "users", userID, "model.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	config := UserModelConfig{Model: modelName}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LoadUserModel はユーザーのモデル設定を読み込む
func (s *Store) LoadUserModel(userID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dataDir, "config", "users", userID, "model.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil // no preference stored
	}
	if err != nil {
		return "", err
	}

	var config UserModelConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", err
	}
	return config.Model, nil
}

// UserThinkingShowConfig is stored at data/config/users/<user_id>/thinking.json
type UserThinkingShowConfig struct {
	Enabled bool `json:"enabled"`
}

// LoadThinkingShow はユーザーの思考表示設定を読み込む
func (s *Store) LoadThinkingShow(userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dataDir, "config", "users", userID, "thinking.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var config UserThinkingShowConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return false, err
	}
	return config.Enabled, nil
}

// SaveThinkingShow はユーザーの思考表示設定を保存する
func (s *Store) SaveThinkingShow(userID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dataDir, "config", "users", userID, "thinking.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	config := UserThinkingShowConfig{Enabled: enabled}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// UserSkillConfig is stored at data/config/users/<user_id>/skills.json
type UserSkillConfig struct {
	Skills []string `json:"skills"`
}

// SaveUserSkill はユーザーのSkillを保存する
func (s *Store) SaveUserSkill(userID, skillName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	skills, err := s.loadUserSkillsUnsafe(userID)
	if err != nil {
		return err
	}

	for _, sk := range skills {
		if sk == skillName {
			return nil
		}
	}

	skills = append(skills, skillName)
	return s.saveUserSkillsUnsafe(userID, skills)
}

// LoadUserSkills はユーザーのインストール済みSkill一覧を読み込む
func (s *Store) LoadUserSkills(userID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadUserSkillsUnsafe(userID)
}

// RemoveUserSkill はユーザーのSkillを削除する
func (s *Store) RemoveUserSkill(userID, skillName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	skills, err := s.loadUserSkillsUnsafe(userID)
	if err != nil {
		return err
	}

	for i, sk := range skills {
		if sk == skillName {
			skills = append(skills[:i], skills[i+1:]...)
			return s.saveUserSkillsUnsafe(userID, skills)
		}
	}

	return fmt.Errorf("skill not found: %s", skillName)
}

func (s *Store) loadUserSkillsUnsafe(userID string) ([]string, error) {
	path := filepath.Join(s.dataDir, "config", "users", userID, "skills.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	var config UserSkillConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return config.Skills, nil
}

func (s *Store) saveUserSkillsUnsafe(userID string, skills []string) error {
	path := filepath.Join(s.dataDir, "config", "users", userID, "skills.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	config := UserSkillConfig{Skills: skills}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// HistoryEntry は会話履歴の1メッセージ（永続化用、role+contentのみ）
type HistoryEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// HistoryData は会話履歴のファイル形式
type HistoryData struct {
	UserID      string         `json:"user_id"`
	Messages    []HistoryEntry `json:"messages"`
	LastUpdated time.Time      `json:"last_updated"`
}

// SaveHistory はユーザーの会話履歴を保存する
func (s *Store) SaveHistory(userID string, messages []HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveHistoryUnsafe(userID, messages)
}

// saveHistoryUnsafe はロックなしで会話履歴を保存する
func (s *Store) saveHistoryUnsafe(userID string, messages []HistoryEntry) error {
	dir := filepath.Join(s.dataDir, "history")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create history directory: %w", err)
	}

	path := filepath.Join(dir, userID+".json")
	tmpPath := path + ".tmp"

	data := HistoryData{
		UserID:      userID,
		Messages:    messages,
		LastUpdated: time.Now(),
	}

	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	if err := os.WriteFile(tmpPath, payload, 0644); err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename history: %w", err)
	}

	return nil
}

// LoadHistory はユーザーの会話履歴を読み込む
func (s *Store) LoadHistory(userID string) ([]HistoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.dataDir, "history")
	path := filepath.Join(dir, userID+".json")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []HistoryEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read history: %w", err)
	}

	var history HistoryData
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to parse history: %w", err)
	}

	return history.Messages, nil
}

func (s *Store) getChannelsUnsafe() (*ChannelConfig, error) {
	path := filepath.Join(s.dataDir, "config", "channels.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ChannelConfig{Channels: []string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read channels config: %w", err)
	}

	var config ChannelConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse channels config: %w", err)
	}

	return &config, nil
}

func (s *Store) saveChannelsUnsafe(config *ChannelConfig) error {
	path := filepath.Join(s.dataDir, "config", "channels.json")

	// 一時ファイルに書き込み
	tmpPath := path + ".tmp"
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal channels config: %w", err)
	}

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write channels config: %w", err)
	}

	// アトミックにリネーム
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename channels config: %w", err)
	}

	return nil
}

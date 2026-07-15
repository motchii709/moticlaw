package store

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestStore creates a temporary directory with the required config/ subdirectory
// and returns a Store instance pointing to it, plus a cleanup function.
func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "moticlaw-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create the required config/ subdirectory
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create config dir: %v", err)
	}

	s, err := New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return s, cleanup
}

func TestGetChannels_Empty(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels on empty config returned error: %v", err)
	}

	if config == nil {
		t.Fatal("GetChannels returned nil config")
	}

	if len(config.Channels) != 0 {
		t.Fatalf("expected empty channels list, got %v", config.Channels)
	}
}

func TestAddChannel(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	channelID := "123456789"

	err := s.AddChannel(channelID)
	if err != nil {
		t.Fatalf("AddChannel returned error: %v", err)
	}

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels after AddChannel returned error: %v", err)
	}

	if len(config.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d: %v", len(config.Channels), config.Channels)
	}

	if config.Channels[0] != channelID {
		t.Fatalf("expected channel %q, got %q", channelID, config.Channels[0])
	}
}

func TestAddChannel_Duplicate(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	channelID := "123456789"

	// Add first time
	if err := s.AddChannel(channelID); err != nil {
		t.Fatalf("first AddChannel returned error: %v", err)
	}

	// Add second time — should not error and should not duplicate
	if err := s.AddChannel(channelID); err != nil {
		t.Fatalf("second AddChannel (duplicate) returned error: %v", err)
	}

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels returned error: %v", err)
	}

	if len(config.Channels) != 1 {
		t.Fatalf("expected 1 channel after duplicate add, got %d: %v", len(config.Channels), config.Channels)
	}
}

func TestRemoveChannel(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	channelID := "123456789"

	// Add then remove
	if err := s.AddChannel(channelID); err != nil {
		t.Fatalf("AddChannel returned error: %v", err)
	}

	if err := s.RemoveChannel(channelID); err != nil {
		t.Fatalf("RemoveChannel returned error: %v", err)
	}

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels after RemoveChannel returned error: %v", err)
	}

	if len(config.Channels) != 0 {
		t.Fatalf("expected empty channels list after remove, got %v", config.Channels)
	}
}

func TestRemoveChannel_NonExistent(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	// Removing a channel that was never added should not error
	err := s.RemoveChannel("nonexistent-channel")
	if err != nil {
		t.Fatalf("RemoveChannel on non-existent channel returned error: %v", err)
	}
}

func TestIsChannelRegistered(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	channelID := "123456789"

	// Should not be registered initially
	if s.IsChannelRegistered(channelID) {
		t.Fatal("expected IsChannelRegistered to return false before adding")
	}

	// Add the channel
	if err := s.AddChannel(channelID); err != nil {
		t.Fatalf("AddChannel returned error: %v", err)
	}

	// Should be registered now
	if !s.IsChannelRegistered(channelID) {
		t.Fatal("expected IsChannelRegistered to return true after adding")
	}

	// Remove the channel
	if err := s.RemoveChannel(channelID); err != nil {
		t.Fatalf("RemoveChannel returned error: %v", err)
	}

	// Should not be registered after removal
	if s.IsChannelRegistered(channelID) {
		t.Fatal("expected IsChannelRegistered to return false after removing")
	}
}

func TestIsChannelRegistered_NonExistent(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	// A channel that was never added should return false
	if s.IsChannelRegistered("never-added") {
		t.Fatal("expected IsChannelRegistered to return false for non-existent channel")
	}
}

func TestAddChannel_Multiple(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ids := []string{"111", "222", "333"}

	for _, id := range ids {
		if err := s.AddChannel(id); err != nil {
			t.Fatalf("AddChannel(%q) returned error: %v", id, err)
		}
	}

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels returned error: %v", err)
	}

	if len(config.Channels) != len(ids) {
		t.Fatalf("expected %d channels, got %d: %v", len(ids), len(config.Channels), config.Channels)
	}

	// Verify all IDs are present
	seen := make(map[string]bool)
	for _, ch := range config.Channels {
		seen[ch] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("expected channel %q to be in list, got %v", id, config.Channels)
		}
	}
}

func TestRemoveChannel_Multiple(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ids := []string{"111", "222", "333", "444"}

	for _, id := range ids {
		if err := s.AddChannel(id); err != nil {
			t.Fatalf("AddChannel(%q) returned error: %v", id, err)
		}
	}

	// Remove middle element
	if err := s.RemoveChannel("222"); err != nil {
		t.Fatalf("RemoveChannel returned error: %v", err)
	}

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels returned error: %v", err)
	}

	expected := []string{"111", "333", "444"}
	if len(config.Channels) != len(expected) {
		t.Fatalf("expected %d channels, got %d: %v", len(expected), len(config.Channels), config.Channels)
	}

	for i, ch := range config.Channels {
		if ch != expected[i] {
			t.Fatalf("at index %d: expected %q, got %q", i, expected[i], ch)
		}
	}
}

func TestGetChannels_FileNotFound(t *testing.T) {
	// When channels.json doesn't exist, GetChannels should return empty list, not error
	s, cleanup := setupTestStore(t)
	defer cleanup()

	config, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels when file doesn't exist returned error: %v", err)
	}

	if len(config.Channels) != 0 {
		t.Fatalf("expected empty channels list, got %v", config.Channels)
	}
}

func TestNew_NonExistentDataDir(t *testing.T) {
	_, err := New("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error when data directory does not exist")
	}
}

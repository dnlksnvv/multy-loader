package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileEntry represents a single file in the config
type FileEntry struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	FileName    string `json:"fileName"`
	Folder      string `json:"folder"`      // Relative to root directory
	Title       string `json:"title"`       // Human-readable title
	Description string `json:"description"` // Description with clickable links
	SourceURL   string `json:"sourceUrl"`   // Link to source page (e.g. model page)
	UseToken    bool   `json:"useToken"`    // Whether to append auth token to URL
}

// Config represents a download configuration
type Config struct {
	Name          string      `json:"name"`
	RootDirectory string      `json:"rootDirectory"`
	CivitaiToken  string      `json:"civitaiToken"` // API token for civitai.com
	Files         []FileEntry `json:"files"`
}

// Manager handles config operations
type Manager struct {
	configsDir string
	mu         sync.RWMutex
}

// NewManager creates a new config manager
func NewManager(configsDir string) (*Manager, error) {
	if err := os.MkdirAll(configsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create configs directory: %w", err)
	}
	return &Manager{configsDir: configsDir}, nil
}

// ListConfigs returns all available config names
func (m *Manager) ListConfigs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.configsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read configs directory: %w", err)
	}

	var configs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			name := strings.TrimSuffix(entry.Name(), ".json")
			configs = append(configs, name)
		}
	}
	return configs, nil
}

// LoadConfig loads a config by name
func (m *Manager) LoadConfig(name string) (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := filepath.Join(m.configsDir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config '%s' not found", name)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig saves a config
func (m *Manager) SaveConfig(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.Name == "" {
		return fmt.Errorf("config name cannot be empty")
	}

	// Sanitize name for filename
	safeName := sanitizeFileName(cfg.Name)
	path := filepath.Join(m.configsDir, safeName+".json")

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// DeleteConfig deletes a config by name
func (m *Manager) DeleteConfig(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.configsDir, name+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config '%s' not found", name)
		}
		return fmt.Errorf("failed to delete config: %w", err)
	}
	return nil
}

// GetFoldersInRoot returns all folders within the root directory
func GetFoldersInRoot(rootDir string) ([]string, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("root directory not specified")
	}

	// Expand ~ to home directory
	if strings.HasPrefix(rootDir, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		rootDir = filepath.Join(home, rootDir[1:])
	}

	var folders []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}
		if info.IsDir() && path != rootDir {
			rel, err := filepath.Rel(rootDir, path)
			if err == nil {
				folders = append(folders, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}
	return folders, nil
}

// ExpandPath expands ~ and returns absolute path
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func sanitizeFileName(name string) string {
	// Remove or replace characters that are invalid in filenames
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}


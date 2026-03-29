package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigResolvePaths_UsesOSMWorkspacesOverride(t *testing.T) {
	t.Setenv("OSM_WORKSPACES", "$HOME/override-workspaces")

	cfg := &Config{
		BaseFolder: "/tmp/osmedeus-base",
		Environments: EnvironmentConfig{
			ExternalBinariesPath: "{{base_folder}}/external-binaries",
			ExternalData:         "{{base_folder}}/external-data",
			ExternalConfigs:      "{{base_folder}}/external-configs",
			Workspaces:           "{{base_folder}}/workspaces",
			Workflows:            "{{base_folder}}/workflows",
		},
	}

	cfg.ResolvePaths()

	homeDir, err := os.UserHomeDir()
	assert.NoError(t, err)
	expected := filepath.Clean(filepath.Join(homeDir, "override-workspaces"))
	assert.Equal(t, expected, filepath.Clean(cfg.WorkspacesPath))
	assert.Equal(t, expected, filepath.Clean(cfg.Environments.Workspaces))
}

func TestLoadFromFileResolvesRuntimePaths(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "osm-settings.yaml")
	settings := []byte(`
base_folder: /tmp/osmedeus-base
environments:
  external_data: "{{base_folder}}/external-data"
  external_configs: "{{base_folder}}/external-configs"
  workspaces: "{{base_folder}}/workspaces"
  workflows: "{{base_folder}}/workflows"
  external_scripts: "{{base_folder}}/external-scripts"
database:
  db_path: "{{base_folder}}/database-osm.sqlite"
`)
	require.NoError(t, os.WriteFile(settingsPath, settings, 0o644))

	cfg, err := LoadFromFile(settingsPath)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/osmedeus-base/external-data", filepath.Clean(cfg.DataPath))
	assert.Equal(t, "/tmp/osmedeus-base/external-configs", filepath.Clean(cfg.ConfigsPath))
	assert.Equal(t, "/tmp/osmedeus-base/workspaces", filepath.Clean(cfg.WorkspacesPath))
	assert.Equal(t, "/tmp/osmedeus-base/workflows", filepath.Clean(cfg.WorkflowsPath))
	assert.Equal(t, "/tmp/osmedeus-base/external-scripts", filepath.Clean(cfg.ExternalScriptsPath))
	assert.Equal(t, "/tmp/osmedeus-base/database-osm.sqlite", filepath.Clean(cfg.GetDBPath()))
}

func TestServerConfig_GetServerURL(t *testing.T) {
	tests := []struct {
		name   string
		config ServerConfig
		want   string
	}{
		{
			name: "EventReceiverURL takes precedence",
			config: ServerConfig{
				EventReceiverURL: "http://custom.example.com:9000",
				Host:             "localhost",
				Port:             8002,
			},
			want: "http://custom.example.com:9000",
		},
		{
			name: "EventReceiverURL trailing slash removed",
			config: ServerConfig{
				EventReceiverURL: "http://custom.example.com:9000/",
			},
			want: "http://custom.example.com:9000",
		},
		{
			name: "Computed from Host and Port",
			config: ServerConfig{
				Host: "localhost",
				Port: 8002,
			},
			want: "http://localhost:8002",
		},
		{
			name: "0.0.0.0 converted to 127.0.0.1",
			config: ServerConfig{
				Host: "0.0.0.0",
				Port: 8002,
			},
			want: "http://127.0.0.1:8002",
		},
		{
			name: "Empty when no config",
			config: ServerConfig{
				Host: "",
				Port: 0,
			},
			want: "",
		},
		{
			name: "Empty when only host set",
			config: ServerConfig{
				Host: "localhost",
				Port: 0,
			},
			want: "",
		},
		{
			name: "Empty when only port set",
			config: ServerConfig{
				Host: "",
				Port: 8002,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetServerURL()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestServerConfig_GetEventReceiverURL(t *testing.T) {
	tests := []struct {
		name   string
		config ServerConfig
		want   string
	}{
		{
			name: "EventReceiverURL set",
			config: ServerConfig{
				EventReceiverURL: "http://custom.example.com:9000",
			},
			want: "http://custom.example.com:9000",
		},
		{
			name: "Computed from Host and Port",
			config: ServerConfig{
				Host: "localhost",
				Port: 8002,
			},
			want: "http://localhost:8002",
		},
		{
			name: "0.0.0.0 converted to 127.0.0.1",
			config: ServerConfig{
				Host: "0.0.0.0",
				Port: 8002,
			},
			want: "http://127.0.0.1:8002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetEventReceiverURL()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEmbeddingsConfigFallbackForKnowledgeVector(t *testing.T) {
	cfg, err := LoadFromBytes([]byte(`
base_folder: /tmp/osmedeus-base
embeddings_config:
  enabled: true
  provider: jina
  jina:
    api_url: https://api.jina.ai/v1/embeddings
    model: jina-embeddings-v5-text-small
    api_key: test-key
knowledge_vector:
  enabled: true
  db_path: "{{base_folder}}/knowledge/vector-kb.sqlite"
`))
	require.NoError(t, err)

	assert.True(t, cfg.IsEmbeddingsConfigEnabled())
	assert.Equal(t, "jina", cfg.GetKnowledgeVectorProvider())
	assert.Equal(t, "jina-embeddings-v5-text-small", cfg.GetKnowledgeVectorModel(""))

	provider, source, err := cfg.ResolveEmbeddingProvider("jina")
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Equal(t, "embeddings_config", source)
	assert.Equal(t, "https://api.jina.ai/v1/embeddings", provider.BaseURL)
	assert.Equal(t, "jina-embeddings-v5-text-small", provider.Model)
	assert.Equal(t, "test-key", provider.AuthToken)
	assert.Contains(t, cfg.ListEmbeddingProviders(), "jina")
	assert.Contains(t, cfg.ListEmbeddingProviderModels(), "jina:jina-embeddings-v5-text-small")
}

func TestResolveEmbeddingProviderPrefersEmbeddingsConfigOverLLMProvider(t *testing.T) {
	cfg := &Config{
		BaseFolder: "/tmp/osmedeus-base",
		Embeddings: EmbeddingsConfig{
			Enabled:  boolPtr(true),
			Provider: "openai",
			OpenAI: EmbeddingsProviderConfig{
				APIURL: "https://embed.example/v1/embeddings",
				Model:  "text-embedding-3-small",
				APIKey: "embed-key",
			},
		},
		LLM: LLMConfig{
			LLMProviders: []LLMProvider{{
				Provider:  "openai",
				BaseURL:   "https://chat.example/v1/chat/completions",
				AuthToken: "chat-key",
				Model:     "gpt-5.4",
			}},
		},
	}

	provider, source, err := cfg.ResolveEmbeddingProvider("openai")
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Equal(t, "embeddings_config", source)
	assert.Equal(t, "https://embed.example/v1/embeddings", provider.BaseURL)
	assert.Equal(t, "text-embedding-3-small", provider.Model)
	assert.Equal(t, "embed-key", provider.AuthToken)
}

func TestLLMRetrySettingsParse(t *testing.T) {
	cfg, err := LoadFromBytes([]byte(`
base_folder: /tmp/osmedeus-base
llm_config:
  max_retries: 5
  retry_delay: 10s
  retry_backoff: true
  request_delay: 2s
`))
	require.NoError(t, err)

	assert.Equal(t, 5, cfg.LLM.MaxRetries)
	assert.Equal(t, "10s", cfg.LLM.RetryDelay)
	assert.True(t, cfg.LLM.RetryBackoff)
	assert.Equal(t, "2s", cfg.LLM.RequestDelay)
}

func boolPtr(value bool) *bool {
	return &value
}

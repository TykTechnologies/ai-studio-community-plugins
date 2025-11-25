package main

import (
	_ "embed"
	"fmt"
	"mime"
	"path/filepath"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/git"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/ingestion"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/rpc"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/secrets"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

//go:embed plugin.manifest.json
var manifestJSON []byte

//go:embed config.schema.json
var configSchemaJSON []byte

//go:embed ui/dist/bundle.js
var uiBundleJS []byte

//go:embed assets/github-icon.svg
var githubIconSVG []byte

// GitHubRAGPlugin implements the GitHub RAG ingestion plugin
type GitHubRAGPlugin struct {
	plugin_sdk.BasePlugin
	pluginID   uint32
	rpcHandler *rpc.Handler
}

// NewGitHubRAGPlugin creates a new instance of the plugin
func NewGitHubRAGPlugin() *GitHubRAGPlugin {
	return &GitHubRAGPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(
			"com.tyk.github-rag-ingest",
			"1.0.0",
			"GitHub RAG Ingestion Plugin - Ingest content from GitHub repositories into RAG datasources",
		),
	}
}

// Initialize sets up the plugin with broker ID and configuration
func (p *GitHubRAGPlugin) Initialize(ctx plugin_sdk.Context, config map[string]string) error {
	// Extract broker ID for Service API access
	brokerIDStr := ""
	if id, ok := config["_service_broker_id"]; ok {
		brokerIDStr = id
	} else if id, ok := config["service_broker_id"]; ok {
		brokerIDStr = id
	}

	if brokerIDStr != "" {
		var brokerID uint32
		fmt.Sscanf(brokerIDStr, "%d", &brokerID)
		ai_studio_sdk.SetServiceBrokerID(brokerID)
	}

	// Extract plugin ID
	if pluginIDStr, ok := config["plugin_id"]; ok {
		fmt.Sscanf(pluginIDStr, "%d", &p.pluginID)
		ai_studio_sdk.SetPluginID(p.pluginID)
	}

	// Initialize storage layer
	kv := storage.NewKVStore(ctx.Services.KV())
	repoStore := storage.NewRepositoryStore(kv)
	jobStore := storage.NewJobStore(kv)
	secretStore := storage.NewSecretStore(kv)

	// Initialize secrets backend (defaults to KV)
	secretsBackend := config["secrets_backend"]
	var backend secrets.Backend
	if secretsBackend == "vault" {
		vaultConfig := &secrets.Config{
			Backend:         "vault",
			VaultAddress:    config["vault_address"],
			VaultToken:      config["vault_token"],
			VaultMountPath:  config["vault_mount_path"],
			VaultSecretPath: config["vault_secret_path"],
		}
		vaultBackend, err := secrets.NewVaultBackend(vaultConfig)
		if err != nil {
			ctx.Services.Logger().Warn("Failed to initialize Vault backend, using KV", "error", err)
			backend = secrets.NewKVBackend(secretStore)
		} else {
			backend = vaultBackend
		}
	} else {
		backend = secrets.NewKVBackend(secretStore)
	}

	// Initialize git client
	cacheDir := config["cache_path"]
	if cacheDir == "" {
		cacheDir = "/tmp/github-rag-cache"
	}
	gitClient, err := git.NewClient(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to initialize git client: %w", err)
	}

	// Initialize ingestion engine
	engine := ingestion.NewEngine(gitClient, secretStore, jobStore, repoStore)

	// Initialize RPC handler
	p.rpcHandler = rpc.NewHandler(repoStore, jobStore, secretStore, gitClient, backend, engine)

	ctx.Services.Logger().Info("GitHub RAG Ingestion Plugin initialized",
		"plugin_id", p.pluginID,
		"broker_id", brokerIDStr,
		"secrets_backend", secretsBackend,
		"cache_dir", cacheDir)

	return nil
}

// GetManifest returns the plugin manifest
func (p *GitHubRAGPlugin) GetManifest() ([]byte, error) {
	return manifestJSON, nil
}

// GetConfigSchema returns the JSON Schema for plugin configuration
func (p *GitHubRAGPlugin) GetConfigSchema() ([]byte, error) {
	return configSchemaJSON, nil
}

// GetAsset serves plugin assets (JS bundles, icons, etc.)
func (p *GitHubRAGPlugin) GetAsset(assetPath string) ([]byte, string, error) {
	// Determine MIME type from extension
	mimeType := mime.TypeByExtension(filepath.Ext(assetPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Serve embedded assets
	switch assetPath {
	case "/ui/dist/bundle.js", "ui/dist/bundle.js":
		return uiBundleJS, "application/javascript", nil
	case "/assets/github-icon.svg", "assets/github-icon.svg":
		return githubIconSVG, "image/svg+xml", nil
	default:
		return nil, "", fmt.Errorf("asset not found: %s", assetPath)
	}
}

// ListAssets returns list of available assets
func (p *GitHubRAGPlugin) ListAssets(pathPrefix string) ([]*pb.AssetInfo, error) {
	assets := []*pb.AssetInfo{
		{Path: "/ui/dist/bundle.js", Size: int64(len(uiBundleJS))},
		{Path: "/assets/github-icon.svg", Size: int64(len(githubIconSVG))},
	}
	return assets, nil
}

// HandleRPC processes RPC calls from the UI
func (p *GitHubRAGPlugin) HandleRPC(method string, payload []byte) ([]byte, error) {
	// Extract broker ID from payload if present
	brokerID := ai_studio_sdk.ExtractBrokerIDFromPayload(payload)
	if brokerID != 0 {
		ai_studio_sdk.SetServiceBrokerID(brokerID)
	}

	// Delegate to RPC handler
	if p.rpcHandler == nil {
		return nil, fmt.Errorf("RPC handler not initialized")
	}

	switch method {
	case "list_repositories":
		return p.rpcHandler.ListRepositories(payload)
	case "get_repository":
		return p.rpcHandler.GetRepository(payload)
	case "create_repository":
		return p.rpcHandler.CreateRepository(payload)
	case "update_repository":
		return p.rpcHandler.UpdateRepository(payload)
	case "delete_repository":
		return p.rpcHandler.DeleteRepository(payload)
	case "trigger_ingestion":
		return p.rpcHandler.TriggerIngestion(payload)
	case "list_jobs":
		return p.rpcHandler.ListJobs(payload)
	case "get_job":
		return p.rpcHandler.GetJob(payload)
	case "get_job_logs":
		return p.rpcHandler.GetJobLogs(payload)
	case "cancel_job":
		return p.rpcHandler.CancelJob(payload)
	case "list_datasources":
		return p.rpcHandler.ListDatasources(payload)
	case "clone_datasource":
		return p.rpcHandler.CloneDatasource(payload)
	case "update_datasource_fields":
		return p.rpcHandler.UpdateDatasourceFields(payload)
	case "get_statistics":
		return p.rpcHandler.GetStatistics(payload)
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}
}

// ExecuteScheduledTask implements the SchedulerPlugin capability
func (p *GitHubRAGPlugin) ExecuteScheduledTask(ctx plugin_sdk.Context, schedule *plugin_sdk.Schedule) error {
	logger := ctx.Services.Logger()
	logger.Info("Executing scheduled sync task",
		"schedule_id", schedule.ID,
		"schedule_name", schedule.Name)

	// Extract repo ID from schedule config
	repoIDRaw, ok := schedule.Config["repo_id"]
	if !ok {
		return fmt.Errorf("schedule missing repo_id in config")
	}

	repoID, ok := repoIDRaw.(string)
	if !ok {
		return fmt.Errorf("repo_id in schedule config is not a string")
	}

	// Load repository
	repo, err := p.rpcHandler.GetRepoStore().Get(ctx.Context, repoID)
	if err != nil {
		return fmt.Errorf("failed to load repository: %w", err)
	}

	// Create and run incremental sync job
	job := types.NewJob(repo.ID, types.JobTypeIncremental, types.TriggerSchedule, false)
	if err := p.rpcHandler.GetJobStore().Create(ctx.Context, job); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	// Run ingestion
	if err := p.rpcHandler.GetEngine().Run(ctx.Context, job); err != nil {
		logger.Error("Scheduled ingestion failed",
			"repo_id", repoID,
			"job_id", job.ID,
			"error", err)
		return err
	}

	logger.Info("Scheduled sync completed successfully",
		"repo_id", repoID,
		"job_id", job.ID,
		"chunks_written", job.Stats.ChunksWritten)

	return nil
}

// Shutdown cleans up plugin resources
func (p *GitHubRAGPlugin) Shutdown(ctx plugin_sdk.Context) error {
	ctx.Services.Logger().Info("GitHub RAG Ingestion Plugin shutting down")
	return nil
}

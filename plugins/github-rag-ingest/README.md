# GitHub RAG Ingestion Plugin

A comprehensive plugin for Tyk AI Studio that ingests content from GitHub repositories into RAG datasources with support for incremental sync, multiple chunking strategies, and scheduled updates.

## Features

- **Multiple Repository Support**: Manage multiple Git repositories with different configurations
- **Authentication**: PAT (Personal Access Token) and SSH key support
- **Incremental Sync**: Efficient diff-based updates to only process changed files
- **Chunking Strategies**:
  - Simple text chunking with configurable overlap
  - Code-aware chunking using regex patterns (Go, Python, JS/TS)
  - Hybrid strategy that selects appropriate chunking based on file type
  - Markdown heading-aware chunking for documentation
- **Scheduled Ingestion**: Cron-based automatic sync with configurable schedules
- **Job Tracking**: Detailed job history with statistics and logs
- **Secrets Management**: Dual-mode (KV storage or HashiCorp Vault)
- **Rich Metadata**: Each chunk includes repository info, file paths, line numbers, GitHub URLs

## Installation

### Building the Plugin

```bash
cd community/plugins/github-rag-ingest/server
go build -o github-rag-ingest
```

### Adding to AI Studio

1. Copy the built binary to your plugins directory
2. Register the plugin in AI Studio:
   ```bash
   # Using file:// protocol for local development
   file:///path/to/github-rag-ingest
   ```

## Architecture

### Plugin Capabilities
- **UIProvider**: Serves dashboard UI for repository and job management
- **SchedulerPlugin**: Executes scheduled sync tasks via cron
- **Service API**: Uses AI Studio APIs for RAG operations and datasource management

### Data Storage
- **KV Storage**: Repository configs, job state, secrets (with Vault option)
- **Vector Store**: Chunks with rich metadata in configured datasources

### Key Components
```
server/
├── types/          # Data models (Repository, Job, Chunk)
├── storage/        # KV storage abstraction and stores
├── secrets/        # Secret backends (KV, Vault)
├── git/            # Git operations (clone, fetch, diff, auth)
├── chunking/       # Chunking strategies
├── ingestion/      # Ingestion pipeline and filters
├── rpc/            # RPC handlers for UI communication
└── ui/             # Lit-based WebComponents
```

## Configuration

### Repository Configuration
Each repository can be configured with:
- Repository URL and branch
- Authentication method (PAT, SSH)
- Target paths and file masks
- Ignore patterns (.gitignore, .ragignore, custom)
- Chunking strategy and parameters
- Sync schedule (cron expression)
- Assigned datasource

### Plugin Config Schema
```json
{
  "secrets_backend": "kv|vault",
  "vault_address": "https://vault.example.com:8200",
  "vault_token": "hvs.xxx",
  "vault_mount_path": "secret",
  "vault_secret_path": "github-rag"
}
```

## Usage

### Adding a Repository
1. Navigate to GitHub RAG → Repositories in AI Studio
2. Click "Add Repository"
3. Configure repository URL, authentication, and chunking settings
4. Select or create a datasource
5. Optionally enable scheduled sync

### Manual Ingestion
- Click "Run Ingestion" on any repository
- Choose full or incremental sync
- Use dry-run mode to preview changes

### Viewing Jobs
- Navigate to GitHub RAG → Jobs
- View job history with statistics
- Inspect detailed logs for each job

## Building the Plugin

### Prerequisites
- Go 1.24.4+
- Node.js 18+ and npm
- Git

### Build Steps

**Step 1: Build UI Components**
```bash
cd server/ui
npm install
npm run build
```

This creates `ui/dist/bundle.js` (29.4kb) which gets embedded into the Go binary.

**Step 2: Build Go Binary**
```bash
cd server
go mod tidy
go build -o github-rag-ingest
```

This creates a 28MB binary with all dependencies and embedded UI.

**Quick Build (One Command)**
```bash
cd server && cd ui && npm install && npm run build && cd .. && go build -o github-rag-ingest
```

### Installing in AI Studio

Use the `file://` protocol to register the plugin:

```bash
file:///absolute/path/to/community/plugins/github-rag-ingest/server/github-rag-ingest
```

### Development Workflow

For UI changes:
```bash
cd server/ui
npm run watch  # Auto-rebuild on changes
```

After UI changes, rebuild the Go binary to embed the new bundle:
```bash
cd server
go build -o github-rag-ingest
```

## License

Part of Tyk AI Studio - see main project for license details.

## Contributing

This plugin is part of the Tyk AI Studio marketplace. Contributions welcome!

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

## User Interface

The plugin provides a comprehensive web UI accessible via the AI Studio sidebar under "GitHub RAG".

### Dashboard

The main dashboard displays:
- **Total Repositories**: Count of configured repositories
- **Active Repositories**: Repositories with sync enabled
- **Total Jobs**: All ingestion jobs (success/failed/running)
- **Chunks Ingested**: Total number of chunks stored across all repositories

Quick actions:
- Add Repository button
- Navigation to Repositories and Jobs views

---

### Repository Management

#### Repository List View

Displays all configured repositories in a table with columns:
- **Repository**: Owner/name (e.g., "TykTechnologies/tyk-docs")
- **Branch**: The branch being monitored
- **Datasource**: The RAG datasource where chunks are stored
- **Status**: Active (sync enabled) or Inactive
- **Last Sync**: Timestamp of last successful sync

**Actions per repository:**
- **Sync**: Run incremental sync (only changed files)
- **Full Sync**: Re-process all files
- **Edit**: Modify repository configuration
- **Delete**: Remove repository and optionally delete all ingested chunks

#### Add/Edit Repository Form

**Basic Configuration**
- **Repository Name** (*required*): Display name for the repository
- **Repository Owner** (*required*): GitHub username or organization (e.g., "TykTechnologies")
- **Repository URL** (*required*): Full GitHub repository URL
  - Example: `https://github.com/TykTechnologies/tyk-docs`
- **Branch** (*required*): Branch to monitor (default: "main")

**Authentication**
- **Authentication Type**: Select from dropdown
  - `public`: No authentication (for public repositories)
  - `pat`: Personal Access Token
  - `ssh`: SSH private key

When `pat` selected:
- **Personal Access Token** (*required*): GitHub PAT with repo access

When `ssh` selected:
- **SSH Private Key** (*required*): OpenSSH format private key
- **SSH Passphrase**: Optional passphrase if key is encrypted

**Datasource Configuration**
- **Datasource** (*required*): Select RAG datasource from dropdown
  - Format: "Name - VectorStoreType" (e.g., "Production RAG - chromadb")
  - **Clone Button**: Quick-clone datasource with different namespace (see Datasource Cloning below)

**File Filtering**
- **Target Paths**: Comma-separated paths to include
  - Example: `src/, docs/`
  - Leave empty to process entire repository
- **File Masks**: Comma-separated glob patterns
  - Example: `*.md, *.mdx, *.go`
  - Simple patterns like `*.md` auto-expand to `**/*.md` to match subdirectories
- **Ignore Patterns**: Comma-separated patterns to exclude
  - Example: `node_modules/, *.min.js, dist/`
- **Use .gitignore**: Checkbox to respect repository's `.gitignore` file
- **Use .ragignore**: Checkbox to respect custom `.ragignore` file for RAG-specific exclusions
- **Max File Size (KB)**: Skip files larger than this size (default: 1024KB)

**Chunking Configuration**
- **Chunking Strategy**: Select chunking method
  - `simple`: Fixed-size chunks with overlap (best for plain text)
  - `code_aware`: Function/class-based chunking for code files (Go, Python, JS/TS)
  - `hybrid`: Automatically select strategy based on file type (recommended)
- **Chunk Size**: Target chunk size in characters (default: 1000)
- **Chunk Overlap**: Overlap between chunks in characters (default: 200)

**Sync Schedule**
- **Sync Schedule**: Cron expression for automatic sync
  - Example: `0 2 * * *` (2 AM daily)
  - Example: `0 */6 * * *` (every 6 hours)
  - Leave empty for manual sync only
- **Enable automatic sync**: Checkbox to activate scheduled sync

**Actions:**
- **Save Repository**: Create or update repository configuration
- **Cancel**: Return to repository list without saving

---

### Datasource Cloning

When configuring a repository, you can quickly create a new datasource for a different namespace:

1. **Select existing datasource** from the dropdown
2. **Click "Clone" button** next to the dropdown
3. **Modal opens** with two fields:
   - **Datasource Name**: Pre-filled with "Copy of [original name]" - edit as needed
   - **Namespace / Collection Name**: The vector store collection/namespace (db_name field) - edit to create new collection
4. **Click "Create Datasource"**
   - Server-side clone creates datasource with all configuration including API keys
   - Only name and namespace are customized
   - Dropdown automatically refreshes and selects the new datasource
5. **Continue** with repository configuration

This feature eliminates the need to manually copy datasource configuration when creating multiple namespaces on the same vector database.

---

### Job Management

#### Job List View

Displays job history with columns:
- **Job ID**: First 8 characters of UUID
- **Repository**: Repository name
- **Type**: `full` or `incremental`
- **Status**: `queued`, `running`, `success`, `failed`, or `partial`
- **Started**: Start timestamp
- **Completed**: Completion timestamp (if finished)
- **Stats**: Quick stats summary (files scanned, chunks written, errors)

**Actions:**
- **View Logs**: Opens detailed job view with execution logs

**Filtering:**
- Filter by repository (dropdown)
- Filter by status (dropdown)

#### Job Detail View

Shows comprehensive job information:

**Job Summary:**
- Repository ID
- Job type (full/incremental)
- Status
- Started and completed timestamps

**Statistics Grid:**
- Files Scanned
- Files Added
- Files Skipped
- Chunks Written
- Errors

**Execution Logs:**
- Real-time log viewer with color-coded entries
- **Filter by level**: All, Info, Warn, Error, Debug
- Each log entry shows:
  - Timestamp
  - Log level (color-coded)
  - Message
  - Additional details (if available)

**Log Level Colors:**
- Info: Blue border
- Warn: Orange border, light orange background
- Error: Red border, light red background
- Debug: Gray border

**Actions:**
- **Back to Jobs**: Return to job list

---

## Usage Workflows

### Initial Repository Setup

1. **Navigate** to GitHub RAG → Repositories
2. **Click** "Add Repository"
3. **Fill in** repository details (owner, name, URL, branch)
4. **Select authentication**:
   - For public repos: select "public"
   - For private repos: select "pat" or "ssh" and provide credentials
5. **Configure datasource**:
   - Select existing datasource, OR
   - Click "Clone" to create new namespace
6. **Set file filtering** (optional):
   - Target specific paths: `docs/, src/`
   - File masks: `*.md, *.mdx`
   - Enable .gitignore/.ragignore
7. **Choose chunking strategy**:
   - Use "hybrid" for automatic selection (recommended)
   - Or select "simple" for plain text, "code_aware" for code files
8. **Configure sync schedule** (optional):
   - Enter cron expression: `0 2 * * *`
   - Enable automatic sync checkbox
9. **Click** "Save Repository"

### Running Manual Sync

1. **Navigate** to GitHub RAG → Repositories
2. **Locate** your repository in the table
3. **Click action button**:
   - **Sync**: Incremental sync (only changed files since last sync)
   - **Full Sync**: Re-process all files
4. **Monitor** job progress in Jobs view

### Monitoring Jobs

1. **Navigate** to GitHub RAG → Jobs
2. **View** job history with status and statistics
3. **Click** "View Logs" on any job to see:
   - Detailed statistics
   - Execution logs with timestamps
   - Errors and warnings
4. **Filter** logs by level (Info/Warn/Error/Debug)

### Updating Repository Configuration

1. **Navigate** to GitHub RAG → Repositories
2. **Click** "Edit" on the repository
3. **Modify** fields as needed
   - Note: Datasource pre-selected on edit
   - All configuration preserved
4. **Click** "Save Repository"
5. **Changes apply** to next sync (scheduled or manual)

### Deleting a Repository

1. **Navigate** to GitHub RAG → Repositories
2. **Click** "Delete" on the repository
3. **Confirm** deletion
4. **Choose**: Delete repository configuration only, or also delete all ingested chunks from the vector store

---

## Field Reference

### Repository Fields

| Field | Required | Type | Description | Example |
|-------|----------|------|-------------|---------|
| name | Yes | String | Display name for repository | "Tyk Docs" |
| owner | Yes | String | GitHub username or org | "TykTechnologies" |
| url | Yes | String | Full repository URL | `https://github.com/TykTechnologies/tyk-docs` |
| branch | Yes | String | Branch to monitor | "main" |
| auth_type | Yes | Enum | Authentication method | "public", "pat", "ssh" |
| pat_token | Conditional | String | GitHub PAT (if auth_type=pat) | "ghp_xxxx" |
| ssh_private_key | Conditional | Text | SSH key (if auth_type=ssh) | "-----BEGIN OPENSSH PRIVATE KEY-----" |
| ssh_passphrase | No | String | SSH key passphrase | "" |
| datasource_id | Yes | Integer | Target RAG datasource ID | 42 |
| target_paths | No | Array | Paths to include | ["src/", "docs/"] |
| file_masks | No | Array | File glob patterns | ["*.md", "*.go"] |
| ignore_patterns | No | Array | Exclusion patterns | ["node_modules/", "*.min.js"] |
| use_gitignore | No | Boolean | Respect .gitignore | true |
| use_ragignore | No | Boolean | Respect .ragignore | true |
| max_file_size_kb | No | Integer | Max file size | 1024 |
| chunking_strategy | Yes | Enum | Chunking method | "simple", "code_aware", "hybrid" |
| chunk_size | Yes | Integer | Target chunk size (chars) | 1000 |
| chunk_overlap | Yes | Integer | Overlap size (chars) | 200 |
| sync_schedule | No | String | Cron expression | "0 2 * * *" |
| sync_enabled | No | Boolean | Enable scheduled sync | true |

### Chunk Metadata Fields

Each chunk stored in the vector database includes:

**AI Studio Standard Fields** (for chat compatibility):
- `filename`: File name (e.g., "service.go")
- `file_name`: Alias for compatibility
- `title`: Extracted heading or filename
- `text`: Full chunk content
- `start`: Character offset in file
- `end`: Character offset in file

**GitHub-Specific Fields**:
- `source`: Always "github-rag-ingest"
- `repo_id`: Repository UUID
- `repo_name`: Repository name
- `repo_owner`: Repository owner
- `repo_host`: "github.com"
- `branch`: Branch name
- `commit_sha`: Commit SHA at time of ingestion
- `file_path`: Relative path in repository
- `file_type`: Detected language (go, python, javascript, markdown, etc.)
- `chunk_index`: Position in file (0-indexed)
- `total_chunks`: Total chunks for this file
- `line_start`: Starting line number
- `line_end`: Ending line number
- `github_url`: Direct link to code in GitHub (with line numbers)
- `ingestion_timestamp`: ISO 8601 timestamp
- `namespace`: Repository namespace

---

## Chunking Strategies

### Simple Chunking
- Fixed-size chunks with configurable overlap
- Best for: Plain text, documentation, general content
- Splits on whitespace/newlines when possible
- Configurable chunk size and overlap

### Code-Aware Chunking
- Extracts functions, classes, and methods using regex patterns
- Supported languages:
  - **Go**: Functions (with and without receivers)
  - **Python**: Functions and classes
  - **JavaScript/TypeScript**: Functions, arrow functions, classes
- Best for: Source code files
- Preserves logical code boundaries

### Hybrid Chunking (Recommended)
- Automatically selects strategy based on file type:
  - Code files → Code-aware chunking
  - Markdown files → Heading-based chunking
  - Config files → Simple chunking with smaller chunks
  - Other files → Simple chunking
- Best for: Mixed repositories with code and documentation

---

## File Filtering

### Priority Order
1. **Binary detection**: Binary files are always skipped
2. **Size check**: Files exceeding max_file_size_kb are skipped
3. **.gitignore**: Applied if use_gitignore is enabled
4. **.ragignore**: Applied if use_ragignore is enabled
5. **Ignore patterns**: Custom patterns are checked
6. **Target paths**: If specified, file must be under one of these paths
7. **File masks**: If specified, file must match at least one pattern

### Pattern Syntax
- Supports doublestar glob patterns
- `**/*.md` - All markdown files recursively
- `*.md` - Auto-expands to `**/*.md`
- `src/**/*.go` - All Go files in src/ directory
- `!test` - Negation (not directly supported, use ignore_patterns)

---

## Incremental Sync

### How It Works
1. Plugin fetches latest commit from configured branch
2. If `last_sync_commit` exists:
   - Compute git diff between last_sync_commit and latest commit
   - Process only added/modified files
   - Delete chunks for deleted/renamed files via metadata query
3. If no previous sync:
   - Perform full ingestion of all matching files
4. Update `last_sync_commit` on success

### Benefits
- **Faster**: Only processes changed files
- **Efficient**: Reduces embedding API costs
- **Consistent**: Automatically handles file deletions and renames

---

## Scheduled Sync

### Configuration
- Uses standard cron expression syntax
- Timezone: UTC (configurable in AI Studio schedule settings)
- Timeout: 300 seconds default

### Common Cron Patterns
| Expression | Description |
|------------|-------------|
| `0 2 * * *` | Daily at 2 AM UTC |
| `0 */6 * * *` | Every 6 hours |
| `0 0 * * 0` | Weekly on Sunday midnight |
| `*/30 * * * *` | Every 30 minutes |
| `0 0 1 * *` | Monthly on 1st at midnight |

### Behavior
- Runs incremental sync by default
- Creates job entry for each execution
- Logs accessible via Jobs view
- Continues on next schedule even if previous run failed

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

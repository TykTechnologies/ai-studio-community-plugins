# Changelog

## [1.1.1] - 2026-10-14

### Added
- The marketplace entry now lists every service scope the plugin uses (adds `kv.readwrite`, `datasources.embeddings` and `scheduler.manage`); AI Studio 2.3.0 enforces approved scopes, so approve them when upgrading
- `vault_token` and `secret_encryption_key` accept `$SECRET/NAME` (resolved by AI Studio 2.3.0) or `$ENV/NAME` references, and are masked in the configuration form
- `known_hosts_path` for SSH repositories

### Fixed
- `CancelJob` stops the running ingestion, and a queued job cancelled before it starts never runs
- The clone cache is private: by default under the user cache directory (mode 0700), and a configured `cache_path` must not be world-writable, a symlink, owned by another user, or under a directory owned by another user
- Vault secret IDs are random, not derived from names
- Plugin storage errors are reported instead of being treated as missing data

### Changed
- Requires AI Studio 2.3.0 (secret references, `PLUGIN_ENV_PASSTHROUGH`). Delete the old shared `/tmp/github-rag-cache` after upgrading

## [1.1.0] - 2026-09-02

### Security
- Enable SSH host-key verification in the git client
- Validate Git clone URLs to prevent SSRF
- Encrypt plugin secrets at rest in KV storage

### Fixed
- Stop the simple chunker splitting multi-byte UTF-8 characters
- Align the plugin module with the parent module's Go version

### Changed
- Minimum AI Studio version is now 2.1

## [1.0.2] - 2026-04-01

### Fixed
- Fixed simple chunking to not break UTF-8 strings

## [1.0.1] - 2026-04-01

### Fixed
- Handles UTF-Encoded strings properly for processing

## [1.0.0] - 2025-12-04

### Added
- Initial release of GitHub RAG Ingestion plugin
- Multiple repository support with authentication (PAT, SSH)
- Incremental sync with git diff-based updates
- Chunking strategies: simple, code-aware, hybrid
- Scheduled ingestion via cron expressions
- Job tracking with detailed statistics and logs
- Secrets management (KV storage or HashiCorp Vault)
- Rich metadata for each chunk (repo info, file paths, line numbers, GitHub URLs)
- Web UI for repository and job management
- Datasource cloning feature

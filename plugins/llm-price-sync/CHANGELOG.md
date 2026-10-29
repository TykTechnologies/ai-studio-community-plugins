# Changelog

## [1.1.1] - 2026-10-14

### Fixed
- The prices URL must be http(s) and is refused when it, a redirect or the proxy path resolves to an internal address (including CGNAT `100.64.0.0/10`)
- Concurrent syncs no longer lose price-history entries
- A sync stops when it is cancelled, and a completed sync is still saved if the request ends
- Storage read errors are reported instead of being treated as an empty history

### Changed
- Requires AI Studio 2.3.0

## [1.1.0] - 2026-09-02

### Fixed
- Align the plugin module with the parent module's Go version

### Changed
- Minimum AI Studio version is now 2.1

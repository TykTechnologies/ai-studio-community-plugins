# Changelog

Source of the entry for the marketplace listing: `make plugin-publish` carries
the previous version's CHANGELOG.md forward, so copy the new entry into the
marketplace repo when publishing. Newest first.

## [1.3.1] - unreleased

Requires AI Studio 2.3.0.

### Fixed
- **`X-Cache-Status: MISS` (and `BYPASS`) reach streamed and bridged
  responses.** The status of a request that went upstream was only set on a
  buffered response's body, so streams, and `/ai` and `/v1` calls to
  non-Bedrock LLMs (whose inner call streams), never carried it. It is now
  also set with the response headers. `X-Cache-Key` on a miss is still only
  on responses buffered end to end, where the answer is stored.

## [1.3.0] - 2026-10-23

Requires AI Studio 2.3.0. Built on the 2.3 Plugin SDK (64 MiB plugin message
limit).

### Fixed
- **Bedrock.** AWS errors (`X-Amzn-ErrorType`, `{"message": ...}` and
  `__type` bodies) are never cached; before, they were stored and served as
  answers. Tokens saved are counted from Converse's camelCase usage.
  `converse-stream` and `invoke-with-response-stream` requests go upstream
  uncached.
- Tokens saved are read with the Plugin SDK's `ResponseUsage`, so Google
  `usageMetadata` counts too.
- A hit no longer replays the headers other plugins set about the original
  request (`X-Tyk-Compression*`, `X-RateLimit-*`, `X-LB-*`, `Server-Timing`).
- A streamed response whose status is not 2xx is never cached.
- Every completed stream releases its pending entry, including when
  `cache_streaming_responses` is off; before, those entries stayed until the
  five-minute cleanup.

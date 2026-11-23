# Changelog

Source of the entry for the marketplace listing: `make plugin-publish` carries
the previous version's CHANGELOG.md forward, so copy the new entry into the
marketplace repo when publishing. Newest first.

## [1.2.2] - 2026-11-23

Requires AI Studio 2.3.0.

### Fixed
- **Rate limit headers reach the client, not the vendor.** On an allowed
  request `X-RateLimit-Limit`, `X-RateLimit-Remaining` and
  `X-RateLimit-Reset` (and `X-RateLimit-Shadow-Breach` and
  `X-RateLimit-Error`) were returned from the post-auth hook, which sets
  them on the request the gateway sends to the vendor: the vendor saw the
  customer's limits, and the client saw the headers only on a `429`. They
  are now added to the response, buffered and streamed, and nothing is added
  to the upstream request. The vendor's own rate limit headers (such as
  OpenAI's `X-Ratelimit-Limit-Requests`) are passed on unchanged.
- **`X-RateLimit-Remaining` counts the request it is returned on.** With a
  limit of N the first allowed response says N-1 and the last says 0, which
  agrees with the `429` that follows. It was one too high. For a token rule
  it is what was left before the response, whose tokens are not known when
  the headers are sent.

The headers of a request wait in memory for its response. A request that
never gets one (the client went away, another plugin answered it) has them
dropped after ten minutes, and at most 100,000 requests are held at a time.

## [1.2.1] - unreleased

Requires AI Studio 2.3.0.

### Fixed
- **Saving a rule keeps the plugin's settings.** A rule change wrote the
  rules into the stored configuration together with the settings the Studio
  process had started with, so settings saved since in the configuration
  form (Redis backend, URL and password, `fail_open`, window size) were
  reverted without a warning, and edges went back to per-instance counters
  after the next push. A rule change now replaces only `rules` in the stored
  configuration; if the stored configuration cannot be read, the rules are
  not synced (the log says so) rather than written over it.

## [1.2.0] - 2026-10-23

Requires AI Studio 2.3.0. Built on the 2.3 Plugin SDK (64 MiB plugin message
limit).

### Fixed
- **Token rules count Bedrock.** Usage is read with the Plugin SDK's
  `ResponseUsage`, so Bedrock Converse's camelCase usage (`inputTokens`,
  `outputTokens`) and Google's `usageMetadata` are counted. Before, token
  rules never counted Bedrock or Google responses.
- **Streams are counted and released.** When a stream ends, its tokens are
  added to the request's token rules and its concurrent slots are released,
  also when it ends with an upstream error. Before, streams added no tokens,
  and a stream held its concurrent slot until the counter expired five minutes
  later, so concurrent rules could refuse requests that were within the limit.
- **Concurrent slots no longer leak.** A request answered before the
  upstream (a later plugin's block such as a cache hit, a request filter or
  guardrail block) gets no response phase, so its slot was never given back,
  and under steady traffic the counter never expired: the limit crept down
  until every request got a 429. Such slots are now given back once the
  request is ten minutes old, and the rate limiter's own 429 or 503 gives back
  the slots the request took under earlier rules at once.
- A `redis_password` that an edge gateway receives as
  `refused-credential-reference` (a `$SECRET/` reference AI Studio could not
  resolve) is never sent to Redis as the password. The README now says that
  AI Studio 2.3 resolves `$SECRET/` references on edges too.

# Changelog

Source of the entry for the marketplace listing: `make plugin-publish` carries
the previous version's CHANGELOG.md forward, so copy the new entry into the
marketplace repo when publishing. Newest first.

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

# Changelog

Source of the entry for the marketplace listing: `make plugin-publish` carries
the previous version's CHANGELOG.md forward, so copy the new entry into the
marketplace repo when publishing. Newest first.

## [1.0.3] - 2026-11-23

Requires AI Studio 2.3.0 (unchanged).

### Fixed
- **All three admin pages load in any order.** Opening a second Prompt
  Compressor page (Dashboard, Policies, Preview) in the same session failed
  with "Web Component … was not registered after executing JavaScript": the
  page scripts share one global scope in Studio and redeclared each other's
  top-level names. Each page now keeps its names to itself.

This fix was first added to 1.0.2 after a 1.0.2 build without it had been
published to the pre-release marketplace index (2026-10-23). 1.0.3 is the one
version that always has it; there is no other change from 1.0.2.

## [1.0.2] - 2026-10-23

Requires AI Studio 2.3.0 (unchanged).

### Fixed
- **Bedrock and Google requests are priced.** The model is now read from the
  request path when the body names none (Bedrock's `/model/{modelId}/`, an
  escaped ARN or inference profile included, and Google's `models/{model}:`),
  so the savings dashboard prices those records. Before, a Bedrock record had
  no model and was never priced.
- **Documentation:** the coverage table now lists `/ai/`, `/v1`, `/router/`
  and `/anthropic/` to Bedrock as covered from AI Studio 2.3, where the plugin
  runs on the gateway's internal `/llm/call/` hop on the Converse body.

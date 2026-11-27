# Changelog

Source of the entry for the marketplace listing: `make plugin-publish` carries
the previous version's CHANGELOG.md forward, so copy the new entry into the
marketplace repo when publishing. Newest first.

## [1.0.4] - 2026-11-27

Requires AI Studio 2.3.0 (unchanged).

### Fixed
- **Preview lists the saved policies.** The Preview page offered only the
  policies the plugin loaded at start-up: Studio does not hand a saved
  configuration to its own copy of the plugin. Preview now lists (sorted)
  and runs the policies stored in Studio, the ones the Policies page saves.
- **One token estimate everywhere.** Tool-definition sizes on the Savings
  page were UTF-8 bytes / 4 and the Preview's per-unit column showed the
  selection size, while saved tokens used the model's estimator. All three
  now use the estimator the saved-token figures use (tiktoken for
  OpenAI-family models, calibrated on the request's own text; a
  characters-per-token ratio otherwise), and the pages say which figures are
  estimates and which are the vendor's counts. Tool-definition token figures
  recorded before this version stay as they were (bytes / 4). Policy sizes
  (`min_unit_tokens` and the budgets) are still UTF-8 bytes / 4, so what is
  compressed does not change.
- **The menu says Savings.** The first page's menu entry read "Dashboard";
  the docs and the page call it Savings. The path is unchanged.
- **Saving policies asks in a dialog on the page**, not with the browser's
  confirm(), which blocked the page and scripted browsers.

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

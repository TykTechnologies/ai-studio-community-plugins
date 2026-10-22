# Changelog

Source of the entry for the marketplace listing: `make plugin-publish` carries
the previous version's CHANGELOG.md forward, so copy the new entry into the
marketplace repo when publishing. Newest first.

## [1.2.0] - unreleased

Requires AI Studio 2.3.0. Built on the 2.3 Plugin SDK (64 MiB plugin message
limit).

### Fixed
- **Model rules match Bedrock.** The model is read with the Plugin SDK's
  `RequestModel`, so Bedrock requests, whose model is in the
  `/model/{modelId}/` path, match rules by model ID, inference profile or ARN,
  escaped or not. Before, a Bedrock request was matched by its LLM's slug.
- **Bedrock Converse documents are inspected.** A document in a text format
  (`txt`, `md`, `csv`, `html`) is decoded and inspected, with its name and
  context; before, its base64 bytes were matched as they were, so a phrase in
  the document was missed. The bytes of Converse images, videos and binary
  documents no longer count as text, for matching or for `fail_closed`.
- **`hook_phase` is honoured.** The gateway calls both hooks the plugin
  declares, and each checked the request, so every request was checked twice
  whatever `hook_phase` said. The second check, in `post_auth`, saw the body
  as earlier `post_auth` plugins (such as the Prompt Compressor) had
  rewritten it. Now only the configured phase checks: `pre_auth` by default,
  and an unknown value falls back to it. `hook_phase` is in the configuration
  form.

# LLM Price Sync Plugin

Keeps AI Studio's model prices up to date from [llm-prices.com](https://www.llm-prices.com). A schedule (every 6 hours by default) fetches the current prices, converts them from $/million tokens to $/token, and updates the matching model prices. With `auto_create_models`, it also creates prices for models that don't exist yet. The plugin's page shows the last result and the last 10 runs, and can trigger a sync by hand.

## Configuration

| Key | Default | Description |
|-----|---------|-------------|
| `prices_url` | `https://www.llm-prices.com/current-v1.json` | Source of the prices (see below) |
| `sync_interval_cron` | `0 */6 * * *` | Schedule for automatic syncs (UTC) |
| `auto_create_models` | `true` | Create prices for models not yet in AI Studio |
| `vendor_filter` | all | Only sync these vendors |
| `dry_run` | `false` | Log what would change without changing anything |

## Source URL rules

`prices_url` must be an `https` URL. The plugin refuses to connect to internal addresses: loopback, private ranges, link-local (including the 169.254.169.254 cloud metadata address), CGNAT (100.64.0.0/10), multicast and unspecified. The check applies to the address actually dialled, after DNS resolution and on every redirect, so a public hostname that resolves to an internal address is refused too. Through an HTTP proxy (`HTTPS_PROXY`), the target hostname is resolved and checked before the request instead. Redirects must also be `https`.

For local development, `ALLOW_INTERNAL_NETWORK_ACCESS=true` allows plain `http` and internal addresses. From AI Studio 2.3, plugins only see that variable when it is passed through: `PLUGIN_ENV_PASSTHROUGH=ALLOW_INTERNAL_NETWORK_ACCESS`.

A URL that breaks these rules is logged as a warning at start-up, and every sync fails with the reason until it is fixed.

## Sync behaviour

- A scheduled sync runs under the scheduler's context. If AI Studio cancels it (timeout or shutdown), it stops between models, and the partial result is still recorded.
- A sync started from the UI is limited to 5 minutes.
- The result of every run is stored, even when the run failed. If storing it fails, a scheduled run is reported as failed, and a manual run returns `completed_unsaved` with the error.
- History updates are serialised, so a manual sync that runs at the same time as a scheduled one doesn't drop either entry.

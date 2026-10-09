# Changelog

## 1.0.0 (2026-10-09)

Initial release.

- Two APIs, chosen per data source: the native Timebase API, which adds units, descriptions and state labels, and the i3X standard API. Both use `dataset:tag` names, so queries survive a switch.
- History queries with one series per tag; a missing tag is a warning, not a failed query.
- Live streaming through Grafana Live for both APIs: panels backfill their time range, then append values as they arrive. One shared historian connection per tag, reconnected with backoff; i3X subscriptions are cleaned up.
- OAuth 2.0 client credentials against Timebase Pulse, with token caching, renewal before expiry, and one retry with a fresh token after a 401. A fixed bearer token is also supported.
- Grafana alerting support.
- Tag picker fed by `/objects`, custom ElementIds, and multi-value dashboard variables.
- Tree browser built from i3X parent links and dotted Timebase tag paths, with search, folder selection, and the historian's own health tags hidden unless asked for.
- Optional quality field, and an option to show non-Good samples as gaps.

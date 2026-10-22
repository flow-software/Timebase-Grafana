# Changelog

## 2.1.0 (2026-10-22)

- Tree browser: field filters are kept when the browser is closed and reopened, and are shared by every query editor using the same data source, for the rest of the browser session. A kept filter on a field the tags no longer have is ignored rather than hiding every tag.

## 2.0.0 (2026-10-22)

Breaking: the plugin ID is now `flowsoftware-timebase-datasource` (was `flowsoftware-timebasei3x-datasource`), so it installs as a new plugin. Data sources, dashboards and alert rules made with 1.x have to be recreated or pointed at the new type, and the `allow_loading_unsigned_plugins` setting needs the new ID.

- The native Timebase API is the default for new data sources; i3X is marked as a preview while its responses are still being aligned with the i3X 1.0 spec.
- The tag list behind the query editor is cached for a minute, so a dashboard full of query editors reads the historian once.
- New icon, and screenshots for the plugin catalog.
- Go 1.26.10 and current Grafana plugin SDK and libraries; no known vulnerabilities (govulncheck).
- Licence: Apache-2.0 for the whole repository (the repository root had a template MIT file).

## 1.1.0 (2026-10-21)

- Tree browser: filter tags by their fields (the historian's tag metadata, such as Site or Area). Add a field from "Filter by a tag field", then pick values as chips, or from a drop-down when a field has many values. Values of one field match any, fields combine, and counts narrow as you pick. Filters only change what is listed; queries still read the tags you tick. Native Timebase API only.
- Tree browser: hovering a tag shows its fields.

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

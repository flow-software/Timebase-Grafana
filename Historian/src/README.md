# Timebase Historian data source

Query tag history from the [Timebase Historian](https://timebase.flow-software.com) in Grafana, through either of the historian's APIs:

- **Timebase (native API).** Adds each tag's units, description and state labels, so axes read "°F" and a state tag shows "Running" instead of 2.
- **i3X.** The i3X standard API. Use it to stay on the standard, or with other i3X servers.

Both use the same tag names (`dataset:tag`), so dashboards and alert rules keep working if you switch a data source from one API to the other.

- **Automatic token renewal.** Give the data source a client ID and secret; it fetches OAuth 2.0 tokens from Timebase Pulse and renews them before they expire, so dashboards and alerts keep running.
- **Grafana alerting.** Queries run in the plugin's backend, so they work in alert rules, recorded queries and public dashboards. Each tag is its own series labelled `elementId`, so one rule produces one alert instance per tag.
- **Tree browser.** Pick tags from a tree of datasets and folders, with search and whole-folder selection, or type a tag name or a dashboard variable.

## Requirements

- Grafana 12.3 or later.
- Timebase Historian 2.0 or later, reachable from the Grafana server.
- A client registered in Timebase Pulse (client ID and secret) that may read the historian.

## Configuration

| Setting | Example | Notes |
| --- | --- | --- |
| API | Timebase | *Timebase* for the native API, *i3X* for the standard one. Switching adjusts the URL. |
| URL | `https://historian:4512` (Timebase) or `https://historian:4512/i3x/v1` (i3X) | i3X needs the `/i3x/v1` path; the native API takes the bare address. |
| Authentication | Client credentials | Recommended: tokens are renewed automatically. *Bearer token* takes a fixed token that is not renewed. |
| Token URL | `https://pulse:4542/auth/token` | The Timebase Pulse token endpoint. |
| Client ID / secret | | The secret is stored encrypted and is only read by the plugin backend. |
| Skip certificate verification | | For self-signed certificates. Prefer **Custom CA certificate** with the Pulse root CA. |

Select **Save & test**. A working setup reports what it reached, for example *Connected to the Timebase Historian (native API); 3 datasets* or *Connected to Timebase Historian 2.0.813 (i3X 1.0)*. If it fails, the message says whether getting a token or calling the API went wrong.

## Queries

Pick one or more tags. Each returns a series named after the tag, with the dataset prefix removed.

- Numbers and booleans (as 1/0) become numeric series that can be graphed and alerted on. Text tags come back as text series.
- With the native API, units and state labels from the historian are applied automatically; you can still override them in the panel.
- **Quality field** adds each sample's quality (Good, Uncertain, Bad) as an extra field. Leave it off for alert rules.
- **Bad quality as gaps** shows samples whose quality is not Good as gaps, so stats and alerts ignore them.
- A tag that does not exist shows a warning on the panel; the other tags still load.
- The historian's own health tags (CPU, memory, storage, failure counters) are hidden in the tree browser unless you turn on **Show historian system tags**.

### Live

Turn on **Live** to stream new values into the panel as the historian records them, with no dashboard refresh. The panel first loads the tags' history for its time range, then appends each new value and scrolls to keep the same window.

- Both APIs stream: the native API through its data stream, i3X through a subscription that the plugin creates and deletes for you.
- Each tag uses one connection from Grafana to the historian, shared by everyone viewing it, and closed when nobody is. Dropped connections are retried automatically.
- **Quality field** is not available on live queries.
- Alert rules always read history; the Live setting does not affect them.

### Variables

Tag names can contain dashboard variables, such as `$site:Line1.Speed`. A multi-value variable expands to one tag per selected value.

## Alerting

Create an alert rule with this data source as query A, add a *Reduce* expression (for example *Last*) and a *Threshold*. Leave **Quality field** off in alert queries.

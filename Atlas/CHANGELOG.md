# Changelog

## 0.2.0 (unreleased)

- History query: stored values of live properties over the panel time range. Instances and single properties are picked together in one tree, with properties listed under each instance; ticking a row picks it and everything nested under it; ids and `$variables` can be typed too. Record properties (fields with one timestamp, e.g. MaintainX LiveAsset) are read as a column per field. Auto draws one line that smooths noise but keeps stops and spikes at their true values (no extra series); Raw reads every value; Minute / Hour / Day are averages with optional min/max series. Optional quality. Series are labelled `instance` and `property` and work in alert rules.
- Cypher: "Time series" output format (one series per combination of the text columns); "Edit as Cypher" writes a whole History selection as typed reads joined with `UNION ALL`. `$bucket` is filled with the History builder's bucket unit for the range.
- Cypher: `$from` and `$to` are filled from the panel time range; ISO timestamp columns become time fields, so Cypher history queries work in time series panels.
- Demo dashboard: History panels.

## 0.1.0 (2026-11-16)

First version of the Timebase Atlas data source.

- Graph query: the whole domain, or the instances within 1 to 4 relationships of one instance, for the Node Graph panel.
  - Node rings by data quality, or one colour per entity type.
  - Lines by containment / other, or one colour per relationship type, with a key in the query editor.
  - Optional relationship-name dots, so names show without hovering.
  - Clicking a node lists its values and links to Explore: graph around it, its values, its nested values.
- Values query: one instance, an instance and everything nested under it, or every instance of a type; one row per property or one row per instance. Compound properties are split into readable parts.
- Cypher query: read-only Cypher with dashboard variables passed as Cypher parameters; output as a table or a node graph.
- Browse modal for instances (nested by containment, as in Atlas) and entity types.
- "Edit as Cypher" turns a builder query into the matching Cypher query.
- Dashboard variables from any query; the first column becomes the values.
- Uses the Atlas /api/v1 REST API only.

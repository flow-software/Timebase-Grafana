# Changelog

## 0.1.0 (unreleased)

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
- Uses the Atlas /api/v1 REST API only. No history yet (see flow-software/Timebase#2394).

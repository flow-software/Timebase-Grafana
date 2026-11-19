# Changelog

## 0.2.0 (unreleased)

- Values and History merged into one **Data** tab: one selection, shown as **Current** values (table) or a **Trend** (time series). The selection can hold an instance with everything nested under it, and every instance of a type (Types view), both resolved when the query runs; older Values and History queries open as the matching selection. The picker uses Timebase colours for selections and property kinds.
- Trend (History): stored values of live properties over the panel time range. Instances and single properties are picked together in one tree, with properties listed under each instance; ticking a row picks it and everything nested under it; ids and `$variables` can be typed too. Record properties (fields with one timestamp, e.g. MaintainX LiveAsset) are read as a column per field. Auto draws one line that smooths noise but keeps stops and spikes at their true values (no extra series); Raw reads every value; Minute / Hour / Day are averages with optional min/max series. Optional quality. Series are labelled `instance` and `property` and work in alert rules.
- Cypher: "Time series" output format (one series per combination of the text columns); "Edit as Cypher" writes a whole History selection as typed reads joined with `UNION ALL`. `$bucket` is filled with the History builder's bucket unit for the range.
- Cypher: `$from` and `$to` are filled from the panel time range; ISO timestamp columns become time fields, so Cypher history queries work in time series panels.
- Demo dashboard: History panels.
- Property kinds in the picker and for Trend come from the Atlas type definitions (entity types, pins), not only the current value, so properties with an empty current value no longer show as "unknown".
- Fix: Auto Trend of a text property whose current value is empty returned Atlas 500; it is now read raw.
- Trend averages are time-weighted (each value counts for as long as it held), in Auto and in Minute / Hour / Day buckets, the way Timebase Explorer averages. Exact from raw values up to 3 hours or 100,000 values, otherwise from Atlas minute buckets. Live on CNC02 FeedRate, one hour read 2395 as a plain average and 680 time-weighted: the machine stood still most of that hour. A slice or bucket with no value breaks the line.
- Units on Trend series from the instance's `UOM` (atlas-common Tag pattern, e.g. InfoHub KPIs).
- "Edit as Cypher" keeps dashboard variables in Data selections as Cypher parameters (`[m IN $machines | m + "-Production"]`, one read per property switched on by `"FeedRate" IN $prop`).
- "Edit as Cypher" writes nested picks with a variable as a containment match (`-[:…contains…*1..N]->(n:Type)`, live-checked to read history like a match by id). Fix: such picks were written as reads of the variable's own instances (e.g. `CNCMachine … PartCount`), which returned nothing; the Time series format then reported a missing timestamp column instead of "no rows".
- Dashboard variables in the Select modal: instance variables listed with an example instance to pick under ("every CNCProduction under `$machines`", matched by containment and type), property variables (`$prop`) next to the properties, and a **$** button on chips that offers the variables that fit.
- Dashboard variables from a **Selection** in the Select modal: instance ids (e.g. every CNCMachine) or property names. `$variables` also work in the property part of a pick (`$machine-Production/$prop`).
- Demo dashboard: a state timeline of job, program and alarm per machine, `$machines` and `$prop` Selection variables, and an hourly time-weighted FeedRate panel.

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

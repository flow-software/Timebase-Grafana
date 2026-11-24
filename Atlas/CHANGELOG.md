# Changelog

## 0.3.0 (2026-11-24)

Breaking: the plugin ID is now `flowsoftware-timebaseatlas-datasource` (was `flowsoftware-atlas-datasource`), so both Timebase plugins read as Timebase products (the Historian's is `flowsoftware-timebasehistorian-datasource`). It installs as a new plugin: data sources and dashboards made with 0.2.0 have to be recreated or pointed at the new type, and the `allow_loading_unsigned_plugins` setting needs the new ID.

## 0.2.0 (2026-11-24)

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
- Fix: Graph with Colour **None** and **Relationship names** on drew no rings around instances (an empty colour in the color field the name nodes need); they now get a neutral grey ring.
- Graph **Start at** matches the Data selection: a Select button, the pick as a chip with a **$** suggestion, and dashboard variables listed first in the modal (with the Timebase look). A multi-value variable gives one graph around all its instances. "Edit as Cypher" keeps a start variable as a parameter (it used to write `a.entityId = "$machine"`).
- "Edit as Cypher" includes record properties: one raw read per number part (`v.MxLocationId`), matching the Trend's series. The tree resource lists each record's parts.
- "Edit as Cypher" writes nested picks with a variable as a containment match (`-[:…contains…*1..N]->(n:Type)`, live-checked to read history like a match by id). Fix: such picks were written as reads of the variable's own instances (e.g. `CNCMachine … PartCount`), which returned nothing; the Time series format then reported a missing timestamp column instead of "no rows".
- Dashboard variables in the Select modal: instance variables listed with an example instance to pick under ("every CNCProduction under `$machines`", matched by containment and type), property variables (`$prop`) next to the properties, and a **$** button on chips that offers the variables that fit.
- Dashboard variables from a **Selection** in the Select modal: instance ids (e.g. every CNCMachine) or property names. `$variables` also work in the property part of a pick (`$machine-Production/$prop`).
- Cypher **Time-weight** (Time series format): Auto, Minute, Hour or Day draws the returned raw values with the Trend's own time-weighting. "Edit as Cypher" on a Trend now writes raw reads and sets it to the Trend's aggregation, so the panel no longer changes when you switch (it used to write plain `avg()` buckets: a stop stored as a single 0 showed as a small dip and a gap). Live on CNC01-SpindleMotor RPM over 6 hours, Trend and Cypher returned the same 349 points (Auto) and 361 points (Minute). Over 7 days the Cypher read reaches the 200,000-value cap and says so.
- Enumerations (Atlas enumeration types): properties bound to one are read raw and never averaged, in the Trend (every aggregation) and in time-weighted Cypher, and get value mappings with one colour per member, so codes show as labels (ControllerMode 1 = Automatic). Current tables add a **Value label** column (row per property) or map the column (row per instance). The Select modal shows the enumeration on the property. Live-tested on cesmii-workshop with a ControllerModeEnum bound to CNCProduction.ControllerMode: Trend Auto and Hour, and Cypher Auto and Hour, returned only 1, 2 and 3 with the labels.
- Graph **Relationships**: follow and draw only the relationship types you tick (with Depth, the walk follows only them), picked in a modal that shows each type's line colour and style, with All / None and relationship-type variables. "Edit as Cypher" writes it as `type(r) IN [...]`. The modal counts each type: around the graph's start instance (within its depth, listing only the types it has) or in the whole domain. Live on CNC01 depth 2: 12 edges over 10 types with every relationship, 1 edge with only _cncBaseContainsChannel_.
- Dashboard variables from a Selection: the Select modal opens on **Types**. With **Values = Instances** it lists only instances and types (no properties; any instance can be picked, with live values or not), and properties saved in older instance variables are ignored, so "every CNCMachine LiveOpenWorkOrders" gives all five machines.
- Dashboard variables: **Relationship types** (names shown, type URIs as values; optionally only the types picked under Offer), for a Graph query's Relationships.
- Dashboard variables: **Enumeration members** (Selection → Values) lists an Atlas enumeration's members, labels shown and keys as values, for Cypher filters like `n.ControllerMode.Value IN $mode`; number and boolean keys are sent typed so they match stored values. The enumeration is picked in the same Atlas modal as instances, with its members listed under it. New `enums` resource.
- Trend **Quality** is no longer drawn as its own line (a flat line at 192 that stretched the value axis). It stays with its series, shown as Good / Uncertain / Bad in the tooltip and as a table column. In Auto, every point now has a quality: an averaged point shows the worst quality in its slice, and a value held through a stop keeps its quality (before, only the peaks had one).
- Demo dashboard: `$rels` variable filtering the "Around `$machine`" graph.
- Demo dashboard: `$mode` variable and a "CNC machines now in `${mode:text}`" Cypher table.
- Demo dashboard: controller mode state timeline and current-mode table, labels from the Atlas enumeration.
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

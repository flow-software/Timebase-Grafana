# Timebase Atlas data source

Show what is in Timebase Atlas in Grafana: the entities of an information model, their current values, and the relationships between them.

## Query types

| Query      | What it returns                                                                                                                  | Use it with                                               |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| **Graph**  | Entities and relationships: the whole domain, or everything within 1 to 4 relationships of one instance                          | Node Graph panel                                          |
| **Data**   | The instances and properties you pick, shown as **Current** values (with quality and time) or as a **Trend** over the time range | Current: Table, Stat, Gauge. Trend: Time series, alerting |
| **Cypher** | Your own read-only Cypher query. Output is a table, or a node graph when the result holds relationships                          | Any panel                                                 |

Every query has a **Branch** and **Domain**. Leave them empty to use the defaults from the data source settings.

**Edit as Cypher** turns a Graph or Data query into the matching Cypher query, so you can start from something that works.

### Graph

- **Start at**: empty shows the whole domain. **Browse** opens the instance tree, nested by containment as in the Atlas Instances view, with a filter; you can also type an id or a `$variable`.
- **Colour**: _Data quality_ splits each node's ring by the quality of its live values (green Good, yellow Uncertain, red Bad, grey no data, blue no live values). _Entity type_ gives each type its own colour.
- **Lines**: _Containment / other_ draws containment grey and solid, other relationships orange and dashed. _Relationship type_ gives each relationship type its own colour; containment stays solid. A key under the options lists the types in the current result. A type keeps its colour across panels.
- **Relationship names**: the Node Graph panel shows relationship names only on hover. Switch this on to draw each relationship as a small dot with its name, so names are always visible.
- Click a node to see all its values, and links that open Explore with a graph around that instance or its values.

### Data

One selection, two ways to show it. **Show**: _Current_ gives the latest value of each property; _Trend_ gives the stored values over the panel's time range.

**Select instances and properties** opens the instance tree, nested as in Atlas, with each instance's live properties listed under it:

- tick a row to read all live values of that instance and of everything nested under it (a dash means part of it is picked);
- open an instance and tick single properties;
- the filter matches instance names, ids, types and property names.

Ticking a row with instances nested under it picks it **with everything nested**: the query reads whatever is nested under it when it runs, so instances added later are included. Unticking part of it turns it into single picks. The **Types** view picks **every instance of a type** (or one property of every instance), also resolved when the query runs.

Ids, dashboard variables and types can be typed next to the button: `$machine-Production` for all its live values, `$machine-Production/FeedRate` for one property, `type:CNCProduction/FeedRate` for that property of every CNCProduction. A multi-value variable gives one pick per value. The tree icon on a chip switches an instance (or a `$variable`) to include everything nested under it.

A whole instance shows every property; a picked property only that one.

#### Current

**Layout**: _Row per property_ gives Instance, Property, Value, Quality, Quality code and Time on each row. _Row per instance_ gives one row per instance and a column per property, to compare instances; switch on **Quality** and **Timestamps** to add a column of each next to every live value. Compound properties are split into parts, for example `LiveAsset.MxAssetName`. Quality is named from the OPC quality code: Good, Uncertain, Bad, or No data when there is no value. Current shows a value however long ago it last changed.

Queries saved before 0.2.0 (Values or History of an instance, of an instance with everything nested under it, or of a type) open as the matching selection.

#### Trend

Each instance and property becomes one series, labelled `instance` and `property`, so the query also works in alert rules. A trend only has values stored inside the time range.

Record properties (a group of fields with one timestamp, such as MaintainX `LiveAsset` or `LiveOpenWorkOrders`) are listed too. They change rarely; each stored version becomes one row with a column per field, best shown in a table or state timeline.

Text and status properties (a job number, a program, an alarm flag) suit the **State timeline** panel: pick them with Aggregation _Raw_, and give numbers value mappings (0 = OK, 1 = Alarm). Keep the panel's colour scheme on a palette: with colours from thresholds the state timeline merges values into threshold bands and ignores the mappings.

**Averages are time-weighted.** A stored value holds until the next one, so each value counts for as long as it was in force, not by how often it was recorded: a machine that ran for 15 minutes (a value every few seconds) and then stood still for 45 (one value of 0) averages low, not near its running speed. Atlas's own `avg()` is a plain mean, so the plugin does the weighting, as Timebase Explorer does. Time with an empty value is left out. The value in force when the range starts counts from the start, and the last value holds until now.

**Aggregation**: _Auto_ draws one line per series that smooths noise but keeps stops and spikes. The panel range is cut into slices (a third of the panel's max data points); each slice draws its time-weighted average, plus its lowest or highest value at its own time when that lies further from the average than the series' typical spread (the median min-to-max range of its slices). Raw values that fit the panel are returned unchanged. A slice with no value at all after one with a value breaks the line. _Raw_ reads every value. _Minute_, _Hour_ and _Day_ give one time-weighted average per bucket (UTC), with optional min and max. Text and boolean properties are always read raw.

The weighting is exact when the plugin can read the raw values: always up to 3 hours, and beyond that whenever the range holds at most 100,000 of them. Past that it weighs Atlas's minute (or hour, day) bucket averages: exact between buckets, a plain mean inside each. If even that is too coarse for a fixed bucket size, the panel says the values are plain averages. A property's kind (number, text, boolean, record) comes from its type definition in the Atlas information model (`entity-types`, including pins on subtypes), so it is right even when the current value is empty; only a property whose value type is left open is judged by its current value. **Min/max** (Minute, Hour, Day) adds the bucket minimum and maximum (including a value held over from before the bucket) as series; **Quality** adds the OPC quality code of each raw value.

**Units**: Atlas type definitions carry no units, but instances built on the atlas-common Tag pattern have a `UOM` text property next to `Data` (the InfoHub KPI instances do). The unit is set on the `Data` series, or on the only live value of such an instance. Common units (`%`, `°C`, `min`, `rpm`, `kW`, `bar`, ...) map to Grafana units; others are shown as a suffix.

The first value can be from just before the range: it is the value the property held when the range starts.

## Cypher and dashboard variables

Atlas Cypher is read-only: `CREATE`, `MERGE`, `DELETE` and `SET` are rejected by Atlas.

Use `$name` in a Cypher query to refer to the dashboard variable `name`. The plugin sends the variable's value as a Cypher parameter, so a value can never change the query itself. A multi-value variable is sent as a list:

```cypher
MATCH (m:CNCMachine)-[r]->(c)
WHERE m.entityId IN $machine
RETURN m, r, c
```

In the Graph and Data fields, `$name` is replaced as text, as in other data sources.

`$from` and `$to` are filled from the panel's time range unless a dashboard variable has that name, so a Cypher history query follows the time picker:

```cypher
MATCH (n:CNCProduction) WHERE n.entityId = "CNC01-Production"
UNWIND n.SpindleTemp AS v
WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)
RETURN v.Timestamp AS time, v.Value AS SpindleTemp ORDER BY time
```

Name the type in the `MATCH` (`(n:CNCProduction)`): with a bare `(n)` Atlas unwinds only the current value. `$bucket` is filled the same way, with the unit the Trend builder would pick for the range (`"m"`, `"h"` or `"d"`), for `relative(v.Timestamp, $bucket)`.

**Edit as Cypher** on a Trend writes the whole selection (with Cypher's plain `avg()`, not time-weighted): one typed read per entity type and property, joined with `UNION ALL`, returning `entity, property, time, value`. Dashboard variables stay variables: `$machines-Production` becomes `n.entityId IN [m IN $machines | m + "-Production"]`, typed by the instances the variable gives now. A property variable (`/$prop`) gets one read per number property it could name, each switched on by `AND "FeedRate" IN $prop`, because Atlas answers `n[$prop]` wrongly (the entity id for a current value, nothing for history). Nested and type picks with a variable cannot be written as Cypher. The **Time series** format splits such rows into one series per entity and property. Text, boolean and record values are left out (listed in a comment), since they do not plot. Group into buckets with `relative(v.Timestamp, "h")` and `avg()`, `min()` or `max()`; only a bare unit (`"m"`, `"h"`, `"d"`) makes buckets. Timestamp columns become time fields, so the result can go in a time series panel.

## Dashboard variables from Atlas

A variable query has two sources:

- **Selection** uses the same Select modal as the Data tab. **Values** _Instances_ gives the ids of the picked instances (shown with their names, or the id where names repeat); _Properties_ gives the names of the picked live properties. Picks are resolved when the dashboard loads, so "every CNCMachine" includes machines added later.
- **Query** runs any Graph, Data or Cypher query; the first column becomes the values. For example `MATCH (m:CNCMachine) RETURN m.entityId` gives one value per machine. Variables saved before 0.2.0 open here.

Use the variables in a Data selection: `$machine-Production/$prop`, or `$machine` with the tree icon on (+ nested). A multi-value or All variable gives one pick per value, in the instance and in the property. Grafana's **Run query** in the variable editor refreshes the preview of values.

## Live values

Values are read when the panel refreshes. Set the dashboard to refresh every few seconds to follow them; each refresh is one or two small calls to Atlas.

## Limits

- Trend reads are capped at 200,000 raw values per property; use buckets for longer ranges.
- A whole-domain graph returns at most 2000 relationships unless you raise **Limit**. Graphs around an instance stop at the same limit.
- Cypher results are capped at 50,000 rows.
- Atlas returns internal numbers instead of ids for Cypher paths (`p = (...)`) and variable-length relationships (`-[r*1..3]-`). Use the Graph query with a depth instead, or return the nodes at each end.
- `n.entityLabel` returns null in Cypher; return the node `n` itself to get its label ([flow-software/Timebase#2396](https://github.com/flow-software/Timebase/issues/2396)).

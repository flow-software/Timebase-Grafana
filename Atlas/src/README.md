# Timebase Atlas data source

Show what is in Timebase Atlas in Grafana: the entities of an information model, their current values, and the relationships between them.

## Query types

| Query       | What it returns                                                                                                                 | Use it with            |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| **Graph**   | Entities and relationships: the whole domain, or everything within 1 to 4 relationships of one instance                         | Node Graph panel       |
| **Values**  | Current values, with quality and time, of one instance, an instance and everything nested under it, or every instance of a type | Table, Bar chart, Stat |
| **History** | Stored values of live properties over the panel time range: raw points, or minute, hour or day averages                         | Time series, alerting  |
| **Cypher**  | Your own read-only Cypher query. Output is a table, or a node graph when the result holds relationships                         | Any panel              |

Every query has a **Branch** and **Domain**. Leave them empty to use the defaults from the data source settings.

**Edit as Cypher** turns a Graph or Values query into the matching Cypher query, so you can start from something that works.

### Picking instances and types

**Browse** opens a tree of the domain's instances, nested by containment as in the Atlas Instances view, with a filter. For **Types**, the tree lists the entity types, each with its instances underneath. You can also type an id, a type name or a `$variable` directly.

### Values

| Read                 | Reads                                                    |
| -------------------- | -------------------------------------------------------- |
| **Instances**        | One instance                                             |
| **Nested Instances** | An instance and everything nested under it, parent first |
| **Types**            | Every instance of one entity type                        |

**Layout**: _Row per property_ gives Property, Value, Quality, Quality code and Time on each row. _Row per instance_ gives one row per instance and a column per property, to compare instances; switch on **Quality** and **Timestamps** to add a column of each next to every live value.

Compound properties are split into parts, for example `LiveAsset.MxAssetName`, each with the compound's timestamp. Quality is named from the OPC quality code: Good, Uncertain, Bad, or No data when there is no value.

### Graph

- **Colour**: _Data quality_ splits each node's ring by the quality of its live values (green Good, yellow Uncertain, red Bad, grey no data, blue no live values). _Entity type_ gives each type its own colour.
- **Lines**: _Containment / other_ draws containment grey and solid, other relationships orange and dashed. _Relationship type_ gives each relationship type its own colour; containment stays solid. A key under the options lists the types in the current result. A type keeps its colour across panels.
- **Relationship names**: the Node Graph panel shows relationship names only on hover. Switch this on to draw each relationship as a small dot with its name, so names are always visible.
- Click a node to see all its values, and links that open Explore with a graph around that instance or its values.

### History

Reads the stored values of live properties (those with a timestamp and quality, such as FeedRate) over the panel's time range. **Select instances and properties** opens the instance tree, nested as in Atlas, with each instance's live properties listed under it:

- tick a row to read all live values of that instance and of everything nested under it (a dash means part of it is picked);
- open an instance and tick single properties;
- **Only with history** hides instances without live values; the filter matches instance names, ids, types and property names.

Ids and dashboard variables that are not in the tree can be typed next to the button: `$machine-Production` for all its live values, `$machine-Production/FeedRate` for one property. A multi-value variable gives one pick per value.

Each instance and property becomes one series, labelled `instance` and `property`, so the query also works in alert rules.

Record properties (a group of fields with one timestamp, such as MaintainX `LiveAsset` or `LiveOpenWorkOrders`) are listed too. They change rarely; each stored version becomes one row with a column per field, best shown in a table or state timeline.

**Aggregation**: _Auto_ draws one line per series that smooths noise but keeps stops and spikes. The panel range is cut into slices (a third of the panel's max data points); each slice draws its average, plus its lowest or highest value at its own time when that lies further from the average than the series' typical spread (the median min-to-max range of its slices). Atlas supplies every value up to 3 hours (returned unchanged when they fit the panel), then each minute's (for long ranges each hour's or day's) average, min and max. _Raw_ reads every value. _Minute_, _Hour_ and _Day_ are plain averages of the stored values in each bucket (not time-weighted), so short peaks shrink. Text and boolean properties are always read raw. **Min/max** (Minute, Hour, Day) adds the bucket minimum and maximum as series; **Quality** adds the OPC quality code of each raw value.

The first value can be from just before the range: it is the value the property held when the range starts.

## Cypher and dashboard variables

Atlas Cypher is read-only: `CREATE`, `MERGE`, `DELETE` and `SET` are rejected by Atlas.

Use `$name` in a Cypher query to refer to the dashboard variable `name`. The plugin sends the variable's value as a Cypher parameter, so a value can never change the query itself. A multi-value variable is sent as a list:

```cypher
MATCH (m:CNCMachine)-[r]->(c)
WHERE m.entityId IN $machine
RETURN m, r, c
```

In the Graph, Values and History fields, `$name` is replaced as text, as in other data sources.

`$from` and `$to` are filled from the panel's time range unless a dashboard variable has that name, so a Cypher history query follows the time picker:

```cypher
MATCH (n:CNCProduction) WHERE n.entityId = "CNC01-Production"
UNWIND n.SpindleTemp AS v
WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)
RETURN v.Timestamp AS time, v.Value AS SpindleTemp ORDER BY time
```

Name the type in the `MATCH` (`(n:CNCProduction)`): with a bare `(n)` Atlas unwinds only the current value. `$bucket` is filled the same way, with the unit the History builder would pick for the range (`"m"`, `"h"` or `"d"`), for `relative(v.Timestamp, $bucket)`.

**Edit as Cypher** on a History query writes the whole selection: one typed read per entity type and property, joined with `UNION ALL`, returning `entity, property, time, value`. The **Time series** format splits such rows into one series per entity and property. Text, boolean and record values are left out (listed in a comment), since they do not plot. Group into buckets with `relative(v.Timestamp, "h")` and `avg()`, `min()` or `max()`; only a bare unit (`"m"`, `"h"`, `"d"`) makes buckets. Timestamp columns become time fields, so the result can go in a time series panel.

## Dashboard variables from Atlas

Any query can fill a variable; the first column becomes the values. For example a Cypher query `MATCH (m:CNCMachine) RETURN m.entityId` gives one value per machine.

## Live values

Values are read when the panel refreshes. Set the dashboard to refresh every few seconds to follow them; each refresh is one or two small calls to Atlas.

## Limits

- History reads are capped at 200,000 raw values per property; use buckets for longer ranges.
- A whole-domain graph returns at most 2000 relationships unless you raise **Limit**. Graphs around an instance stop at the same limit.
- Cypher results are capped at 50,000 rows.
- Atlas returns internal numbers instead of ids for Cypher paths (`p = (...)`) and variable-length relationships (`-[r*1..3]-`). Use the Graph query with a depth instead, or return the nodes at each end.
- `n.entityLabel` returns null in Cypher; return the node `n` itself to get its label ([flow-software/Timebase#2396](https://github.com/flow-software/Timebase/issues/2396)).

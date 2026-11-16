# Timebase Atlas data source

Show what is in Timebase Atlas in Grafana: the entities of an information model, their current values, and the relationships between them.

## Query types

| Query | What it returns | Use it with |
| --- | --- | --- |
| **Graph** | Entities and relationships: the whole domain, or everything within 1 to 4 relationships of one instance | Node Graph panel |
| **Values** | Current values, with quality and time, of one instance, an instance and everything nested under it, or every instance of a type | Table, Bar chart, Stat |
| **Cypher** | Your own read-only Cypher query. Output is a table, or a node graph when the result holds relationships | Any panel |

Every query has a **Branch** and **Domain**. Leave them empty to use the defaults from the data source settings.

**Edit as Cypher** turns a Graph or Values query into the matching Cypher query, so you can start from something that works.

### Picking instances and types

**Browse** opens a tree of the domain's instances, nested by containment as in the Atlas Instances view, with a filter. For **Types**, the tree lists the entity types, each with its instances underneath. You can also type an id, a type name or a `$variable` directly.

### Values

| Read | Reads |
| --- | --- |
| **Instances** | One instance |
| **Nested Instances** | An instance and everything nested under it, parent first |
| **Types** | Every instance of one entity type |

**Layout**: *Row per property* gives Property, Value, Quality, Quality code and Time on each row. *Row per instance* gives one row per instance and a column per property, to compare instances; switch on **Quality** and **Timestamps** to add a column of each next to every live value.

Compound properties are split into parts, for example `LiveAsset.MxAssetName`, each with the compound's timestamp. Quality is named from the OPC quality code: Good, Uncertain, Bad, or No data when there is no value.

### Graph

- **Colour**: *Data quality* splits each node's ring by the quality of its live values (green Good, yellow Uncertain, red Bad, grey no data, blue no live values). *Entity type* gives each type its own colour.
- **Lines**: *Containment / other* draws containment grey and solid, other relationships orange and dashed. *Relationship type* gives each relationship type its own colour; containment stays solid. A key under the options lists the types in the current result. A type keeps its colour across panels.
- **Relationship names**: the Node Graph panel shows relationship names only on hover. Switch this on to draw each relationship as a small dot with its name, so names are always visible.
- Click a node to see all its values, and links that open Explore with a graph around that instance or its values.

## Cypher and dashboard variables

Atlas Cypher is read-only: `CREATE`, `MERGE`, `DELETE` and `SET` are rejected by Atlas.

Use `$name` in a Cypher query to refer to the dashboard variable `name`. The plugin sends the variable's value as a Cypher parameter, so a value can never change the query itself. A multi-value variable is sent as a list:

```cypher
MATCH (m:CNCMachine)-[r]->(c)
WHERE m.entityId IN $machine
RETURN m, r, c
```

In the Graph and Values fields, `$name` is replaced as text, as in other data sources.

## Dashboard variables from Atlas

Any query can fill a variable; the first column becomes the values. For example a Cypher query `MATCH (m:CNCMachine) RETURN m.entityId` gives one value per machine.

## Live values

Values are read when the panel refreshes. Set the dashboard to refresh every few seconds to follow them; each refresh is one or two small calls to Atlas.

## Limits

- No history yet. Atlas has no history route on its public REST API; see [flow-software/Timebase#2394](https://github.com/flow-software/Timebase/issues/2394).
- A whole-domain graph returns at most 2000 relationships unless you raise **Limit**. Graphs around an instance stop at the same limit.
- Cypher results are capped at 50,000 rows.
- Atlas returns internal numbers instead of ids for Cypher paths (`p = (...)`) and variable-length relationships (`-[r*1..3]-`). Use the Graph query with a depth instead, or return the nodes at each end.
- `n.entityLabel` returns null in Cypher; return the node `n` itself to get its label.

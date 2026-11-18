import { AtlasQuery, TreeNodeData } from './types';

/**
 * Names used as $parameters in a Cypher query, in order of first use.
 * A $ inside a string literal is ignored, so 'cost in $' is not a parameter.
 */
export function cypherParameterNames(cypher: string): string[] {
  const names: string[] = [];
  let quote: string | null = null;
  for (let i = 0; i < cypher.length; i++) {
    const c = cypher[i];
    if (quote) {
      if (c === '\\') {
        i++;
      } else if (c === quote) {
        quote = null;
      }
      continue;
    }
    if (c === '"' || c === "'" || c === '`') {
      quote = c;
      continue;
    }
    if (c === '$') {
      const m = /^[A-Za-z_][A-Za-z0-9_]*/.exec(cypher.slice(i + 1));
      if (m && !names.includes(m[0])) {
        names.push(m[0]);
      }
    }
  }
  return names;
}

/** A Cypher string literal for a value typed into the editor. */
export function cypherString(s: string): string {
  return '"' + s.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
}

/**
 * The Cypher equivalent of a builder query, for "Edit as Cypher". Returns
 * undefined where there is no single-query equivalent: Atlas returns internal
 * ids for multi-hop paths, so the plugin expands those one hop at a time.
 */
export function toCypher(q: AtlasQuery): string | undefined {
  switch (q.queryType ?? 'graph') {
    case 'graph': {
      const limit = q.limit && q.limit > 0 ? q.limit : 2000;
      if (!q.instanceId) {
        return `MATCH (a)-[r]->(b)\nRETURN a, r, b\nLIMIT ${limit}`;
      }
      if ((q.depth ?? 1) > 1) {
        return undefined;
      }
      return `MATCH (a)-[r]-(b)\nWHERE a.entityId = ${cypherString(q.instanceId)}\nRETURN a, r, b\nLIMIT ${limit}`;
    }
    case 'values':
    case 'instances': {
      const scope = q.queryType === 'instances' ? 'type' : (q.scope ?? 'instance');
      if (scope === 'instance' && q.instanceId) {
        return `MATCH (n)\nWHERE n.entityId = ${cypherString(q.instanceId)}\nRETURN n`;
      }
      if (scope !== 'type' || !q.entityType || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(q.entityType)) {
        return undefined;
      }
      const limit = q.limit && q.limit > 0 ? q.limit : 1000;
      return `MATCH (n:${q.entityType})\nRETURN n\nLIMIT ${limit}`;
    }
    case 'history': {
      // A selection needs each instance's type and properties: see historyCypher.
      if (q.selection?.length) {
        return undefined;
      }
      const prop = q.properties?.length === 1 ? q.properties[0] : undefined;
      if (q.scope !== 'type' || !prop || !q.entityType || !IDENT.test(q.entityType) || !IDENT.test(prop)) {
        return undefined;
      }
      return (
        `MATCH (n:${q.entityType})\nUNWIND n.${prop} AS v\n` +
        `WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)\n` +
        `RETURN n.entityId, relative(v.Timestamp, "h") AS t, avg(v.Value)\nORDER BY t`
      );
    }
    default:
      return undefined;
  }
}

const IDENT = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Value kinds that plot as numbers. Text, boolean and record values are left out of the Cypher. */
const PLOTTABLE = new Set(['number', 'unknown']);

/**
 * The Cypher for a History selection, using the browse tree for each
 * instance's type and live properties. Atlas only reads stored values when
 * the MATCH names the type, so there is one read per type and property,
 * joined with UNION ALL into long rows (entity, property, time, value) for
 * the "Time series" output format. Auto aggregation uses $bucket, which the
 * plugin fills from the time range like the builder does.
 */
export function historyCypher(q: AtlasQuery, nodes: TreeNodeData[]): { cypher?: string; error?: string } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const groups = new Map<string, { type: string; prop: string; ids: string[] }>();
  const missing: string[] = [];
  const left: string[] = [];
  for (const pick of q.selection ?? []) {
    const n = byId.get(pick.instance);
    if (!n) {
      missing.push(pick.instance);
      continue;
    }
    const live = n.live ?? [];
    const props = pick.property ? [pick.property] : live.map((p) => p.name);
    for (const prop of props) {
      const kind = live.find((p) => p.name === prop)?.kind ?? 'unknown';
      if (!PLOTTABLE.has(kind) || !IDENT.test(n.type) || !IDENT.test(prop)) {
        left.push(`${n.id} ${prop} (${kind})`);
        continue;
      }
      const key = n.type + '\u0000' + prop;
      const g = groups.get(key) ?? { type: n.type, prop, ids: [] };
      if (!g.ids.includes(n.id)) {
        g.ids.push(n.id);
      }
      groups.set(key, g);
    }
  }
  if (missing.length) {
    return { error: `Not in this domain's tree (variables cannot be turned into Cypher): ${missing.join(', ')}` };
  }
  if (groups.size === 0) {
    return { error: 'The selection has no number values to plot.' };
  }

  const agg = q.aggregation ?? 'auto';
  const unit = { minute: '"m"', hour: '"h"', day: '"d"', auto: '$bucket' }[agg as string];
  const ret = unit
    ? `relative(v.Timestamp, ${unit}) AS time, avg(v.Value) AS value` +
      (q.includeMinMax !== false ? ', min(v.Value) AS min, max(v.Value) AS max' : '')
    : 'v.Timestamp AS time, v.Value AS value';
  const parts = Array.from(groups.values()).map(
    (g) =>
      `MATCH (n:${g.type}) WHERE n.entityId IN [${g.ids.map(cypherString).join(', ')}]\n` +
      `UNWIND n.${g.prop} AS v\n` +
      `WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)\n` +
      `RETURN n.entityId AS entity, ${cypherString(g.prop)} AS property, ${ret}`
  );
  const head = [
    `// History: one read per entity type and property (Atlas reads stored values only for a typed MATCH).`,
    unit === '$bucket'
      ? '// $from, $to and $bucket follow the time picker; use "m", "h" or "d" instead of $bucket for fixed buckets.'
      : '// $from and $to follow the time picker.',
  ];
  if (left.length) {
    head.push(`// Left out, not numbers: ${left.join(', ')}`);
  }
  return { cypher: head.join('\n') + '\n' + parts.join('\nUNION ALL\n') };
}

import { expandSelection, shortType } from './tree';
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
      if (q.queryType === 'values' && q.selection?.length) {
        // A selection needs the tree for nested and type picks: see valuesCypher.
        return undefined;
      }
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
 * What "Edit as Cypher" needs to know about dashboard variables. A variable
 * stays a variable in the Cypher (the plugin sends it as a parameter); its
 * current values only decide which entity types and properties to read.
 */
export interface CypherVars {
  /** The text with its variables replaced by their current values, one result per value. */
  values(text: string): string[];
  /** Whether the variable can hold several values (multi-value or All); it is then sent as a list. */
  isMulti(name: string): boolean;
}

const NO_VARS: CypherVars = { values: (t) => [t], isMulti: () => false };

/** "before$nameafter" with one variable; undefined when there is none. */
interface VarText {
  before: string;
  name: string;
  after: string;
}

const VAR = /^(.*?)(?:\$\{([A-Za-z0-9_]+)(?::[^}]*)?\}|\$([A-Za-z0-9_]+)|\[\[([A-Za-z0-9_]+)\]\])(.*)$/;

function parseVar(text: string | undefined): VarText | undefined {
  const m = text ? VAR.exec(text) : null;
  return m ? { before: m[1], name: m[2] ?? m[3] ?? m[4], after: m[5] } : undefined;
}

function hasVar(text: string | undefined): boolean {
  return !!parseVar(text);
}

/** The variable as a Cypher list: $name, or [$name] for a single-value variable. */
function varList(name: string, vars: CypherVars): string {
  return vars.isMulti(name) ? `$${name}` : `[$${name}]`;
}

/**
 * Cypher list of ids for a pick like "$machine-Production":
 * [m IN $machine | m + "-Production"]. Live-checked on Atlas 2.0.
 */
function idListExpr(v: VarText, vars: CypherVars): string {
  const list = varList(v.name, vars);
  if (!v.before && !v.after) {
    return list;
  }
  const parts = [v.before && cypherString(v.before), 'm', v.after && cypherString(v.after)].filter(Boolean);
  return `[m IN ${list} | ${parts.join(' + ')}]`;
}

/** An instance pick that can be written as Cypher: literal ids, or an id list built from a variable. */
interface IdSource {
  ids: string[]; // literal ids, or the variable's current ids
  expr?: string; // Cypher list from a variable
}

function idSource(instance: string, vars: CypherVars): IdSource | { error: string } {
  const v = parseVar(instance);
  if (!v) {
    return { ids: [instance] };
  }
  if (hasVar(v.before) || hasVar(v.after)) {
    return { error: `Only one variable per instance can be written as Cypher: ${instance}` };
  }
  return { ids: vars.values(instance), expr: idListExpr(v, vars) };
}

/**
 * The Cypher for a Current selection. One whole type gives MATCH (n:Type),
 * which follows new instances; anything else is the picked instances by id
 * (whole nodes: Cypher returns every property, not only picked ones). An id
 * with a variable stays a variable.
 */
export function valuesCypher(
  q: AtlasQuery,
  nodes: TreeNodeData[],
  vars: CypherVars = NO_VARS
): { cypher?: string; error?: string } {
  const sel = q.selection ?? [];
  const types = new Set(sel.map((p) => (p.scope === 'type' && !p.property && p.type ? shortType(p.type) : '')));
  const only = [...types][0];
  if (sel.length > 0 && types.size === 1 && only && IDENT.test(only)) {
    return { cypher: `MATCH (n:${only})\nRETURN n` };
  }
  const ids: string[] = [];
  const exprs: string[] = [];
  const outside: string[] = [];
  for (const p of expandSelection(sel, nodes)) {
    if (hasVar(p.instance)) {
      if (p.scope) {
        return { error: `A nested or type pick with a variable cannot be written as Cypher: ${p.instance}` };
      }
      const src = idSource(p.instance, vars);
      if ('error' in src) {
        return src;
      }
      if (!exprs.includes(src.expr!)) {
        exprs.push(src.expr!);
      }
    } else if (!nodes.some((n) => n.id === p.instance)) {
      outside.push(p.instance);
    } else if (!ids.includes(p.instance)) {
      ids.push(p.instance);
    }
  }
  if (outside.length) {
    return { error: `Not in this domain's tree: ${outside.join(', ')}` };
  }
  const conds = [
    ...(ids.length ? [`n.entityId IN [${ids.map(cypherString).join(', ')}]`] : []),
    ...exprs.map((e) => `n.entityId IN ${e}`),
  ];
  if (conds.length === 0) {
    return { error: 'No instance matches the selection.' };
  }
  return { cypher: `MATCH (n)\nWHERE ${conds.join('\n   OR ')}\nRETURN n` };
}

interface ReadGroup {
  type: string;
  prop: string;
  ids: string[];
  expr?: string; // ids from a variable
  guard?: string; // "FeedRate" IN $prop, for a property from a variable
}

/**
 * The Cypher for a History selection, using the browse tree for each
 * instance's type and live properties. Atlas only reads stored values when
 * the MATCH names the type, so there is one read per type and property,
 * joined with UNION ALL into long rows (entity, property, time, value) for
 * the "Time series" output format. Auto aggregation uses $bucket, which the
 * plugin fills from the time range like the builder does.
 *
 * Variables stay variables. An instance like "$machine-Production" becomes
 * n.entityId IN [m IN $machine | m + "-Production"], typed by the instances
 * it gives now. A property variable cannot pick the property itself (Atlas
 * answers n[$prop] wrongly), so each property it could name gets its own
 * read, switched on by "Prop" IN $prop.
 */
export function historyCypher(
  q: AtlasQuery,
  nodes: TreeNodeData[],
  vars: CypherVars = NO_VARS
): { cypher?: string; error?: string } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const groups = new Map<string, ReadGroup>();
  const missing: string[] = [];
  const left: string[] = [];

  const add = (n: TreeNodeData, propText: string | undefined, expr?: string) => {
    const live = n.live ?? [];
    const pv = parseVar(propText);
    let props: string[];
    let guard: ((p: string) => string) | undefined;
    if (pv && !pv.before && !pv.after) {
      // Every number property the variable could name.
      props = live.filter((l) => PLOTTABLE.has(l.kind)).map((l) => l.name);
      guard = (p) => `${cypherString(p)} IN ${varList(pv.name, vars)}`;
    } else if (pv) {
      left.push(`${n.id} ${propText} (a property variable must be the whole name)`);
      return;
    } else {
      props = propText ? [propText] : live.map((p) => p.name);
    }
    for (const prop of props) {
      const kind = live.find((p) => p.name === prop)?.kind ?? 'unknown';
      if (!PLOTTABLE.has(kind) || !IDENT.test(n.type) || !IDENT.test(prop)) {
        left.push(`${n.id} ${prop} (${kind})`);
        continue;
      }
      const g0: ReadGroup = { type: n.type, prop, ids: [], expr, guard: guard?.(prop) };
      const key = [n.type, prop, expr ?? '', g0.guard ?? ''].join('\u0000');
      const g = groups.get(key) ?? g0;
      if (!expr && !g.ids.includes(n.id)) {
        g.ids.push(n.id);
      }
      groups.set(key, g);
    }
  };

  for (const pick of expandSelection(q.selection ?? [], nodes)) {
    if (hasVar(pick.instance)) {
      if (pick.scope) {
        return { error: `A nested or type pick with a variable cannot be written as Cypher: ${pick.instance}` };
      }
      const src = idSource(pick.instance, vars);
      if ('error' in src) {
        return src;
      }
      // The variable's current instances give the types to read.
      const found = src.ids.map((id) => byId.get(id)).filter((n): n is TreeNodeData => !!n);
      if (found.length === 0) {
        missing.push(`${pick.instance} (now ${src.ids.join(', ') || 'empty'})`);
        continue;
      }
      const seenType = new Map<string, TreeNodeData[]>();
      found.forEach((n) => seenType.set(n.type, [...(seenType.get(n.type) ?? []), n]));
      for (const sameType of seenType.values()) {
        // One read per property any of them has.
        const merged: TreeNodeData = {
          ...sameType[0],
          live: Array.from(new Map(sameType.flatMap((n) => n.live ?? []).map((l) => [l.name, l])).values()),
        };
        add(merged, pick.property, src.expr);
      }
      continue;
    }
    const n = byId.get(pick.instance);
    if (!n) {
      missing.push(pick.instance);
      continue;
    }
    add(n, pick.property);
  }
  if (missing.length) {
    return { error: `Not in this domain's tree: ${missing.join(', ')}` };
  }
  if (groups.size === 0) {
    return { error: 'The selection has no number values to plot.' };
  }

  const agg = q.aggregation ?? 'auto';
  const unit = { minute: '"m"', hour: '"h"', day: '"d"', auto: '$bucket' }[agg as string];
  const ret = unit
    ? `relative(v.Timestamp, ${unit}) AS time, avg(v.Value) AS value` +
      (q.includeMinMax ? ', min(v.Value) AS min, max(v.Value) AS max' : '')
    : 'v.Timestamp AS time, v.Value AS value';
  const parts = Array.from(groups.values()).map(
    (g) =>
      `MATCH (n:${g.type}) WHERE n.entityId IN ${g.expr ?? `[${g.ids.map(cypherString).join(', ')}]`}\n` +
      `UNWIND n.${g.prop} AS v\n` +
      `WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)` +
      (g.guard ? ` AND ${g.guard}` : '') +
      `\nRETURN n.entityId AS entity, ${cypherString(g.prop)} AS property, ${ret}`
  );
  const head = [
    `// History: one read per entity type and property (Atlas reads stored values only for a typed MATCH).`,
    ...(unit
      ? ['// Cypher avg() is a plain mean; the builder time-weights its averages (and Auto keeps stops and spikes).']
      : []),
    unit === '$bucket'
      ? '// $from, $to and $bucket follow the time picker; use "m", "h" or "d" instead of $bucket for fixed buckets.'
      : '// $from and $to follow the time picker.',
  ];
  if (Array.from(groups.values()).some((g) => g.expr || g.guard)) {
    head.push(
      '// Dashboard variables stay variables. Entity types come from their current values; a property variable switches each read on or off.'
    );
  }
  if (left.length) {
    head.push(`// Left out, not numbers: ${left.join(', ')}`);
  }
  return { cypher: head.join('\n') + '\n' + parts.join('\nUNION ALL\n') };
}

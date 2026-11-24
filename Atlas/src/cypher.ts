import { expandSelection, shortType, subtreeIds } from './tree';
import { AtlasQuery, CypherWeighting, TreeNodeData } from './types';

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
export function toCypher(q: AtlasQuery, vars: CypherVars = NO_VARS): string | undefined {
  switch (q.queryType ?? 'graph') {
    case 'graph': {
      const limit = q.limit && q.limit > 0 ? q.limit : 2000;
      const rels = relationshipFilter(q.relationships, vars);
      if (!q.instanceId) {
        return `MATCH (a)-[r]->(b)\n${rels ? `WHERE ${rels}\n` : ''}RETURN a, r, b\nLIMIT ${limit}`;
      }
      if ((q.depth ?? 1) > 1) {
        return undefined;
      }
      // A start variable stays a variable (several values: around all of them).
      const v = parseVar(q.instanceId);
      const where = v ? `a.entityId IN ${idListExpr(v, vars)}` : `a.entityId = ${cypherString(q.instanceId)}`;
      return `MATCH (a)-[r]-(b)\nWHERE ${where}${rels ? ` AND ${rels}` : ''}\nRETURN a, r, b\nLIMIT ${limit}`;
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

/** The deepest nesting in the tree, as the bound for a variable-length containment match. */
function treeDepth(nodes: TreeNodeData[]): number {
  const parent = new Map(nodes.map((n) => [n.id, n.parentId]));
  let deepest = 1;
  for (const n of nodes) {
    let d = 0;
    for (let cur = n.parentId; cur && d < 50; cur = parent.get(cur)) {
      d++;
    }
    deepest = Math.max(deepest, d);
  }
  return deepest;
}

/**
 * MATCH clauses for "everything nested under the instances of a variable",
 * by entity type, as the tree resolves them now. Atlas has no zero-length
 * match, so the variable's own instances get their own clause; what is
 * nested under them is reached through containment relationships only
 * (live-checked on Atlas 2.0: a typed variable-length match reads stored
 * values like a match by id).
 */
function nestedMatches(
  rootIds: string[],
  expr: string,
  nestedType: string | undefined,
  nodes: TreeNodeData[],
  containment: string[]
): Array<{ match: string; type: string; members: TreeNodeData[] }> | { error: string } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const want = nestedType ? shortType(nestedType) : undefined;
  const self = new Map<string, TreeNodeData[]>();
  const below = new Map<string, TreeNodeData[]>();
  for (const r of rootIds.filter((id) => byId.has(id))) {
    for (const id of subtreeIds(nodes, r)) {
      const n = byId.get(id)!;
      if (want && n.type !== want) {
        continue;
      }
      const into = id === r ? self : below;
      into.set(n.type, [...(into.get(n.type) ?? []), n]);
    }
  }
  if (below.size > 0 && containment.length === 0) {
    return {
      error:
        'The containment relationship types of this domain are not known, so "nested under" cannot be written as Cypher.',
    };
  }
  const rels = containment.join('|');
  const depth = treeDepth(nodes);
  const out: Array<{ match: string; type: string; members: TreeNodeData[] }> = [];
  self.forEach((members, type) => out.push({ type, members, match: `MATCH (n:${type}) WHERE n.entityId IN ${expr}` }));
  below.forEach((members, type) =>
    out.push({ type, members, match: `MATCH (m)-[:${rels}*1..${depth}]->(n:${type}) WHERE m.entityId IN ${expr}` })
  );
  return out;
}

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

/**
 * The Graph's relationship filter as Cypher: type(r) IN [...] for picked
 * type URIs, + $rels for a relationship-type variable. type(r) returns the
 * full URI (live-checked). Empty when every relationship is followed.
 */
function relationshipFilter(rels: string[] | undefined, vars: CypherVars): string {
  const list = (rels ?? []).filter((r) => r.trim());
  if (list.length === 0) {
    return '';
  }
  const literal = list.filter((r) => !parseVar(r));
  const parts = [
    ...(literal.length ? [`[${literal.map(cypherString).join(', ')}]`] : []),
    ...list.map(parseVar).flatMap((v) => (v && !v.before && !v.after ? [varList(v.name, vars)] : [])),
  ];
  return `type(r) IN ${parts.join(' + ')}`;
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
  vars: CypherVars = NO_VARS,
  containment: string[] = []
): { cypher?: string; error?: string } {
  const sel = q.selection ?? [];
  const types = new Set(sel.map((p) => (p.scope === 'type' && !p.property && p.type ? shortType(p.type) : '')));
  const only = [...types][0];
  if (sel.length > 0 && types.size === 1 && only && IDENT.test(only)) {
    return { cypher: `MATCH (n:${only})\nRETURN n` };
  }
  const ids: string[] = [];
  const exprs: string[] = [];
  const nested: string[] = [];
  const outside: string[] = [];
  for (const p of expandSelection(sel, nodes)) {
    if (hasVar(p.instance)) {
      if (p.scope === 'type') {
        return { error: `A type pick with a variable cannot be written as Cypher: ${p.type}` };
      }
      const src = idSource(p.instance, vars);
      if ('error' in src) {
        return src;
      }
      if (p.scope === 'nested') {
        const m = nestedMatches(src.ids, src.expr!, p.nestedType, nodes, containment);
        if ('error' in m) {
          return m;
        }
        m.forEach((x) => !nested.includes(x.match) && nested.push(x.match));
        continue;
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
  if (conds.length === 0 && nested.length === 0) {
    return { error: 'No instance matches the selection.' };
  }
  const parts = [
    ...(conds.length ? [`MATCH (n)\nWHERE ${conds.join('\n   OR ')}\nRETURN n`] : []),
    ...nested.map((m) => `${m}\nRETURN n`),
  ];
  return { cypher: parts.join('\nUNION ALL\n') };
}

interface ReadGroup {
  type: string;
  prop: string;
  ids: string[];
  match?: string; // MATCH ... WHERE for a variable pick; literal ids otherwise
  part?: string; // a record's numeric part, read raw like the Trend does
  guard?: string; // "FeedRate" IN $prop, for a property from a variable
}

/**
 * The Cypher for a History selection, using the browse tree for each
 * instance's type and live properties. Atlas only reads stored values when
 * the MATCH names the type, so there is one read per type and property,
 * joined with UNION ALL into long rows (entity, property, time, value) for
 * the "Time series" output format. Values are read raw, and the query's
 * weighting (set from the aggregation) has the plugin time-weight them with
 * the Data view's own code, so the panel looks the same in both modes.
 *
 * Variables stay variables. An instance like "$machine-Production" becomes
 * n.entityId IN [m IN $machine | m + "-Production"], typed by the instances
 * it gives now. A property variable cannot pick the property itself (Atlas
 * answers n[$prop] wrongly), so each property it could name gets its own
 * read, switched on by "Prop" IN $prop.
 */
const WEIGHT_LABEL: Record<CypherWeighting, string> = {
  none: 'Off',
  auto: 'Auto',
  minute: 'Minute',
  hour: 'Hour',
  day: 'Day',
};

/** The Cypher weighting that draws a Trend query's aggregation the same way. */
export function cypherWeighting(q: AtlasQuery): CypherWeighting {
  const agg = q.aggregation ?? 'auto';
  return agg === 'raw' ? 'none' : agg;
}

export function historyCypher(
  q: AtlasQuery,
  nodes: TreeNodeData[],
  vars: CypherVars = NO_VARS,
  containment: string[] = []
): { cypher?: string; error?: string } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const groups = new Map<string, ReadGroup>();
  const missing: string[] = [];
  const left: string[] = [];
  const enums = new Set<string>();

  // match: the MATCH ... WHERE of a variable pick, whose instances (merged into n) only
  // contribute properties they have.
  const add = (n: TreeNodeData, propText: string | undefined, match?: string) => {
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
      if (match && !live.some((l) => l.name === prop)) {
        continue; // none of the variable's instances of this type has it
      }
      const lp = live.find((p) => p.name === prop);
      const kind = lp?.kind ?? 'unknown';
      if (kind === 'record' && IDENT.test(n.type) && IDENT.test(prop)) {
        // The Trend reads a record raw, one column per part; its number parts plot.
        const parts = lp?.parts ?? [];
        for (const part of parts) {
          if (part.kind !== 'number' || !IDENT.test(part.name)) {
            left.push(`${n.id} ${prop}.${part.name} (${part.kind})`);
            continue;
          }
          const key = [n.type, prop, part.name, match ?? '', guard?.(prop) ?? ''].join('\u0000');
          const g = groups.get(key) ?? { type: n.type, prop, part: part.name, ids: [], match, guard: guard?.(prop) };
          if (!match && !g.ids.includes(n.id)) {
            g.ids.push(n.id);
          }
          groups.set(key, g);
        }
        if (parts.length === 0) {
          left.push(`${n.id} ${prop} (record with no known parts)`);
        }
        continue;
      }
      if (!PLOTTABLE.has(kind) || !IDENT.test(n.type) || !IDENT.test(prop)) {
        left.push(`${n.id} ${prop} (${kind})`);
        continue;
      }
      if (lp?.enum) {
        enums.add(`${prop} (${lp.enum.label})`);
      }
      const g0: ReadGroup = { type: n.type, prop, ids: [], match, guard: guard?.(prop) };
      const key = [n.type, prop, match ?? '', g0.guard ?? ''].join('\u0000');
      const g = groups.get(key) ?? g0;
      if (!match && !g.ids.includes(n.id)) {
        g.ids.push(n.id);
      }
      groups.set(key, g);
    }
  };

  for (const pick of expandSelection(q.selection ?? [], nodes)) {
    if (hasVar(pick.instance)) {
      if (pick.scope === 'type') {
        return { error: `A type pick with a variable cannot be written as Cypher: ${pick.type}` };
      }
      const src = idSource(pick.instance, vars);
      if ('error' in src) {
        return src;
      }
      const merge = (members: TreeNodeData[]): TreeNodeData => ({
        ...members[0],
        live: Array.from(new Map(members.flatMap((n) => n.live ?? []).map((l) => [l.name, l])).values()),
      });
      if (pick.scope === 'nested') {
        const m = nestedMatches(src.ids, src.expr!, pick.nestedType, nodes, containment);
        if ('error' in m) {
          return m;
        }
        if (m.length === 0) {
          missing.push(`${pick.instance} (nothing nested of that type now)`);
        }
        m.forEach((x) => add(merge(x.members), pick.property, x.match));
        continue;
      }
      // The variable's current instances give the types to read.
      const found = src.ids.map((id) => byId.get(id)).filter((n): n is TreeNodeData => !!n);
      if (found.length === 0) {
        missing.push(`${pick.instance} (now ${src.ids.join(', ') || 'empty'})`);
        continue;
      }
      const seenType = new Map<string, TreeNodeData[]>();
      found.forEach((n) => seenType.set(n.type, [...(seenType.get(n.type) ?? []), n]));
      seenType.forEach((sameType, type) =>
        // One read per property any of them has.
        add(merge(sameType), pick.property, `MATCH (n:${type}) WHERE n.entityId IN ${src.expr}`)
      );
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

  const weighting = cypherWeighting(q);
  const ret = 'v.Timestamp AS time, v.Value AS value';
  const partRet = (p: string) => `v.Timestamp AS time, v.${p} AS value`;
  const parts = Array.from(groups.values()).map((g) =>
    g.part
      ? (g.match ?? `MATCH (n:${g.type}) WHERE n.entityId IN [${g.ids.map(cypherString).join(', ')}]`) +
        '\n' +
        `UNWIND n.${g.prop} AS v\n` +
        `WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)` +
        (g.guard ? ` AND ${g.guard}` : '') +
        `\nRETURN n.entityId AS entity, ${cypherString(g.prop + '.' + g.part)} AS property, ${partRet(g.part)}`
      : (g.match ?? `MATCH (n:${g.type}) WHERE n.entityId IN [${g.ids.map(cypherString).join(', ')}]`) +
        '\n' +
        `UNWIND n.${g.prop} AS v\n` +
        `WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)` +
        (g.guard ? ` AND ${g.guard}` : '') +
        `\nRETURN n.entityId AS entity, ${cypherString(g.prop)} AS property, ${ret}`
  );
  const head = [
    `// History: one read per entity type and property (Atlas reads stored values only for a typed MATCH).`,
    weighting === 'none'
      ? '// Every stored value, as returned.'
      : `// Stored values; the plugin time-weights them like the Data view (Time-weight: ${WEIGHT_LABEL[weighting]}).`,
    '// $from and $to follow the time picker.',
  ];
  if (Array.from(groups.values()).some((g) => g.match || g.guard)) {
    head.push(
      '// Dashboard variables stay variables. Entity types come from their current values; a property variable switches each read on or off.'
    );
  }
  if (Array.from(groups.values()).some((g) => g.match?.includes('*1..'))) {
    head.push('// "Nested under" follows containment relationships only, as the instance tree does.');
  }
  if (left.length) {
    head.push(`// Left out, not numbers: ${left.join(', ')}`);
  }
  if (enums.size) {
    head.push(`// Enumerations are shown by label and never time-weighted: ${Array.from(enums).join(', ')}.`);
  }
  if (Array.from(groups.values()).some((g) => g.part)) {
    head.push('// Record parts (e.g. LiveAsset.MxLocationId) are read raw, one value per stored version.');
  }
  return { cypher: head.join('\n') + '\n' + parts.join('\nUNION ALL\n') };
}

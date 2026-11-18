import { AtlasQuery } from './types';

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

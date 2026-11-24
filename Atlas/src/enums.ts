import { EnumInfo, RelationshipColor } from './types';

/**
 * Enumeration keys come to Grafana as text; Atlas stores them typed. A
 * Cypher parameter from an "Enumeration members" variable is sent with the
 * key type, so `v.Value IN $mode` matches stored numbers (live-checked:
 * `IN ["1"]` matches nothing on a double, `IN [1]` does).
 */
export function typedKeys(value: unknown, keyKind?: EnumInfo['keyKind']): unknown {
  const one = (v: unknown): unknown => {
    if (typeof v !== 'string') {
      return v;
    }
    if (keyKind === 'number') {
      const n = Number(v);
      return v.trim() !== '' && Number.isFinite(n) ? n : v;
    }
    if (keyKind === 'boolean' && (v === 'true' || v === 'false')) {
      return v === 'true';
    }
    return v;
  };
  return Array.isArray(value) ? value.map(one) : one(value);
}

/** The variable's options: member labels shown, keys as the values. */
export function enumOptions(e: EnumInfo | undefined): { text: string[]; value: string[] } {
  const members = e?.members ?? [];
  return { text: members.map((m) => m.label), value: members.map((m) => m.key) };
}

/** Relationship-type variable options: names shown, type URIs as values; only the offered types when some are picked. */
export function relationshipOptions(
  types: RelationshipColor[],
  offered?: string[]
): { text: string[]; value: string[] } {
  const keep = offered?.length ? new Set(offered) : undefined;
  const shown = types.filter((t) => !keep || keep.has(t.uri));
  return { text: shown.map((t) => t.name), value: shown.map((t) => t.uri) };
}

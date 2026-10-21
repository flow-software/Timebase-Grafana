import { ObjectOption } from './types';

/** Picked values per field key. A key with no values picked does not narrow anything. */
export type FieldPicks = Record<string, string[]>;

export interface FieldFacet {
  key: string;
  /** Each value with the number of tags that have it, given the other keys' picks. */
  values: Array<{ value: string; count: number }>;
}

/**
 * True if the tag has one of the picked values for every key that has
 * picks. Keys compare without regard to case, as the collector treats them.
 * exceptKey is left out, so a key's own options can be counted.
 */
export function matchesPicks(
  fields: Record<string, string> | undefined,
  picks: FieldPicks,
  exceptKey?: string
): boolean {
  for (const [key, values] of Object.entries(picks)) {
    if (values.length === 0 || key === exceptKey) {
      continue;
    }
    const value = fieldValue(fields, key);
    if (value === undefined || !values.includes(value)) {
      return false;
    }
  }
  return true;
}

/**
 * The field keys used by the tags, each with the values it takes. Values are
 * counted over the tags that pass the picks on the other keys, so choosing
 * Site narrows the Area options to that site. A picked value stays listed
 * (with 0) so it can always be unpicked.
 */
export function fieldFacets(tags: ObjectOption[], picks: FieldPicks): FieldFacet[] {
  const keys = new Map<string, string>(); // lower-case -> first spelling seen
  for (const t of tags) {
    for (const k of Object.keys(t.fields ?? {})) {
      if (!keys.has(k.toLowerCase())) {
        keys.set(k.toLowerCase(), k);
      }
    }
  }

  return [...keys.values()].sort(collator.compare).map((key) => {
    const counts = new Map<string, number>();
    for (const t of tags) {
      const value = fieldValue(t.fields, key);
      if (value !== undefined && matchesPicks(t.fields, picks, key)) {
        counts.set(value, (counts.get(value) ?? 0) + 1);
      }
    }
    for (const v of picks[key] ?? []) {
      if (!counts.has(v)) {
        counts.set(v, 0);
      }
    }
    const values = [...counts]
      .map(([value, count]) => ({ value, count }))
      .sort((a, b) => collator.compare(a.value, b.value));
    return { key, values };
  });
}

function fieldValue(fields: Record<string, string> | undefined, key: string): string | undefined {
  if (!fields) {
    return undefined;
  }
  if (key in fields) {
    return fields[key];
  }
  const lower = key.toLowerCase();
  const found = Object.keys(fields).find((k) => k.toLowerCase() === lower);
  return found === undefined ? undefined : fields[found];
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

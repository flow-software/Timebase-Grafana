import { FieldPicks } from './fieldFacets';

// The tree browser's field filters, kept per data source for the browser
// session, so they survive closing the browser and moving to another panel.
// Storage can be missing or blocked (private windows, previews); the filters
// then simply start empty.
const PREFIX = 'timebase-datasource.treeFilters.';

export function loadFieldPicks(datasourceUid: string): FieldPicks {
  try {
    const raw = window.sessionStorage.getItem(PREFIX + datasourceUid);
    return raw ? sanitize(JSON.parse(raw)) : {};
  } catch {
    return {};
  }
}

export function saveFieldPicks(datasourceUid: string, picks: FieldPicks): void {
  try {
    if (Object.keys(picks).length === 0) {
      window.sessionStorage.removeItem(PREFIX + datasourceUid);
    } else {
      window.sessionStorage.setItem(PREFIX + datasourceUid, JSON.stringify(picks));
    }
  } catch {
    // Not kept; the filters still work for this editor.
  }
}

// Keeps only { key: string[] } entries, whatever was stored.
function sanitize(value: unknown): FieldPicks {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {};
  }
  const out: FieldPicks = {};
  for (const [key, values] of Object.entries(value)) {
    if (Array.isArray(values)) {
      out[key] = values.filter((v): v is string => typeof v === 'string');
    }
  }
  return out;
}

import { AtlasQuery, HistoryPick, LiveProperty, TreeNodeData } from './types';

export interface TreeItem {
  node: TreeNodeData;
  children: TreeItem[];
}

/** Display name: the Atlas label, or the id when the instance has none. */
export function nodeName(n: TreeNodeData): string {
  return n.label || n.id;
}

/**
 * Nests the flat list from the backend by parentId. The backend sends it
 * sorted by name, and that order is kept at every level. An instance whose
 * parent is missing becomes a root, so nothing is hidden.
 */
export function buildTree(nodes: TreeNodeData[]): TreeItem[] {
  const items = new Map<string, TreeItem>();
  for (const n of nodes) {
    items.set(n.id, { node: n, children: [] });
  }
  const roots: TreeItem[] = [];
  for (const n of nodes) {
    const item = items.get(n.id)!;
    const parent = n.parentId ? items.get(n.parentId) : undefined;
    if (parent && parent !== item) {
      parent.children.push(item);
    } else {
      roots.push(item);
    }
  }
  return roots;
}

/** Ids of every ancestor of id, nearest first. */
export function ancestorsOf(nodes: TreeNodeData[], id: string): string[] {
  const parentOf = new Map(nodes.map((n) => [n.id, n.parentId]));
  const out: string[] = [];
  const seen = new Set<string>([id]);
  for (let p = parentOf.get(id); p && !seen.has(p); p = parentOf.get(p)) {
    out.push(p);
    seen.add(p);
  }
  return out;
}

/**
 * Keeps the instances whose label, id or type contains the text, plus their
 * ancestors so each match is shown in place. Returns the trimmed tree and
 * the number of matches.
 */
export function filterTree(roots: TreeItem[], text: string): { roots: TreeItem[]; matches: number } {
  const q = text.trim().toLowerCase();
  if (!q) {
    return { roots, matches: 0 };
  }
  let matches = 0;
  const walk = (item: TreeItem): TreeItem | undefined => {
    const children = item.children.map(walk).filter((c): c is TreeItem => !!c);
    const n = item.node;
    const self = [n.label ?? '', n.id, n.type].some((s) => s.toLowerCase().includes(q));
    if (self) {
      matches++;
    }
    return self || children.length > 0 ? { node: n, children } : undefined;
  };
  return { roots: roots.map(walk).filter((c): c is TreeItem => !!c), matches };
}

/** A tree row for picking History series: an instance with the live properties to show under it. */
export interface PickItem {
  node: TreeNodeData;
  props: LiveProperty[];
  children: PickItem[];
}

/**
 * The tree for the History picker. With onlyLive, branches without any live
 * property are left out. A filter keeps instances whose label, id or type
 * matches (with all their properties) and properties whose name matches
 * (with their instance), plus ancestors so everything shows in place.
 */
export function pickTree(roots: TreeItem[], text: string, onlyLive: boolean): { roots: PickItem[]; matches: number } {
  const q = text.trim().toLowerCase();
  let matches = 0;
  const walk = (item: TreeItem): PickItem | undefined => {
    const children = item.children.map(walk).filter((c): c is PickItem => !!c);
    const n = item.node;
    const live = n.live ?? [];
    let props = live;
    let keepSelf = !onlyLive || live.length > 0;
    if (q) {
      const self = [n.label ?? '', n.id, n.type].some((s) => s.toLowerCase().includes(q));
      if (!self) {
        props = live.filter((p) => p.name.toLowerCase().includes(q));
      }
      keepSelf = (self && keepSelf) || props.length > 0;
      if (keepSelf) {
        matches++;
      }
    }
    return keepSelf || children.length > 0 ? { node: n, props, children } : undefined;
  };
  return { roots: roots.map(walk).filter((c): c is PickItem => !!c), matches };
}

/** Ids of the item and everything under it that has live properties. */
export function liveDescendants(item: PickItem): string[] {
  const out: string[] = [];
  const walk = (i: PickItem) => {
    if ((i.node.live?.length ?? 0) > 0) {
      out.push(i.node.id);
    }
    i.children.forEach(walk);
  };
  walk(item);
  return out;
}

/** Ids of an instance and everything nested under it, parent first, in list order. */
export function subtreeIds(nodes: TreeNodeData[], id: string): string[] {
  const kids = new Map<string, string[]>();
  for (const n of nodes) {
    if (n.parentId) {
      kids.set(n.parentId, [...(kids.get(n.parentId) ?? []), n.id]);
    }
  }
  const out: string[] = [];
  const seen = new Set<string>();
  const walk = (i: string) => {
    if (seen.has(i)) {
      return;
    }
    seen.add(i);
    out.push(i);
    (kids.get(i) ?? []).forEach(walk);
  };
  walk(id);
  return out;
}

/** The short type name, e.g. CNCProduction for urn:atlas:ontology:x:CNCProduction. */
export function shortType(t: string): string {
  return t.slice(Math.max(t.lastIndexOf(':'), t.lastIndexOf('/'), t.lastIndexOf('#')) + 1);
}

/**
 * Resolves nested and type picks into plain instance picks with the tree, as
 * the backend does at query time. With a property, only instances that have
 * it as a live value are kept.
 */
export function expandSelection(picks: HistoryPick[], nodes: TreeNodeData[]): HistoryPick[] {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const has = (id: string, prop: string) => (byId.get(id)?.live ?? []).some((l) => l.name === prop);
  const out: HistoryPick[] = [];
  for (const p of picks) {
    let ids: string[];
    if (p.scope === 'nested' && byId.has(p.instance)) {
      ids = subtreeIds(nodes, p.instance);
      if (p.nestedType) {
        const t = shortType(p.nestedType);
        ids = ids.filter((id) => byId.get(id)?.type === t);
      }
    } else if (p.scope === 'type' && p.type) {
      const t = shortType(p.type);
      ids = nodes.filter((n) => n.type === t).map((n) => n.id);
    } else {
      out.push(p.scope === 'type' ? p : { instance: p.instance, property: p.property, type: p.type });
      continue;
    }
    for (const id of ids) {
      // A property variable ($prop) is checked when it is resolved.
      if (!p.property || p.property.includes('$') || has(id, p.property)) {
        out.push({ instance: id, property: p.property, type: byId.get(id)?.type });
      }
    }
  }
  return out;
}

/** One entity type for the Types view: its instances and the live properties they have. */
export interface TypeGroup {
  type: string;
  ids: string[];
  /** Live properties of its instances, with how many instances have each. */
  props: Array<LiveProperty & { count: number }>;
}

export function typeGroups(nodes: TreeNodeData[], text: string): TypeGroup[] {
  const q = text.trim().toLowerCase();
  const groups = new Map<string, { ids: string[]; props: Map<string, LiveProperty & { count: number }> }>();
  for (const n of nodes) {
    const g = groups.get(n.type) ?? { ids: [] as string[], props: new Map<string, LiveProperty & { count: number }>() };
    g.ids.push(n.id);
    for (const l of n.live ?? []) {
      const cur = g.props.get(l.name);
      g.props.set(l.name, { ...l, count: (cur?.count ?? 0) + 1 });
    }
    groups.set(n.type, g);
  }
  const out: TypeGroup[] = [];
  for (const [type, g] of groups) {
    let props = Array.from(g.props.values()).sort((a, b) => a.name.localeCompare(b.name));
    if (q && !type.toLowerCase().includes(q)) {
      props = props.filter((p) => p.name.toLowerCase().includes(q));
      if (props.length === 0) {
        continue;
      }
    }
    out.push({ type, ids: g.ids, props });
  }
  return out.sort((a, b) => a.type.localeCompare(b.type));
}

/** The values of a Selection variable: instances (name as text, id as value) or property names. */
export function variableValues(q: AtlasQuery, nodes: TreeNodeData[]): { text: string[]; value: string[] } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const picks = expandSelection(q.selection ?? [], nodes);
  const text: string[] = [];
  const value: string[] = [];
  const seen = new Set<string>();
  const add = (t: string, v: string) => {
    if (v && !seen.has(v)) {
      seen.add(v);
      text.push(t);
      value.push(v);
    }
  };
  for (const p of picks) {
    if (p.scope === 'type') {
      continue; // a type that is not in the tree
    }
    const n = byId.get(p.instance);
    if (q.returns === 'properties') {
      const names = p.property ? [p.property] : (n?.live ?? []).map((l) => l.name);
      names.forEach((name) => add(name, name));
    } else if (n) {
      add(nodeName(n), n.id);
    }
  }
  if (q.returns === 'properties') {
    const order = value.map((v, i) => i).sort((a, b) => value[a].localeCompare(value[b]));
    return { text: order.map((i) => text[i]), value: order.map((i) => value[i]) };
  }
  // Names that repeat ("Production" on every machine) say nothing: show the id instead.
  const count = new Map<string, number>();
  text.forEach((t) => count.set(t, (count.get(t) ?? 0) + 1));
  return { text: text.map((t, i) => ((count.get(t) ?? 0) > 1 ? value[i] : t)), value };
}

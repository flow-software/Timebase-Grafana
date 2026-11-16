import { TreeNodeData } from './types';

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

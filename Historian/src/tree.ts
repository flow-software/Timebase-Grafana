import { ObjectOption } from './types';

export interface TreeNode {
  /** Unique within the tree; stable across rebuilds of the same object list. */
  key: string;
  label: string;
  /** Set for nodes backed by a real i3X object; unset for virtual path folders. */
  elementId?: string;
  isTag: boolean;
  children: TreeNode[];
  /** ElementIds of every tag at or below this node, in tree order. */
  tagIds: string[];
}

/**
 * Builds the browse tree from the flat i3X object list.
 *
 * Two sources of structure are combined:
 *  1. i3X parent links (parentId), the spec's own hierarchy, used by servers
 *     that model real folders and assets.
 *  2. Dotted paths inside ElementIds. Timebase puts every tag directly under
 *     its dataset and keeps the path in the name ("default:Boilers.Boiler #1.
 *     BoilerStatus.Pressure"), so the part after the parent's ElementId is
 *     split on "." into virtual folders.
 * Path splitting only applies when a child's ElementId starts with its
 * parent's, so servers with opaque ids keep their display names intact.
 */
export function buildTree(objects: ObjectOption[]): TreeNode[] {
  const byId = new Map(objects.map((o) => [o.elementId, o]));
  const placed = new Map<string, TreeNode>();
  const root = newNode('', '', false);
  const index = new WeakMap<TreeNode, Map<string, TreeNode>>();

  const child = (parent: TreeNode, label: string, isTag: boolean): TreeNode => {
    let kids = index.get(parent);
    if (!kids) {
      kids = new Map();
      index.set(parent, kids);
    }
    // A folder and a tag may share a name at the same level; keep both.
    const slot = (isTag ? 't:' : 'f:') + label;
    let node = kids.get(slot);
    if (!node) {
      node = newNode(`${parent.key}/${slot}`, label, isTag);
      kids.set(slot, node);
      parent.children.push(node);
    }
    return node;
  };

  const place = (obj: ObjectOption, visiting: Set<string>): TreeNode => {
    const done = placed.get(obj.elementId);
    if (done) {
      return done;
    }
    visiting.add(obj.elementId);

    const parentObj = obj.parentId ? byId.get(obj.parentId) : undefined;
    // A parent link that loops back is treated as no parent.
    const usable = parentObj && !visiting.has(parentObj.elementId) ? parentObj : undefined;
    const parentNode = usable ? place(usable, visiting) : root;

    const segments = pathSegments(obj, usable);
    let cur = parentNode;
    for (const seg of segments.slice(0, -1)) {
      cur = child(cur, seg, false);
    }
    const node = child(cur, segments[segments.length - 1], !obj.isFolder);
    node.elementId = obj.elementId;

    placed.set(obj.elementId, node);
    visiting.delete(obj.elementId);
    return node;
  };

  for (const obj of objects) {
    place(obj, new Set());
  }
  finish(root);
  return root.children;
}

function newNode(key: string, label: string, isTag: boolean): TreeNode {
  return { key, label, isTag, children: [], tagIds: [] };
}

function pathSegments(obj: ObjectOption, parent?: ObjectOption): string[] {
  const id = obj.elementId;
  if (parent && id.length > parent.elementId.length && id.startsWith(parent.elementId)) {
    const rest = id.slice(parent.elementId.length).replace(/^[:./]+/, '');
    const parts = rest.split('.').filter((p) => p !== '');
    if (parts.length > 0) {
      return parts;
    }
  }
  return [obj.displayName || id];
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** Sorts folders before tags, then by name, and fills in tagIds bottom-up. */
function finish(node: TreeNode) {
  node.children.sort((a, b) => Number(a.isTag) - Number(b.isTag) || collator.compare(a.label, b.label));
  node.tagIds = node.isTag && node.elementId ? [node.elementId] : [];
  for (const c of node.children) {
    finish(c);
    node.tagIds.push(...c.tagIds);
  }
}

/**
 * Keeps nodes whose name or ElementId contains the query, plus their
 * ancestors. A matching folder keeps its whole subtree.
 */
export function filterTree(nodes: TreeNode[], query: string): TreeNode[] {
  const q = query.trim().toLowerCase();
  if (!q) {
    return nodes;
  }
  const matches = (n: TreeNode) => n.label.toLowerCase().includes(q) || !!n.elementId?.toLowerCase().includes(q);

  const walk = (list: TreeNode[]): TreeNode[] =>
    list.flatMap((n) => {
      if (matches(n)) {
        return [n];
      }
      const children = walk(n.children);
      if (children.length === 0) {
        return [];
      }
      return [{ ...n, children, tagIds: children.flatMap((c) => c.tagIds) }];
    });
  return walk(nodes);
}

/** Drops folders with no tags at or below them, e.g. datasets a field filter leaves empty. */
export function pruneEmpty(nodes: TreeNode[]): TreeNode[] {
  return nodes
    .filter((n) => n.isTag || n.tagIds.length > 0)
    .map((n) => (n.isTag ? n : { ...n, children: pruneEmpty(n.children) }));
}

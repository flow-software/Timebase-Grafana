import React, { useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Button, Checkbox, FilterInput, Icon, IconButton, InlineSwitch, Modal, useStyles2 } from '@grafana/ui';

import { systemTagMatcher } from '../systemTags';
import { buildTree, filterTree, TreeNode } from '../tree';
import { ObjectOption } from '../types';

interface Props {
  isOpen: boolean;
  objects: ObjectOption[];
  /** The query's current ElementIds, which may include $variables and custom ids. */
  selected: string[];
  onApply: (elementIds: string[]) => void;
  onDismiss: () => void;
}

// Rendering thousands of rows at once makes the modal sluggish; past this
// the user is asked to narrow the search instead.
const MAX_ROWS = 1000;

interface Row {
  node: TreeNode;
  depth: number;
}

export function TagBrowser({ isOpen, objects, selected, onApply, onDismiss }: Props) {
  const styles = useStyles2(getStyles);
  const systemIds = useMemo(() => {
    const isSystem = systemTagMatcher(objects);
    return new Set(objects.filter(isSystem).map((o) => o.elementId));
  }, [objects]);
  // Hidden by default; shown from the start if the query already uses one, so it can be seen and unticked.
  const [showSystem, setShowSystem] = useState(() => selected.some((id) => systemIds.has(id)));
  const tree = useMemo(
    () => buildTree(showSystem ? objects : objects.filter((o) => !systemIds.has(o.elementId))),
    [objects, systemIds, showSystem]
  );
  const byId = useMemo(() => new Map(objects.map((o) => [o.elementId, o])), [objects]);
  const [search, setSearch] = useState('');
  // Top-level nodes (datasets) start open so the first click is useful.
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(tree.map((n) => n.key)));
  const [checked, setChecked] = useState<Set<string>>(() => new Set(selected));

  const visible = useMemo(() => filterTree(tree, search), [tree, search]);
  const searching = search.trim() !== '';

  const rows = useMemo(() => {
    const out: Row[] = [];
    const walk = (nodes: TreeNode[], depth: number) => {
      for (const node of nodes) {
        if (out.length > MAX_ROWS) {
          return;
        }
        out.push({ node, depth });
        // While searching every match is shown, so the tree opens itself.
        if (!node.isTag && (searching || expanded.has(node.key))) {
          walk(node.children, depth + 1);
        }
      }
    };
    walk(visible, 0);
    return out;
  }, [visible, expanded, searching]);

  const toggleExpanded = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });

  const setTags = (ids: string[], on: boolean) =>
    setChecked((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => (on ? next.add(id) : next.delete(id)));
      return next;
    });

  const apply = () => {
    // Keep the existing order, then append new picks in tree order, so
    // panels and legends do not reshuffle when one tag is added.
    const kept = selected.filter((id) => checked.has(id));
    const keptSet = new Set(kept);
    const added = tree.flatMap((n) => n.tagIds).filter((id) => checked.has(id) && !keptSet.has(id));
    // Variables and typed ids are not in the tree; they stay unless unticked here.
    onApply([...kept, ...added]);
  };

  const tagCount = tree.reduce((n, t) => n + t.tagIds.length, 0);
  const pickedInTree = tree.flatMap((n) => n.tagIds).filter((id) => checked.has(id)).length;

  return (
    <Modal title="Tree browser" isOpen={isOpen} onDismiss={onDismiss} className={styles.modal}>
      <div className={styles.toolbar}>
        <div className={styles.search}>
          <FilterInput placeholder="Search tags and folders" value={search} onChange={setSearch} autoFocus />
        </div>
        {systemIds.size > 0 && (
          <InlineSwitch
            id="tree-browser-show-system"
            label={`Show historian system tags (${systemIds.size})`}
            showLabel
            transparent
            value={showSystem}
            onChange={(e) => setShowSystem(e.currentTarget.checked)}
          />
        )}
      </div>

      <div className={styles.list} role="tree" aria-label="Tags">
        {rows.length === 0 && (
          <div className={styles.empty}>{tagCount === 0 ? 'The server lists no tags.' : 'No tags match.'}</div>
        )}
        {rows.slice(0, MAX_ROWS).map(({ node, depth }) => {
          const all = node.tagIds.length > 0 && node.tagIds.every((id) => checked.has(id));
          const some = !all && node.tagIds.some((id) => checked.has(id));
          const isOpenNode = searching || expanded.has(node.key);
          const info = node.elementId ? byId.get(node.elementId) : undefined;
          return (
            <div
              key={node.key}
              role="treeitem"
              aria-level={depth + 1}
              aria-expanded={node.isTag ? undefined : isOpenNode}
              aria-selected={all}
              className={styles.row}
              style={{ paddingLeft: depth * 20 }}
              title={[node.elementId, info?.description].filter(Boolean).join(' · ') || undefined}
            >
              {node.isTag ? (
                <span className={styles.chevronSpace} />
              ) : (
                <IconButton
                  name={isOpenNode ? 'angle-down' : 'angle-right'}
                  aria-label={isOpenNode ? `Collapse ${node.label}` : `Expand ${node.label}`}
                  onClick={() => toggleExpanded(node.key)}
                  disabled={searching}
                />
              )}
              <Checkbox
                value={all}
                indeterminate={some}
                disabled={node.tagIds.length === 0}
                aria-label={`Select ${node.label}`}
                onChange={() => setTags(node.tagIds, !all)}
              />
              <Icon name={node.isTag ? 'chart-line' : isOpenNode ? 'folder-open' : 'folder'} className={styles.icon} />
              <span
                className={styles.label}
                onClick={() => (node.isTag ? setTags(node.tagIds, !all) : !searching && toggleExpanded(node.key))}
              >
                {node.label}
                {info?.description && <span className={styles.description}>{info.description}</span>}
              </span>
              {node.isTag && info?.unit && <span className={styles.count}>{info.unit}</span>}
              {!node.isTag && <span className={styles.count}>{node.tagIds.length}</span>}
            </div>
          );
        })}
        {rows.length > MAX_ROWS && (
          <div className={styles.empty}>Showing the first {MAX_ROWS} rows. Search to narrow the list.</div>
        )}
      </div>

      <Modal.ButtonRow
        leftItems={
          <span className={styles.count}>
            {pickedInTree} of {tagCount} tags selected
          </span>
        }
      >
        <Button variant="secondary" fill="outline" onClick={onDismiss}>
          Cancel
        </Button>
        <Button onClick={apply}>Apply</Button>
      </Modal.ButtonRow>
    </Modal>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  modal: css({ width: '760px', maxWidth: '95vw' }),
  toolbar: css({ display: 'flex', alignItems: 'center', gap: theme.spacing(1) }),
  search: css({ flex: 1, minWidth: 0 }),
  list: css({
    marginTop: theme.spacing(1),
    height: '55vh',
    overflowY: 'auto',
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    padding: theme.spacing(0.5, 0),
  }),
  row: css({
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(1),
    minHeight: theme.spacing(4),
    paddingRight: theme.spacing(1),
    '&:hover': { background: theme.colors.action.hover },
  }),
  chevronSpace: css({ width: theme.spacing(2), flexShrink: 0 }),
  icon: css({ color: theme.colors.text.secondary, flexShrink: 0 }),
  label: css({
    cursor: 'pointer',
    flexGrow: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  }),
  count: css({ color: theme.colors.text.secondary, fontSize: theme.typography.bodySmall.fontSize, flexShrink: 0 }),
  description: css({
    marginLeft: theme.spacing(1.5),
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
  }),
  empty: css({ padding: theme.spacing(2), color: theme.colors.text.secondary }),
});

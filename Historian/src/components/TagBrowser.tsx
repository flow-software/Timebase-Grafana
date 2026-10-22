import React, { useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import {
  Button,
  Checkbox,
  Combobox,
  FilterInput,
  Icon,
  IconButton,
  InlineSwitch,
  Modal,
  MultiCombobox,
  useStyles2,
} from '@grafana/ui';

import { FieldFacet, fieldFacets, FieldPicks, matchesPicks } from '../fieldFacets';
import { systemTagMatcher } from '../systemTags';
import { buildTree, filterTree, pruneEmpty, TreeNode } from '../tree';
import { ObjectOption } from '../types';

interface Props {
  isOpen: boolean;
  objects: ObjectOption[];
  /** The query's current ElementIds, which may include $variables and custom ids. */
  selected: string[];
  onApply: (elementIds: string[]) => void;
  onDismiss: () => void;
  /** Field filters to start with, kept by the query editor between openings. */
  initialPicks?: FieldPicks;
  /** Called with the field filters whenever they change. */
  onPicksChange?: (picks: FieldPicks) => void;
}

// Rendering thousands of rows at once makes the modal sluggish; past this
// the user is asked to narrow the search instead.
const MAX_ROWS = 1000;

// A field with more values than this gets a drop-down instead of a row of chips.
const MAX_CHIPS = 12;

interface Row {
  node: TreeNode;
  depth: number;
}

export function TagBrowser({ isOpen, objects, selected, onApply, onDismiss, initialPicks, onPicksChange }: Props) {
  const styles = useStyles2(getStyles);
  const systemIds = useMemo(() => {
    const isSystem = systemTagMatcher(objects);
    return new Set(objects.filter(isSystem).map((o) => o.elementId));
  }, [objects]);
  // Hidden by default; shown from the start if the query already uses one, so it can be seen and unticked.
  const [showSystem, setShowSystem] = useState(() => selected.some((id) => systemIds.has(id)));
  const shown = useMemo(
    () => (showSystem ? objects : objects.filter((o) => !systemIds.has(o.elementId))),
    [objects, systemIds, showSystem]
  );
  // Every tag, whatever the field filters; ticked tags a filter hides still count and are kept.
  const tree = useMemo(() => buildTree(shown), [shown]);

  // Field filters (native API only): within a field any picked value matches,
  // across fields all must. They narrow what is listed, not what the query reads.
  const [storedPicks, setStoredPicks] = useState<FieldPicks>(() => initialPicks ?? {});
  // A kept filter on a field no listed tag has would hide everything with no
  // row to remove it, so only fields the tags still use apply.
  const fieldKeys = useMemo(() => new Set(shown.flatMap((o) => Object.keys(o.fields ?? {}))), [shown]);
  const picks = useMemo(
    () => Object.fromEntries(Object.entries(storedPicks).filter(([k]) => fieldKeys.has(k))),
    [storedPicks, fieldKeys]
  );
  const setPicks = (update: (prev: FieldPicks) => FieldPicks) => {
    const next = update(storedPicks);
    setStoredPicks(next);
    onPicksChange?.(next);
  };
  const picking = Object.values(picks).some((v) => v.length > 0);
  const facets = useMemo(
    () =>
      fieldFacets(
        shown.filter((o) => !o.isFolder),
        picks
      ),
    [shown, picks]
  );
  const fieldTree = useMemo(
    () => (picking ? pruneEmpty(buildTree(shown.filter((o) => o.isFolder || matchesPicks(o.fields, picks)))) : tree),
    [shown, picks, picking, tree]
  );
  const byId = useMemo(() => new Map(objects.map((o) => [o.elementId, o])), [objects]);
  const [search, setSearch] = useState('');
  // Top-level nodes (datasets) start open so the first click is useful.
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(tree.map((n) => n.key)));
  const [checked, setChecked] = useState<Set<string>>(() => new Set(selected));

  const visible = useMemo(() => filterTree(fieldTree, search), [fieldTree, search]);
  // While searching or filtering every match is shown, so the tree opens itself.
  const searching = search.trim() !== '' || picking;

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

      {facets.length > 0 && (
        <div className={styles.filters}>
          {facets
            .filter((facet) => facet.key in picks)
            .map((facet) => (
              <FieldFilterRow
                key={facet.key}
                facet={facet}
                picked={picks[facet.key]}
                onChange={(values) => setPicks((prev) => ({ ...prev, [facet.key]: values }))}
                onRemove={() =>
                  setPicks((prev) => Object.fromEntries(Object.entries(prev).filter(([k]) => k !== facet.key)))
                }
              />
            ))}
          {facets.some((facet) => !(facet.key in picks)) && (
            <div className={styles.filterRow}>
              <span className={styles.filterLabel}>Fields</span>
              <Combobox
                aria-label="Add a field filter"
                width={36}
                // Remounted after each pick, so the box goes back to its placeholder.
                key={Object.keys(picks).join('')}
                placeholder="Filter by a tag field…"
                value={null}
                options={facets
                  .filter((facet) => !(facet.key in picks))
                  .map((facet) => ({
                    label: facet.key,
                    value: facet.key,
                    description: describeFacet(facet),
                  }))}
                onChange={(o) => o && setPicks((prev) => ({ ...prev, [o.value]: [] }))}
              />
            </div>
          )}
        </div>
      )}

      <div
        className={styles.list}
        role="tree"
        aria-label="Tags"
        // Each field filter row takes room from the list, so the modal keeps its height.
        style={{ height: `calc(55vh - ${Object.keys(picks).length * 36}px)` }}
      >
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
              title={
                [node.elementId, info?.description, fieldsText(info?.fields)].filter(Boolean).join(' · ') || undefined
              }
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

// "Site=Southlake, Area=Filling", for the row tooltip.
function fieldsText(fields?: Record<string, string>): string {
  return Object.entries(fields ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join(', ');
}

// "26 tags, 3 values", counted under the other field filters.
function describeFacet(facet: FieldFacet): string {
  const tags = facet.values.reduce((n, v) => n + v.count, 0);
  return `${plural(tags, 'tag')}, ${plural(facet.values.length, 'value')}`;
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

/**
 * One field's filter: a row of toggle chips, or a drop-down when the field
 * has many values. Nothing picked, or All, shows every value.
 */
function FieldFilterRow({
  facet,
  picked,
  onChange,
  onRemove,
}: {
  facet: FieldFacet;
  picked: string[];
  onChange: (values: string[]) => void;
  onRemove: () => void;
}) {
  const styles = useStyles2(getStyles);
  const label = (
    <span className={styles.filterLabel} title={facet.key}>
      <IconButton name="times" size="sm" aria-label={`Remove the ${facet.key} filter`} onClick={onRemove} />
      {facet.key}
    </span>
  );

  if (facet.values.length > MAX_CHIPS) {
    return (
      <div className={styles.filterRow}>
        {label}
        <div className={styles.filterCombo}>
          <MultiCombobox
            aria-label={`Show tags with these ${facet.key} values`}
            options={facet.values.map((v) => ({ label: v.value, value: v.value, description: plural(v.count, 'tag') }))}
            value={picked}
            isClearable
            placeholder={`All (${facet.values.length} values)`}
            onChange={(selected) => onChange(selected.map((o) => o.value))}
          />
        </div>
      </div>
    );
  }

  const chip = (on: boolean) => (on ? `${styles.chip} ${styles.chipOn}` : styles.chip);
  return (
    <div className={styles.filterRow} role="group" aria-label={`Show tags with these ${facet.key} values`}>
      {label}
      <button
        type="button"
        aria-pressed={picked.length === 0}
        className={chip(picked.length === 0)}
        onClick={() => onChange([])}
      >
        All
      </button>
      {facet.values.map(({ value, count }) => {
        const on = picked.includes(value);
        return (
          <button
            key={value}
            type="button"
            aria-pressed={on}
            className={chip(on)}
            onClick={() => onChange(on ? picked.filter((v) => v !== value) : [...picked, value])}
          >
            {value}
            <span className={styles.chipCount}>{count}</span>
          </button>
        );
      })}
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  filters: css({ display: 'flex', flexDirection: 'column', gap: theme.spacing(0.5), marginTop: theme.spacing(1) }),
  filterRow: css({ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing(0.5) }),
  filterLabel: css({
    display: 'inline-flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    width: theme.spacing(16),
    flexShrink: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
  }),
  filterCombo: css({ flex: 1, minWidth: 0 }),
  chip: css({
    display: 'inline-flex',
    alignItems: 'center',
    gap: theme.spacing(0.75),
    padding: theme.spacing(0.25, 1),
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
    background: 'transparent',
    border: `1px solid ${theme.colors.border.medium}`,
    borderRadius: theme.shape.radius.pill,
    cursor: 'pointer',
    '&:hover': { background: theme.colors.action.hover, color: theme.colors.text.primary },
  }),
  chipOn: css({
    color: theme.colors.text.primary,
    background: theme.colors.action.selected,
    borderColor: theme.colors.primary.border,
  }),
  chipCount: css({ color: theme.colors.text.disabled }),
  modal: css({ width: '760px', maxWidth: '95vw' }),
  toolbar: css({ display: 'flex', alignItems: 'center', gap: theme.spacing(1) }),
  search: css({ flex: 1, minWidth: 0 }),
  list: css({
    marginTop: theme.spacing(1),
    height: '55vh',
    minHeight: theme.spacing(25),
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

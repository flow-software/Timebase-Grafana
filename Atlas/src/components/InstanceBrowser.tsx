import React, { useMemo, useState } from 'react';
import { css, cx } from '@emotion/css';
import { colorManipulator, GrafanaTheme2 } from '@grafana/data';
import { Alert, Button, Icon, IconButton, Input, LoadingPlaceholder, Modal, useStyles2 } from '@grafana/ui';

import { ancestorsOf, buildTree, filterTree, nodeName, TreeItem } from '../tree';
import { SelectionVariable, TreeNodeData } from '../types';
import { timebase } from './timebase';

const LOGO = 'public/plugins/flowsoftware-timebaseatlas-datasource/img/logo.svg';

interface Props {
  isOpen: boolean;
  title: string;
  nodes?: TreeNodeData[];
  loading: boolean;
  error?: string;
  selected?: string;
  onSelect: (id: string) => void;
  onRefresh: () => void;
  onDismiss: () => void;
  /** Rows that can be picked; others only expand and collapse. Default: all. */
  canSelect?: (n: TreeNodeData) => boolean;
  /** The grey text after the name. Default: id (when it differs) and type. */
  metaOf?: (n: TreeNodeData) => string;
  /** What the pickable rows are called in the count, e.g. "types". */
  noun?: string;
  /** What is loaded, for the loading and error text. Default "instances". */
  what?: string;
  /** The filter box's placeholder. */
  filterPlaceholder?: string;
  /** Dashboard variables whose values are instances; picking one gives "$name". */
  variables?: SelectionVariable[];
}

/** Rows shown while filtering; more than this is slow and not useful to scroll. */
const MAX_FILTERED_ROWS = 500;

/**
 * A modal tree of a domain's instances, nested by containment like the
 * Atlas Instances view. Picking a row selects it and closes the modal.
 */
export function InstanceBrowser(props: Props) {
  // Remount on each open so the filter and expansion start fresh.
  return props.isOpen ? <BrowserBody {...props} /> : null;
}

function BrowserBody({
  title,
  nodes,
  loading,
  error,
  selected,
  onSelect,
  onRefresh,
  onDismiss,
  canSelect = () => true,
  metaOf = defaultMeta,
  noun = 'instances',
  what = 'instances',
  filterPlaceholder = 'Filter by name, id or type',
  variables = [],
}: Props) {
  const styles = useStyles2(getStyles);
  const [filter, setFilter] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(() =>
    selected && nodes ? new Set(ancestorsOf(nodes, selected)) : new Set()
  );

  const roots = useMemo(() => buildTree(nodes ?? []), [nodes]);
  const filtered = useMemo(() => filterTree(roots, filter), [roots, filter]);
  const filtering = filter.trim() !== '';

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });

  // While filtering every branch is open, so each match shows in place.
  let rowsLeft = MAX_FILTERED_ROWS;
  const renderItem = (item: TreeItem, depth: number): React.ReactNode => {
    if (filtering && rowsLeft-- <= 0) {
      return null;
    }
    const n = item.node;
    const hasChildren = item.children.length > 0;
    const open = filtering || expanded.has(n.id);
    return (
      <li key={n.id}>
        <div
          className={cx(styles.row, n.id === selected && styles.selected)}
          style={{ paddingLeft: depth * 20 }}
          role="treeitem"
          aria-selected={n.id === selected}
          aria-expanded={hasChildren ? open : undefined}
        >
          {hasChildren ? (
            <IconButton
              name={open ? 'angle-down' : 'angle-right'}
              aria-label={open ? 'Collapse' : 'Expand'}
              disabled={filtering}
              onClick={() => toggle(n.id)}
            />
          ) : (
            <span className={styles.spacer} />
          )}
          <button
            type="button"
            className={cx(styles.pick, !canSelect(n) && styles.info)}
            onClick={() => (canSelect(n) ? onSelect(n.id) : hasChildren && !filtering && toggle(n.id))}
            title={canSelect(n) ? `Select ${nodeName(n)}` : undefined}
          >
            <span className={styles.name}>{nodeName(n)}</span>
            <span className={styles.meta}>{metaOf(n)}</span>
          </button>
        </div>
        {hasChildren && open && <ul className={styles.list}>{item.children.map((c) => renderItem(c, depth + 1))}</ul>}
      </li>
    );
  };

  const shownVars = variables.filter(
    (v) => !filtering || v.name.toLowerCase().includes(filter.trim().replace(/^\$/, '').toLowerCase())
  );
  const renderVar = (v: SelectionVariable) => {
    const id = '$' + v.name;
    const first = nodes?.find((n) => n.id === v.values[0]);
    return (
      <li key={id}>
        <div
          className={cx(styles.row, id === selected && styles.selected)}
          role="treeitem"
          aria-selected={id === selected}
        >
          <span className={styles.spacer} />
          <button type="button" className={styles.pick} onClick={() => onSelect(id)} title={`Select ${id}`}>
            <Icon name="dollar-alt" size="sm" className={styles.varIcon} />
            <span className={styles.name}>{id}</span>
            <span className={styles.meta}>
              now {v.values.slice(0, 4).join(', ')}
              {v.values.length > 4 ? `, +${v.values.length - 4}` : ''}
              {first ? ` · ${first.type}` : ''}
            </span>
          </button>
        </div>
      </li>
    );
  };

  return (
    <Modal
      title={
        <span className={styles.title}>
          <img src={LOGO} alt="" className={styles.logo} />
          {title}
        </span>
      }
      ariaLabel={title}
      isOpen
      onDismiss={onDismiss}
      className={styles.modal}
    >
      <div className={styles.toolbar}>
        <Input
          autoFocus
          prefix={<Icon name="search" />}
          placeholder={filterPlaceholder}
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
        />
        <IconButton name="sync" tooltip="Reload from Atlas" onClick={onRefresh} />
      </div>

      {error && (
        <Alert severity="error" title={`Could not load the ${what}`}>
          {error}
        </Alert>
      )}
      {loading && !nodes && <LoadingPlaceholder text={`Loading ${what}…`} />}

      {nodes && (
        <>
          <div className={styles.summary}>
            {filtering
              ? `${filtered.matches} matches` +
                (filtered.matches > MAX_FILTERED_ROWS ? `; refine the filter to see all` : '')
              : `${nodes.filter(canSelect).length} ${noun}`}
          </div>
          <div className={styles.scroll}>
            {shownVars.length > 0 && (
              <>
                <div className={styles.section}>Dashboard variables</div>
                <ul className={styles.list} role="tree">
                  {shownVars.map(renderVar)}
                </ul>
                <div className={styles.section}>Instances</div>
              </>
            )}
            <ul className={styles.list} role="tree">
              {filtered.roots.map((r) => renderItem(r, 0))}
            </ul>
          </div>
        </>
      )}

      <Modal.ButtonRow>
        <Button variant="secondary" fill="outline" onClick={onDismiss}>
          Cancel
        </Button>
      </Modal.ButtonRow>
    </Modal>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  modal: css({ width: 760, maxWidth: '95vw' }),
  title: css({ display: 'inline-flex', alignItems: 'center', gap: theme.spacing(1) }),
  logo: css({ width: 22, height: 22 }),
  varIcon: css({ color: timebase(theme).orange, alignSelf: 'center', flexShrink: 0 }),
  section: css({
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
    fontWeight: theme.typography.fontWeightMedium,
    textTransform: 'uppercase',
    letterSpacing: '0.04em',
    padding: theme.spacing(0.75, 1, 0.25),
  }),
  toolbar: css({ display: 'flex', gap: theme.spacing(1), alignItems: 'center', marginBottom: theme.spacing(1) }),
  summary: css({
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
    margin: theme.spacing(0.5, 0),
  }),
  scroll: css({
    height: '55vh',
    overflowY: 'auto',
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    padding: theme.spacing(0.5),
  }),
  list: css({ listStyle: 'none', margin: 0, padding: 0 }),
  row: css({
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    borderRadius: theme.shape.radius.default,
    '&:hover': { background: theme.colors.action.hover },
  }),
  selected: css({
    background: colorManipulator.alpha(timebase(theme).blue, 0.12),
    boxShadow: `inset 3px 0 0 ${timebase(theme).blue}`,
  }),
  info: css({ cursor: 'default', opacity: 0.8 }),
  spacer: css({ display: 'inline-block', width: 20, flexShrink: 0 }),
  pick: css({
    display: 'flex',
    alignItems: 'baseline',
    gap: theme.spacing(1),
    flex: 1,
    minWidth: 0,
    padding: theme.spacing(0.5, 0.5),
    background: 'none',
    border: 'none',
    color: theme.colors.text.primary,
    textAlign: 'left',
    cursor: 'pointer',
  }),
  name: css({ fontWeight: theme.typography.fontWeightMedium, whiteSpace: 'nowrap' }),
  meta: css({
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  }),
});

function defaultMeta(n: TreeNodeData): string {
  return (n.label && n.label !== n.id ? `${n.id} · ` : '') + n.type;
}

import React, { useMemo, useState } from 'react';
import { css, cx } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import {
  Alert,
  Button,
  Checkbox,
  Icon,
  IconButton,
  InlineSwitch,
  Input,
  LoadingPlaceholder,
  Modal,
  useStyles2,
} from '@grafana/ui';

import { ancestorsOf, buildTree, liveDescendants, nodeName, PickItem, pickTree } from '../tree';
import { HistoryPick, TreeNodeData } from '../types';

interface Props {
  isOpen: boolean;
  title: string;
  nodes?: TreeNodeData[];
  loading: boolean;
  error?: string;
  selection: HistoryPick[];
  onApply: (picks: HistoryPick[]) => void;
  onRefresh: () => void;
  onDismiss: () => void;
}

/** Per instance: 'all' (every live property, including ones added later) or the picked properties. */
type Picked = Map<string, 'all' | Set<string>>;

const MAX_FILTERED_ROWS = 500;

/**
 * Picks History series in one place: the containment tree, as in Atlas, with
 * each instance's live properties listed under it. Tick an instance for all
 * its live properties, or open it and tick single properties. Changes apply
 * on Apply.
 */
export function HistoryBrowser(props: Props) {
  return props.isOpen ? <Body {...props} /> : null;
}

function Body({ title, nodes, loading, error, selection, onApply, onRefresh, onDismiss }: Props) {
  const styles = useStyles2(getStyles);
  const [filter, setFilter] = useState('');
  const [onlyLive, setOnlyLive] = useState(true);
  const [picked, setPicked] = useState<Picked>(() => toPicked(selection));
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const open = new Set<string>();
    if (nodes) {
      for (const p of selection) {
        ancestorsOf(nodes, p.instance).forEach((a) => open.add(a));
        if (p.property) {
          open.add(p.instance);
        }
      }
    }
    return open;
  });

  const byId = useMemo(() => new Map((nodes ?? []).map((n) => [n.id, n])), [nodes]);
  const roots = useMemo(() => buildTree(nodes ?? []), [nodes]);
  const tree = useMemo(() => pickTree(roots, filter, onlyLive), [roots, filter, onlyLive]);
  const filtering = filter.trim() !== '';
  const liveCount = useMemo(() => (nodes ?? []).filter((n) => (n.live?.length ?? 0) > 0).length, [nodes]);
  // Picks that are not in this tree ($variables, other worlds) are kept as they are.
  const outside = Array.from(picked.keys()).filter((id) => !byId.has(id));

  const toggleOpen = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });

  const change = (fn: (next: Picked) => void) =>
    setPicked((prev) => {
      const next = new Map(prev);
      fn(next);
      return next;
    });

  const toggleProperty = (n: TreeNodeData, prop: string) =>
    change((next) => {
      const cur = next.get(n.id);
      let set: Set<string>;
      if (cur === 'all') {
        // Unticking one property of a whole instance keeps the others.
        set = new Set((n.live ?? []).map((p) => p.name));
        set.delete(prop);
      } else {
        set = new Set(cur ?? []);
        set.has(prop) ? set.delete(prop) : set.add(prop);
      }
      if (set.size === 0) {
        next.delete(n.id);
      } else {
        next.set(n.id, set);
      }
    });

  /**
   * The row checkbox covers the instance and everything nested under it that
   * has live values: ticked when all of them are picked whole, a dash when
   * some are picked, empty when none are. Ticking picks all of them whole;
   * ticking a full row clears them.
   */
  const rowState = (ids: string[]): 'all' | 'some' | 'none' => {
    if (ids.length > 0 && ids.every((id) => picked.get(id) === 'all')) {
      return 'all';
    }
    return ids.some((id) => picked.has(id)) ? 'some' : 'none';
  };

  const toggleRow = (ids: string[]) => {
    const clear = rowState(ids) === 'all';
    change((next) => ids.forEach((id) => (clear ? next.delete(id) : next.set(id, 'all'))));
  };

  let rowsLeft = MAX_FILTERED_ROWS;
  const renderItem = (item: PickItem, depth: number): React.ReactNode => {
    if (filtering && rowsLeft-- <= 0) {
      return null;
    }
    const n = item.node;
    const live = n.live ?? [];
    const cur = picked.get(n.id);
    const hasBody = item.children.length > 0 || item.props.length > 0;
    const open = filtering || expanded.has(n.id);
    const covered = liveDescendants(item);
    const state = rowState(covered);
    // A closed row says how many picks are hidden under it.
    const hidden = open ? 0 : covered.filter((id) => id !== n.id && picked.has(id)).length;
    const title =
      covered.length > 1
        ? `${live.length ? 'This instance and ' : ''}${covered.length - (live.length ? 1 : 0)} nested instances with live values`
        : 'All live values of this instance';
    return (
      <li key={n.id}>
        <div className={cx(styles.row, cur && styles.selected)} style={{ paddingLeft: depth * 20 }} role="treeitem">
          {hasBody ? (
            <IconButton
              name={open ? 'angle-down' : 'angle-right'}
              aria-label={open ? 'Collapse' : 'Expand'}
              disabled={filtering}
              onClick={() => toggleOpen(n.id)}
            />
          ) : (
            <span className={styles.spacer} />
          )}
          {covered.length > 0 ? (
            <span title={title} className={styles.boxWrap}>
              {/* Keyed by state: Grafana's Checkbox sets the DOM indeterminate flag but never clears it. */}
              <Checkbox
                key={state}
                value={state === 'all'}
                indeterminate={state === 'some'}
                onChange={() => toggleRow(covered)}
                aria-label={`${nodeName(n)}: ${title}`}
              />
            </span>
          ) : (
            <span className={styles.box} />
          )}
          <button
            type="button"
            className={styles.label}
            title={covered.length > 0 ? title : undefined}
            onClick={() => (covered.length > 0 ? toggleRow(covered) : hasBody && !filtering && toggleOpen(n.id))}
          >
            <span className={styles.name}>{nodeName(n)}</span>
            <span className={styles.meta}>
              {(n.label && n.label !== n.id ? `${n.id} · ` : '') + n.type}
              {live.length > 0 && ` · ${live.length} live`}
            </span>
            {hidden > 0 && <span className={styles.hidden}>{hidden} selected inside</span>}
          </button>
        </div>
        {hasBody && open && (
          <ul className={styles.list}>
            {item.props.map((p) => {
              const on = cur === 'all' || (cur instanceof Set && cur.has(p.name));
              return (
                <li key={'p:' + p.name}>
                  <div className={cx(styles.row, styles.prop)} style={{ paddingLeft: (depth + 1) * 20 }}>
                    <span className={styles.spacer} />
                    <Checkbox
                      value={on}
                      onChange={() => toggleProperty(n, p.name)}
                      aria-label={`${nodeName(n)} ${p.name}`}
                    />
                    <button type="button" className={styles.label} onClick={() => toggleProperty(n, p.name)}>
                      <Icon
                        name={p.kind === 'record' ? 'list-ul' : 'chart-line'}
                        size="sm"
                        className={styles.propIcon}
                      />
                      <span>{p.name}</span>
                      <span className={styles.meta}>
                        {p.kind === 'record' ? 'record: one column per part' : p.kind}
                      </span>
                    </button>
                  </div>
                </li>
              );
            })}
            {item.children.map((c) => renderItem(c, depth + 1))}
          </ul>
        )}
      </li>
    );
  };

  const picks = toPicks(picked, byId);
  const instances = picked.size;
  const series = Array.from(picked.entries()).reduce(
    (sum, [id, v]) => sum + (v === 'all' ? (byId.get(id)?.live?.length ?? 1) : v.size),
    0
  );

  return (
    <Modal title={title} isOpen onDismiss={onDismiss} className={styles.modal}>
      <div className={styles.toolbar}>
        <Input
          autoFocus
          prefix={<Icon name="search" />}
          placeholder="Filter by instance, type or property"
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
        />
        <InlineSwitch
          label="Only with history"
          showLabel
          value={onlyLive}
          onChange={(e) => setOnlyLive(e.currentTarget.checked)}
        />
        <IconButton name="sync" tooltip="Reload from Atlas" onClick={onRefresh} />
      </div>

      {error && (
        <Alert severity="error" title="Could not load the instances">
          {error}
        </Alert>
      )}
      {loading && !nodes && <LoadingPlaceholder text="Loading instances…" />}

      {nodes && (
        <>
          <div className={styles.summary}>
            {filtering
              ? `${tree.matches} matches` + (tree.matches > MAX_FILTERED_ROWS ? '; refine the filter to see all' : '')
              : `${liveCount} instances with live values. Tick a row for it and everything under it, or open it to pick single properties.`}
          </div>
          <div className={styles.scroll}>
            <ul className={styles.list} role="tree">
              {tree.roots.map((r) => renderItem(r, 0))}
            </ul>
            {tree.roots.length === 0 && <div className={styles.empty}>Nothing to show.</div>}
          </div>
        </>
      )}

      <Modal.ButtonRow
        leftItems={
          <div className={styles.footer}>
            <span>
              {instances === 0
                ? 'Nothing selected'
                : `${instances} instance${instances === 1 ? '' : 's'}, about ${series} series`}
              {outside.length > 0 && ` (incl. ${outside.join(', ')})`}
            </span>
            {instances > 0 && (
              <Button size="sm" variant="secondary" fill="text" onClick={() => setPicked(new Map())}>
                Clear
              </Button>
            )}
          </div>
        }
      >
        <Button variant="secondary" fill="outline" onClick={onDismiss}>
          Cancel
        </Button>
        <Button onClick={() => onApply(picks)}>Apply</Button>
      </Modal.ButtonRow>
    </Modal>
  );
}

function toPicked(selection: HistoryPick[]): Picked {
  const out: Picked = new Map();
  for (const p of selection) {
    const cur = out.get(p.instance);
    if (!p.property || cur === 'all') {
      out.set(p.instance, 'all');
    } else {
      out.set(p.instance, new Set([...(cur ?? []), p.property]));
    }
  }
  return out;
}

function toPicks(picked: Picked, byId: Map<string, TreeNodeData>): HistoryPick[] {
  const out: HistoryPick[] = [];
  picked.forEach((v, id) => {
    const type = byId.get(id)?.type;
    const base = type ? { instance: id, type } : { instance: id };
    if (v === 'all') {
      out.push(base);
    } else {
      v.forEach((property) => out.push({ ...base, property }));
    }
  });
  return out;
}

const getStyles = (theme: GrafanaTheme2) => ({
  modal: css({ width: 820, maxWidth: '95vw' }),
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
  empty: css({ color: theme.colors.text.secondary, padding: theme.spacing(1) }),
  list: css({ listStyle: 'none', margin: 0, padding: 0 }),
  row: css({
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    borderRadius: theme.shape.radius.default,
    '&:hover': { background: theme.colors.action.hover },
  }),
  prop: css({ fontSize: theme.typography.bodySmall.fontSize }),
  selected: css({ background: theme.colors.action.selected }),
  spacer: css({ display: 'inline-block', width: 20, flexShrink: 0 }),
  box: css({ display: 'inline-block', width: 16, flexShrink: 0 }),
  label: css({
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
  hidden: css({
    color: theme.colors.primary.text,
    fontSize: theme.typography.bodySmall.fontSize,
    whiteSpace: 'nowrap',
  }),
  propIcon: css({ color: theme.colors.text.secondary, alignSelf: 'center' }),
  boxWrap: css({ display: 'inline-flex', flexShrink: 0 }),
  footer: css({
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(1),
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
  }),
});

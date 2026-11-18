import React, { useMemo, useState } from 'react';
import { css, cx } from '@emotion/css';
import { colorManipulator, GrafanaTheme2, IconName } from '@grafana/data';
import {
  Alert,
  Button,
  Checkbox,
  Icon,
  IconButton,
  Input,
  LoadingPlaceholder,
  Modal,
  RadioButtonGroup,
  useStyles2,
} from '@grafana/ui';

import {
  ancestorsOf,
  buildTree,
  expandSelection,
  liveDescendants,
  nodeName,
  PickItem,
  pickTree,
  shortType,
  subtreeIds,
  typeGroups,
} from '../tree';
import { HistoryPick, LiveProperty, TreeNodeData } from '../types';
import { timebase } from './timebase';

/** Property icons, the same ones Grafana uses for field types. */
const KIND_ICONS: Record<LiveProperty['kind'], IconName> = {
  number: 'calculator-alt',
  text: 'font',
  boolean: 'toggle-on',
  record: 'brackets-curly',
  unknown: 'question-circle',
};

const LOGO = 'public/plugins/flowsoftware-atlas-datasource/img/logo.svg';

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

/** Per instance or type: 'all' (every live property, including ones added later) or the picked properties. */
type Props_ = 'all' | Set<string>;

interface Sel {
  /** Single instances. */
  picked: Map<string, Props_>;
  /** Instances picked with everything nested under them, resolved when the query runs. */
  nested: Map<string, Props_>;
  /** Every instance of a type, resolved when the query runs. */
  types: Map<string, Props_>;
}

type View = 'instances' | 'types';

const views: Array<{ label: string; value: View; description: string }> = [
  { label: 'Instances', value: 'instances', description: 'The containment tree, as in Atlas' },
  { label: 'Types', value: 'types', description: 'Every instance of an entity type, including ones added later' },
];

const MAX_FILTERED_ROWS = 500;

/**
 * Picks the Data selection in one place. Instances view: the containment
 * tree with each instance's live properties under it; ticking a row with
 * nested instances picks it "with everything nested", which follows changes
 * in Atlas. Types view: every instance of a type, or one property of every
 * instance. Changes apply on Apply.
 */
export function HistoryBrowser(props: Props) {
  return props.isOpen ? <Body {...props} /> : null;
}

function Body({ title, nodes, loading, error, selection, onApply, onRefresh, onDismiss }: Props) {
  const styles = useStyles2(getStyles);
  const [filter, setFilter] = useState('');
  const [view, setView] = useState<View>(() =>
    selection.length > 0 && selection.every((p) => p.scope === 'type') ? 'types' : 'instances'
  );
  const [sel, setSel] = useState<Sel>(() => toSel(selection));
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const open = new Set<string>();
    if (nodes) {
      for (const p of selection) {
        if (p.scope === 'type') {
          if (p.property && p.type) {
            open.add('type:' + shortType(p.type));
          }
          continue;
        }
        ancestorsOf(nodes, p.instance).forEach((a) => open.add(a));
        if (p.property) {
          open.add(p.instance);
        }
      }
    }
    return open;
  });

  const byId = useMemo(() => new Map((nodes ?? []).map((n) => [n.id, n])), [nodes]);
  const parent = useMemo(() => new Map((nodes ?? []).map((n) => [n.id, n.parentId])), [nodes]);
  const roots = useMemo(() => buildTree(nodes ?? []), [nodes]);
  const tree = useMemo(() => pickTree(roots, filter, true), [roots, filter]);
  const groups = useMemo(() => typeGroups(nodes ?? [], filter), [nodes, filter]);
  const filtering = filter.trim() !== '';

  const toggleOpen = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });

  const change = (fn: (next: Sel) => void) =>
    setSel((prev) => {
      const next: Sel = { picked: new Map(prev.picked), nested: new Map(prev.nested), types: new Map(prev.types) };
      fn(next);
      return next;
    });

  /** The nested pick covering an instance (itself or an ancestor), if any. */
  const ownerIn = (s: Sel, id: string): string | undefined => {
    for (let cur: string | undefined = id; cur; cur = parent.get(cur)) {
      if (s.nested.has(cur)) {
        return cur;
      }
    }
    return undefined;
  };
  const ownerValue = (id: string): Props_ | undefined => {
    const o = ownerIn(sel, id);
    return o ? sel.nested.get(o) : undefined;
  };
  const isWhole = (id: string) => sel.picked.get(id) === 'all' || ownerValue(id) === 'all';
  const propOn = (n: TreeNodeData, prop: string) => {
    const cur = sel.picked.get(n.id);
    const own = ownerValue(n.id);
    return isWhole(n.id) || (cur instanceof Set && cur.has(prop)) || (own instanceof Set && own.has(prop));
  };

  /** Turns a nested pick into single picks of what it covers now, before part of it is unticked. */
  const materialize = (s: Sel, owner: string) => {
    const val = s.nested.get(owner);
    s.nested.delete(owner);
    for (const id of subtreeIds(nodes ?? [], owner)) {
      s.nested.delete(id);
      const live = (byId.get(id)?.live ?? []).map((l) => l.name);
      if (val === 'all' && live.length > 0) {
        s.picked.set(id, 'all');
      } else if (val instanceof Set) {
        const props = live.filter((l) => val.has(l));
        const cur = s.picked.get(id);
        if (props.length > 0 && cur !== 'all') {
          s.picked.set(id, new Set([...(cur ?? []), ...props]));
        }
      }
    }
  };

  const toggleProperty = (n: TreeNodeData, prop: string) =>
    change((s) => {
      const owner = ownerIn(s, n.id);
      if (owner) {
        materialize(s, owner);
      }
      s.picked.set(
        n.id,
        flip(
          s.picked.get(n.id),
          prop,
          (n.live ?? []).map((p) => p.name)
        ) ?? 'all'
      );
      if (s.picked.get(n.id) === undefined || isEmpty(s.picked.get(n.id))) {
        s.picked.delete(n.id);
      }
    });

  /**
   * The row checkbox covers the instance and everything nested under it that
   * has live values: ticked when all of them are picked whole, a dash when
   * some are picked, empty when none are.
   */
  const rowState = (n: TreeNodeData, covered: string[]): 'all' | 'some' | 'none' => {
    const own = sel.nested.get(n.id);
    if (own === 'all' || (covered.length > 0 && covered.every(isWhole))) {
      return 'all';
    }
    return own || covered.some((id) => sel.picked.has(id) || ownerValue(id) !== undefined) ? 'some' : 'none';
  };

  /** Ticking a row with nested instances picks it with everything nested; a leaf is picked whole. */
  const toggleRow = (n: TreeNodeData, covered: string[]) => {
    const clear = rowState(n, covered) === 'all';
    const below = subtreeIds(nodes ?? [], n.id);
    change((s) => {
      const owner = ownerIn(s, n.id);
      if (clear && owner && owner !== n.id) {
        materialize(s, owner);
      }
      below.forEach((id) => {
        s.picked.delete(id);
        s.nested.delete(id);
      });
      if (!clear) {
        if (below.length > 1) {
          s.nested.set(n.id, 'all');
        } else {
          s.picked.set(n.id, 'all');
        }
      }
    });
  };

  let rowsLeft = MAX_FILTERED_ROWS;
  const renderItem = (item: PickItem, depth: number): React.ReactNode => {
    if (filtering && rowsLeft-- <= 0) {
      return null;
    }
    const n = item.node;
    const live = n.live ?? [];
    const cur = sel.picked.get(n.id);
    const hasBody = item.children.length > 0 || item.props.length > 0;
    const open = filtering || expanded.has(n.id);
    const covered = liveDescendants(item);
    const state = rowState(n, covered);
    const nestedValue = sel.nested.get(n.id);
    const isNested = nestedValue !== undefined;
    const coveredByAncestor = !isNested && !!ownerIn(sel, n.id);
    // A closed row says how many picks are hidden under it.
    const hidden =
      open || isNested ? 0 : covered.filter((id) => id !== n.id && (sel.picked.has(id) || sel.nested.has(id))).length;
    const title =
      covered.length > 1 || subtreeIds(nodes ?? [], n.id).length > 1
        ? 'This instance and everything nested under it, including instances added later'
        : 'All live values of this instance';
    return (
      <li key={n.id}>
        <div
          className={cx(styles.row, (cur || isNested || coveredByAncestor) && styles.selected)}
          style={{ paddingLeft: depth * 20 }}
          role="treeitem"
        >
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
                onChange={() => toggleRow(n, covered)}
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
            onClick={() => (covered.length > 0 ? toggleRow(n, covered) : hasBody && !filtering && toggleOpen(n.id))}
          >
            <span className={styles.name}>{nodeName(n)}</span>
            <span className={styles.meta}>
              {(n.label && n.label !== n.id ? `${n.id} · ` : '') + n.type}
              {live.length > 0 && ` · ${live.length} live`}
            </span>
            {isNested && (
              <span className={styles.tag} title="Follows instances added or removed under it">
                <Icon name="sitemap" size="xs" />
                {nestedValue === 'all'
                  ? 'with everything nested'
                  : `${[...nestedValue].join(', ')} of everything nested`}
              </span>
            )}
            {hidden > 0 && <span className={styles.hidden}>{hidden} selected inside</span>}
          </button>
        </div>
        {hasBody && open && (
          <ul className={styles.list}>
            {item.props.map((p) => (
              <PropertyRow
                key={'p:' + p.name}
                prop={p}
                depth={depth + 1}
                on={propOn(n, p.name)}
                label={`${nodeName(n)} ${p.name}`}
                onToggle={() => toggleProperty(n, p.name)}
              />
            ))}
            {item.children.map((c) => renderItem(c, depth + 1))}
          </ul>
        )}
      </li>
    );
  };

  const renderType = (g: (typeof groups)[number]) => {
    const key = 'type:' + g.type;
    const cur = sel.types.get(g.type);
    const open = (filtering && g.props.length > 0) || expanded.has(key);
    const toggleType = () =>
      change((s) => (s.types.get(g.type) === 'all' ? s.types.delete(g.type) : s.types.set(g.type, 'all')));
    const toggleTypeProp = (prop: string) =>
      change((s) => {
        const next = flip(
          s.types.get(g.type),
          prop,
          g.props.map((p) => p.name)
        );
        next && !isEmpty(next) ? s.types.set(g.type, next) : s.types.delete(g.type);
      });
    return (
      <li key={key}>
        <div className={cx(styles.row, cur && styles.selected)} role="treeitem">
          {g.props.length > 0 ? (
            <IconButton
              name={open ? 'angle-down' : 'angle-right'}
              aria-label={open ? 'Collapse' : 'Expand'}
              onClick={() => toggleOpen(key)}
            />
          ) : (
            <span className={styles.spacer} />
          )}
          <span className={styles.boxWrap} title={`Every ${g.type}, including instances added later`}>
            <Checkbox
              key={cur === 'all' ? 'all' : cur ? 'some' : 'none'}
              value={cur === 'all'}
              indeterminate={cur instanceof Set}
              onChange={toggleType}
              aria-label={`Every ${g.type}`}
            />
          </span>
          <button type="button" className={styles.label} onClick={toggleType}>
            <span className={styles.name}>{g.type}</span>
            <span className={styles.meta}>
              {g.ids.length} instance{g.ids.length === 1 ? '' : 's'}
              {g.props.length > 0 && ` · ${g.props.length} live`}
            </span>
          </button>
        </div>
        {open && (
          <ul className={styles.list}>
            {g.props.map((p) => (
              <PropertyRow
                key={'p:' + p.name}
                prop={p}
                depth={1}
                on={cur === 'all' || (cur instanceof Set && cur.has(p.name))}
                label={`Every ${g.type} ${p.name}`}
                extra={p.count < g.ids.length ? `on ${p.count} of ${g.ids.length}` : undefined}
                onToggle={() => toggleTypeProp(p.name)}
              />
            ))}
          </ul>
        )}
      </li>
    );
  };

  const picks = toPicks(sel, byId);
  const resolved = expandSelection(picks, nodes ?? []);
  const instances = new Set(resolved.map((p) => p.instance)).size;
  const series = resolved.reduce((sum, p) => sum + (p.property ? 1 : (byId.get(p.instance)?.live?.length ?? 1)), 0);
  const outside = picks.filter((p) => p.scope !== 'type' && !byId.has(p.instance)).map((p) => p.instance);
  const nothing = sel.picked.size + sel.nested.size + sel.types.size === 0;

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
        <div className={styles.views}>
          <RadioButtonGroup options={views} value={view} onChange={setView} />
        </div>
        <Input
          autoFocus
          prefix={<Icon name="search" />}
          placeholder={view === 'types' ? 'Filter by type or property' : 'Filter by instance, type or property'}
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
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
            {view === 'types'
              ? 'Tick a type for every instance of it, or open it to pick one property of every instance. New instances are included automatically.'
              : filtering
                ? `${tree.matches} matches` + (tree.matches > MAX_FILTERED_ROWS ? '; refine the filter to see all' : '')
                : 'Tick a row for it and everything nested under it (new instances included), or open it to pick single properties.'}
          </div>
          <div className={styles.scroll}>
            <ul className={styles.list} role="tree">
              {view === 'types' ? groups.map(renderType) : tree.roots.map((r) => renderItem(r, 0))}
            </ul>
            {(view === 'types' ? groups.length : tree.roots.length) === 0 && (
              <div className={styles.empty}>Nothing to show.</div>
            )}
          </div>
        </>
      )}

      <Modal.ButtonRow
        leftItems={
          <div className={styles.footer}>
            <span>
              {nothing
                ? 'Nothing selected'
                : `${instances} instance${instances === 1 ? '' : 's'} now, about ${series} series`}
              {outside.length > 0 && ` (plus ${outside.join(', ')})`}
            </span>
            {!nothing && (
              <Button
                size="sm"
                variant="secondary"
                fill="text"
                onClick={() => setSel({ picked: new Map(), nested: new Map(), types: new Map() })}
              >
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

function PropertyRow({
  prop,
  depth,
  on,
  label,
  extra,
  onToggle,
}: {
  prop: LiveProperty;
  depth: number;
  on: boolean;
  label: string;
  extra?: string;
  onToggle: () => void;
}) {
  const styles = useStyles2(getStyles);
  return (
    <li>
      <div className={cx(styles.row, styles.prop)} style={{ paddingLeft: depth * 20 }}>
        <span className={styles.spacer} />
        <Checkbox value={on} onChange={onToggle} aria-label={label} />
        <button type="button" className={styles.label} onClick={onToggle}>
          <Icon name={KIND_ICONS[prop.kind]} size="sm" className={styles.kind[prop.kind]} />
          <span>{prop.name}</span>
          <span className={styles.meta}>
            {prop.kind === 'record' ? 'record: one column per part' : prop.kind}
            {extra && ` · ${extra}`}
          </span>
        </button>
      </div>
    </li>
  );
}

/** Toggles one property in a pick; unticking one of 'all' keeps the others. */
function flip(cur: Props_ | undefined, prop: string, all: string[]): Props_ | undefined {
  const set = cur === 'all' ? new Set(all) : new Set(cur ?? []);
  set.has(prop) ? set.delete(prop) : set.add(prop);
  return set;
}

function isEmpty(v: Props_ | undefined): boolean {
  return v instanceof Set && v.size === 0;
}

function toSel(selection: HistoryPick[]): Sel {
  const s: Sel = { picked: new Map(), nested: new Map(), types: new Map() };
  const add = (m: Map<string, Props_>, key: string, prop?: string) => {
    const cur = m.get(key);
    m.set(key, !prop || cur === 'all' ? 'all' : new Set([...(cur ?? []), prop]));
  };
  for (const p of selection) {
    if (p.scope === 'type' && p.type) {
      add(s.types, shortType(p.type), p.property);
    } else if (p.scope === 'nested') {
      add(s.nested, p.instance, p.property);
    } else {
      add(s.picked, p.instance, p.property);
    }
  }
  return s;
}

function toPicks(sel: Sel, byId: Map<string, TreeNodeData>): HistoryPick[] {
  const out: HistoryPick[] = [];
  const withType = (id: string): HistoryPick => {
    const type = byId.get(id)?.type;
    return type ? { instance: id, type } : { instance: id };
  };
  sel.picked.forEach((v, id) => {
    if (v === 'all') {
      out.push(withType(id));
    } else {
      v.forEach((property) => out.push({ ...withType(id), property }));
    }
  });
  sel.nested.forEach((v, id) => {
    if (v === 'all') {
      out.push({ ...withType(id), scope: 'nested' });
    } else {
      v.forEach((property) => out.push({ ...withType(id), property, scope: 'nested' }));
    }
  });
  sel.types.forEach((v, type) => {
    if (v === 'all') {
      out.push({ instance: '', type, scope: 'type' });
    } else {
      v.forEach((property) => out.push({ instance: '', type, property, scope: 'type' }));
    }
  });
  return out;
}

const getStyles = (theme: GrafanaTheme2) => {
  const tb = timebase(theme);
  return {
    modal: css({ width: 860, maxWidth: '95vw' }),
    title: css({ display: 'inline-flex', alignItems: 'center', gap: theme.spacing(1) }),
    logo: css({ width: 22, height: 22 }),
    views: css({ flexShrink: 0 }),
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
    selected: css({
      background: colorManipulator.alpha(tb.blue, 0.12),
      boxShadow: `inset 3px 0 0 ${tb.blue}`,
      '&:hover': { background: colorManipulator.alpha(tb.blue, 0.18) },
    }),
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
    hidden: css({ color: tb.text, fontSize: theme.typography.bodySmall.fontSize, whiteSpace: 'nowrap' }),
    tag: css({
      display: 'inline-flex',
      alignItems: 'center',
      gap: 4,
      alignSelf: 'center',
      color: tb.text,
      border: `1px solid ${colorManipulator.alpha(tb.blue, 0.6)}`,
      borderRadius: theme.shape.radius.pill,
      padding: '0 6px',
      fontSize: theme.typography.bodySmall.fontSize,
      whiteSpace: 'nowrap',
    }),
    kind: {
      number: css({ color: tb.blue, alignSelf: 'center' }),
      text: css({ color: tb.green, alignSelf: 'center' }),
      boolean: css({ color: tb.orange, alignSelf: 'center' }),
      record: css({ color: tb.cyan, alignSelf: 'center' }),
      unknown: css({ color: theme.colors.text.secondary, alignSelf: 'center' }),
    } as Record<LiveProperty['kind'], string>,
    boxWrap: css({ display: 'inline-flex', flexShrink: 0 }),
    footer: css({
      display: 'flex',
      alignItems: 'center',
      gap: theme.spacing(1),
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
    }),
  };
};

import React, { useEffect, useState } from 'react';
import { css, cx } from '@emotion/css';
import { colorManipulator, GrafanaTheme2 } from '@grafana/data';
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
  Stack,
  useStyles2,
} from '@grafana/ui';

import { DataSource } from '../datasource';
import { RelationshipColor, SelectionVariable } from '../types';
import { timebase } from './timebase';

const LOGO = 'public/plugins/flowsoftware-timebaseatlas-datasource/img/logo.svg';

interface Props {
  datasource: DataSource;
  branch: string;
  domain: string;
  /** Relationship type URIs and $variables; empty means every relationship. */
  value?: string[];
  /** Shown when nothing is picked. */
  placeholder?: string;
  /** The graph's start instance(s): the modal can count and list only what is around it. */
  instanceId?: string;
  /** Hops around the instance, as in the graph. */
  depth?: number;
  onChange: (v: string[]) => void;
}

/**
 * Relationship types, picked the same way as instances: a Select button opens
 * the Atlas modal (every relationship type of the domain with its line
 * colour, dashboard variables first), and the picks show as chips.
 */
export function RelationshipField({
  datasource,
  branch,
  domain,
  value = [],
  placeholder,
  instanceId,
  depth,
  onChange,
}: Props) {
  const styles = useStyles2(getStyles);
  const [open, setOpen] = useState(false);
  const worldKey = `${branch}/${domain}`;
  const [state, setState] = useState<{ key: string; types?: RelationshipColor[]; error?: string; loading: boolean }>();

  const load = () => {
    setState({ key: worldKey, loading: true });
    datasource
      .getRelationshipColors(branch, domain)
      .then((types) => setState({ key: worldKey, types, loading: false }))
      .catch((e) => setState({ key: worldKey, loading: false, error: e?.data?.message ?? e?.message ?? String(e) }));
  };
  // With picks, load up front so the chips show names and colours.
  const loadedKey = state?.key;
  const hasPicks = value.length > 0;
  useEffect(() => {
    if (!hasPicks || !domain || loadedKey === worldKey) {
      return;
    }
    let live = true;
    datasource
      .getRelationshipColors(branch, domain)
      .then(
        (types) =>
          live && setState((s) => (s?.key === worldKey && s.types ? s : { key: worldKey, types, loading: false }))
      )
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [datasource, hasPicks, branch, domain, worldKey, loadedKey]);

  const current = state?.key === worldKey ? state : undefined;
  const byUri = new Map((current?.types ?? []).map((t) => [t.uri, t]));
  const variables = current?.types ? datasource.relationshipVariables(current.types) : [];

  return (
    <div className={styles.field}>
      <Stack gap={0.5} alignItems="center" wrap="wrap">
        <Button
          variant="secondary"
          icon="link"
          disabled={!domain}
          tooltip={domain ? 'Pick the relationship types to follow' : 'Pick a domain first'}
          onClick={() => {
            setOpen(true);
            if (!current || (!current.types && !current.loading)) {
              load();
            }
          }}
        >
          {hasPicks ? 'Change' : 'Select relationships'}
        </Button>
        {value.map((v) => {
          const t = byUri.get(v);
          const isVar = v.startsWith('$');
          return (
            <span key={v} className={styles.chip} title={isVar ? `Dashboard variable ${v}` : v}>
              {isVar ? (
                <Icon name="dollar-alt" size="xs" className={styles.varIcon} />
              ) : (
                <Swatch color={t?.color ?? '#9FA7B3'} dashed={!!t && !t.isContainment} />
              )}
              <span className={styles.name}>{t?.name ?? (isVar ? v : shortUri(v))}</span>
              <IconButton
                name="times"
                size="xs"
                aria-label={`Remove ${t?.name ?? v}`}
                onClick={() => onChange(value.filter((x) => x !== v))}
              />
            </span>
          );
        })}
        {!hasPicks && <span className={styles.meta}>{placeholder ?? 'every relationship'}</span>}
        {value.length > 1 && (
          <Button size="sm" variant="secondary" fill="text" onClick={() => onChange([])}>
            Clear
          </Button>
        )}
      </Stack>
      {open && (
        <RelationshipBrowser
          title={`Relationships in ${domain}${branch ? ` @ ${branch}` : ''}`}
          types={current?.types}
          loading={!!current?.loading}
          error={current?.error}
          variables={variables}
          value={value}
          instanceId={instanceId?.trim() || undefined}
          instanceLabel={instanceId?.trim() ? datasource.expandList(instanceId.trim()) : undefined}
          depth={depth}
          loadCounts={(around) =>
            datasource.getRelationshipCounts(branch, domain, around ? instanceId : undefined, depth)
          }
          onApply={(v) => {
            setOpen(false);
            onChange(v);
          }}
          onRefresh={load}
          onDismiss={() => setOpen(false)}
        />
      )}
    </div>
  );
}

function shortUri(uri: string): string {
  return uri.slice(Math.max(uri.lastIndexOf(':'), uri.lastIndexOf('/'), uri.lastIndexOf('#')) + 1);
}

/** The line as drawn in the graph: solid for containment, dashed for other relationships. */
function Swatch({ color, dashed }: { color: string; dashed: boolean }) {
  return (
    <svg width="18" height="8" aria-hidden style={{ flexShrink: 0 }}>
      <line
        x1="1"
        y1="4"
        x2="17"
        y2="4"
        stroke={color}
        strokeWidth="2.5"
        strokeDasharray={dashed ? '4 3' : undefined}
      />
    </svg>
  );
}

function RelationshipBrowser({
  title,
  types,
  loading,
  error,
  variables,
  value,
  instanceId,
  instanceLabel,
  depth,
  loadCounts,
  onApply,
  onRefresh,
  onDismiss,
}: {
  title: string;
  types?: RelationshipColor[];
  loading: boolean;
  error?: string;
  variables: SelectionVariable[];
  value: string[];
  instanceId?: string;
  instanceLabel?: string;
  depth?: number;
  loadCounts: (around: boolean) => Promise<Record<string, number>>;
  onApply: (v: string[]) => void;
  onRefresh: () => void;
  onDismiss: () => void;
}) {
  const styles = useStyles2(getStyles);
  const [filter, setFilter] = useState('');
  const [picked, setPicked] = useState(() => new Set(value));
  // With a start instance, list only the types around it (as the graph walks it).
  const [around, setAround] = useState(!!instanceId);
  const [counts, setCounts] = useState<{ key: string; n?: Record<string, number>; error?: string }>();
  const [countsKey, setCountsKey] = useState(0);
  useEffect(() => {
    let live = true;
    const key = `${around}/${countsKey}`;
    loadCounts(around)
      .then((n) => live && setCounts({ key, n }))
      .catch((e) => live && setCounts({ key, error: e?.data?.message ?? e?.message ?? String(e) }));
    return () => {
      live = false;
    };
    // loadCounts is a new function each render; reload on scope or refresh only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [around, countsKey]);
  const current = counts?.key === `${around}/${countsKey}` ? counts : undefined;
  const n = current?.n;
  const countOf = (uri: string) => n?.[uri] ?? 0;
  const f = filter.trim().toLowerCase();
  // Around an instance, types it does not have are hidden (unless ticked).
  const inScope = (types ?? []).filter((t) => !around || !n || countOf(t.uri) > 0 || picked.has(t.uri));
  const hidden = (types?.length ?? 0) - inScope.length;
  const shown = inScope
    .filter((t) => !f || t.name.toLowerCase().includes(f) || t.uri.toLowerCase().includes(f))
    .sort((a, b) => (n ? countOf(b.uri) - countOf(a.uri) : 0) || a.name.localeCompare(b.name));
  const shownVars = variables.filter((v) => !f || v.name.toLowerCase().includes(f.replace(/^\$/, '')));
  const flip = (id: string) =>
    setPicked((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });

  const row = (
    id: string,
    on: boolean,
    label: React.ReactNode,
    meta: string,
    icon: React.ReactNode,
    count?: number
  ) => (
    <li key={id}>
      <div className={cx(styles.row, on && styles.selected)}>
        <Checkbox value={on} onChange={() => flip(id)} aria-label={id} />
        <button type="button" className={styles.pick} onClick={() => flip(id)}>
          {icon}
          <span className={styles.name}>{label}</span>
          <span className={styles.meta}>{meta}</span>
          {count !== undefined && <span className={cx(styles.count, count === 0 && styles.zero)}>{count}</span>}
        </button>
      </div>
    </li>
  );

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
          placeholder="Filter relationships"
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
        />
        <Button
          size="sm"
          variant="secondary"
          onClick={() => setPicked(new Set([...picked, ...shown.map((t) => t.uri)]))}
        >
          All
        </Button>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => setPicked(new Set([...picked].filter((x) => !shown.some((t) => t.uri === x))))}
        >
          None
        </Button>
        <IconButton
          name="sync"
          tooltip="Reload from Atlas"
          onClick={() => {
            onRefresh();
            setCountsKey((k) => k + 1);
          }}
        />
      </div>
      {instanceId && (
        <div className={styles.scope}>
          <RadioButtonGroup
            size="sm"
            value={around}
            onChange={setAround}
            options={[
              {
                label: `Around ${instanceLabel || instanceId}`,
                value: true,
                description: `Relationships within ${depth ?? 1} ${(depth ?? 1) === 1 ? 'hop' : 'hops'} of the graph's start instance${instanceLabel && instanceLabel !== instanceId ? ` (${instanceId})` : ''}`,
              },
              { label: 'Whole domain', value: false, description: 'Every relationship type of the domain' },
            ]}
          />
        </div>
      )}
      {current?.error && (
        <Alert severity="warning" title="Could not count the relationships">
          {current.error}
        </Alert>
      )}
      {error && (
        <Alert severity="error" title="Could not load the relationship types">
          {error}
        </Alert>
      )}
      {loading && !types && <LoadingPlaceholder text="Loading relationship types…" />}
      {types && (
        <>
          <div className={styles.summary}>
            {picked.size === 0
              ? `Nothing ticked: every relationship is followed (${inScope.length} types)`
              : `${picked.size} of ${inScope.length} ticked: only these are followed and drawn`}
            {around && n && hidden > 0 && ` · ${hidden} other types are not around ${instanceLabel || instanceId}`}
            {!n && !current?.error && ' · counting…'}
          </div>
          <div className={styles.scroll}>
            {shownVars.length > 0 && (
              <>
                <div className={styles.section}>Dashboard variables</div>
                <ul className={styles.list}>
                  {shownVars.map((v) =>
                    row(
                      '$' + v.name,
                      picked.has('$' + v.name),
                      '$' + v.name,
                      `now ${v.values
                        .slice(0, 3)
                        .map(shortUri)
                        .join(', ')}${v.values.length > 3 ? `, +${v.values.length - 3}` : ''}`,
                      <Icon name="dollar-alt" size="sm" className={styles.varIcon} />
                    )
                  )}
                </ul>
                <div className={styles.section}>Relationship types</div>
              </>
            )}
            <ul className={styles.list}>
              {shown.map((t) =>
                row(
                  t.uri,
                  picked.has(t.uri),
                  t.name,
                  t.isContainment ? 'containment' : 'relationship',
                  <Swatch color={t.color} dashed={!t.isContainment} />,
                  n ? countOf(t.uri) : undefined
                )
              )}
            </ul>
          </div>
        </>
      )}
      <Modal.ButtonRow>
        <Button variant="secondary" fill="outline" onClick={onDismiss}>
          Cancel
        </Button>
        <Button onClick={() => onApply([...picked])}>Apply</Button>
      </Modal.ButtonRow>
    </Modal>
  );
}

const getStyles = (theme: GrafanaTheme2) => {
  const tb = timebase(theme);
  return {
    field: css({ display: 'flex', flexDirection: 'column', gap: theme.spacing(0.5), minWidth: 0 }),
    chip: css({
      display: 'inline-flex',
      alignItems: 'center',
      gap: theme.spacing(0.5),
      padding: theme.spacing(0.25, 0.5, 0.25, 1),
      borderRadius: theme.shape.radius.pill,
      background: colorManipulator.alpha(tb.blue, 0.1),
      border: `1px solid ${colorManipulator.alpha(tb.blue, 0.45)}`,
      fontSize: theme.typography.bodySmall.fontSize,
      maxWidth: 360,
    }),
    varIcon: css({ color: tb.orange, flexShrink: 0, alignSelf: 'center' }),
    modal: css({ width: 640, maxWidth: '95vw' }),
    title: css({ display: 'inline-flex', alignItems: 'center', gap: theme.spacing(1) }),
    logo: css({ width: 22, height: 22 }),
    toolbar: css({ display: 'flex', gap: theme.spacing(1), alignItems: 'center', marginBottom: theme.spacing(1) }),
    summary: css({
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
      margin: theme.spacing(0.5, 0),
    }),
    section: css({
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
      fontWeight: theme.typography.fontWeightMedium,
      textTransform: 'uppercase',
      letterSpacing: '0.04em',
      padding: theme.spacing(0.75, 1, 0.25),
    }),
    scroll: css({
      height: '50vh',
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
      paddingLeft: theme.spacing(0.5),
      borderRadius: theme.shape.radius.default,
      '&:hover': { background: theme.colors.action.hover },
    }),
    selected: css({
      background: colorManipulator.alpha(tb.blue, 0.12),
      boxShadow: `inset 3px 0 0 ${tb.blue}`,
    }),
    pick: css({
      display: 'flex',
      alignItems: 'center',
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
    scope: css({ marginBottom: theme.spacing(1) }),
    count: css({
      marginLeft: 'auto',
      paddingLeft: theme.spacing(1),
      fontVariantNumeric: 'tabular-nums',
      color: theme.colors.text.primary,
      fontSize: theme.typography.bodySmall.fontSize,
    }),
    zero: css({ color: theme.colors.text.disabled }),
    meta: css({
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    }),
  };
};

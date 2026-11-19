import React, { useState } from 'react';
import { css } from '@emotion/css';
import { colorManipulator, GrafanaTheme2 } from '@grafana/data';
import { Button, Icon, IconButton, Input, Stack, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { HistoryPick, TreeNodeData } from '../types';
import { HistoryBrowser } from './HistoryBrowser';
import { timebase } from './timebase';

interface Props {
  datasource: DataSource;
  branch: string;
  domain: string;
  value: HistoryPick[];
  onChange: (picks: HistoryPick[]) => void;
}

/** Chips shown before "+N more". */
const MAX_CHIPS = 12;

/**
 * The Data selection: chips for what is picked, a Select button that opens
 * the tree with properties, and a box for ids and $variables that are not in
 * the tree ("$machine-Production", "$machine-Production/FeedRate",
 * "type:CNCProduction/FeedRate"). A whole-instance chip can be switched to
 * include everything nested under it.
 */
export function HistoryField({ datasource, branch, domain, value, onChange }: Props) {
  const styles = useStyles2(getStyles);
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [all, setAll] = useState(false);
  const [tree, setTree] = useState<{ key: string; nodes?: TreeNodeData[]; error?: string; loading: boolean }>();
  const worldKey = `${branch}/${domain}`;

  const load = (refresh = false) => {
    setTree({
      key: worldKey,
      loading: true,
      nodes: refresh ? undefined : tree?.key === worldKey ? tree.nodes : undefined,
    });
    datasource
      .getTree(branch, domain, refresh)
      .then((nodes) => setTree({ key: worldKey, nodes, loading: false }))
      .catch((e) => setTree({ key: worldKey, loading: false, error: e?.data?.message ?? e?.message ?? String(e) }));
  };

  const openBrowser = () => {
    setOpen(true);
    if (tree?.key !== worldKey || (!tree.nodes && !tree.loading)) {
      load();
    }
  };

  const add = () => {
    const pick = parsePick(text);
    if (pick && !value.some((p) => samePick(p, pick))) {
      onChange([...value, pick]);
    }
    setText('');
  };

  const current = tree?.key === worldKey ? tree : undefined;
  const shown = all ? value : value.slice(0, MAX_CHIPS);

  return (
    <div className={styles.field}>
      <Stack gap={0.5} alignItems="center" wrap="wrap">
        <Button
          variant="secondary"
          icon="sitemap"
          onClick={openBrowser}
          disabled={!domain}
          tooltip={domain ? 'Pick instances and properties in the tree' : 'Pick a domain first'}
        >
          {value.length ? 'Change selection' : 'Select instances and properties'}
        </Button>
        <Input
          width={40}
          value={text}
          placeholder="or type an id, $variable or type:Name"
          onChange={(e) => setText(e.currentTarget.value)}
          onKeyDown={(e) => e.key === 'Enter' && add()}
          onBlur={() => text.trim() && add()}
        />
        {value.length > 0 && (
          <Button size="sm" variant="secondary" fill="text" onClick={() => onChange([])}>
            Clear
          </Button>
        )}
      </Stack>
      {value.length > 0 && (
        <div className={styles.chips}>
          {shown.map((p, i) => (
            <span
              key={`${p.scope ?? ''}:${p.instance}:${p.type ?? ''}/${p.property ?? ''}`}
              className={styles.chip}
              title={chipTitle(p)}
            >
              {p.scope === 'type' ? (
                <>
                  <Icon name="layer-group" size="xs" className={styles.scopeIcon} />
                  <span className={styles.instance}>every {p.type}</span>
                </>
              ) : (
                <>
                  {p.scope === 'nested' && <Icon name="sitemap" size="xs" className={styles.scopeIcon} />}
                  <span className={styles.instance}>{p.instance}</span>
                  {p.scope === 'nested' && <span className={styles.prop}>+ nested</span>}
                </>
              )}
              <span className={styles.prop}>{p.property ?? 'all live values'}</span>
              {p.scope !== 'type' && (
                <IconButton
                  name="sitemap"
                  size="xs"
                  className={p.scope === 'nested' ? styles.on : undefined}
                  tooltip={
                    p.scope === 'nested'
                      ? 'Includes everything nested under it. Click for this instance only.'
                      : 'Click to include everything nested under it, also instances added later'
                  }
                  onClick={() =>
                    onChange(
                      value.map((q, j) => (j === i ? { ...q, scope: q.scope === 'nested' ? undefined : 'nested' } : q))
                    )
                  }
                />
              )}
              <IconButton
                name="times"
                size="xs"
                aria-label={`Remove ${chipTitle(p)}`}
                onClick={() => onChange(value.filter((_, j) => j !== i))}
              />
            </span>
          ))}
          {value.length > MAX_CHIPS && (
            <Button size="sm" variant="secondary" fill="text" onClick={() => setAll(!all)}>
              {all ? 'Show less' : `+${value.length - MAX_CHIPS} more`}
            </Button>
          )}
        </div>
      )}
      <HistoryBrowser
        isOpen={open}
        title={`Instances and properties in ${domain}${branch ? ` @ ${branch}` : ''}`}
        nodes={current?.nodes}
        loading={!!current?.loading}
        error={current?.error}
        selection={value}
        onApply={(picks) => {
          setOpen(false);
          onChange(picks);
        }}
        onRefresh={() => load(true)}
        onDismiss={() => setOpen(false)}
      />
    </div>
  );
}

/**
 * "id", "id/Property", "type:Name" or "type:Name/Property"; the part after
 * the last "/" is a property only when it is a plain name.
 */
export function parsePick(text: string): HistoryPick | undefined {
  let t = text.trim();
  const isType = /^type:/i.test(t);
  if (isType) {
    t = t.slice(5).trim();
  }
  if (!t) {
    return undefined;
  }
  let property: string | undefined;
  const i = t.lastIndexOf('/');
  if (i > 0 && /^([A-Za-z_][A-Za-z0-9_]*|\$\{?[A-Za-z0-9_]+\}?)$/.test(t.slice(i + 1))) {
    property = t.slice(i + 1);
    t = t.slice(0, i);
  }
  const pick: HistoryPick = isType ? { instance: '', type: t, scope: 'type' } : { instance: t };
  return property ? { ...pick, property } : pick;
}

function samePick(a: HistoryPick, b: HistoryPick): boolean {
  return (
    a.instance === b.instance &&
    a.property === b.property &&
    (a.scope ?? '') === (b.scope ?? '') &&
    (a.scope !== 'type' || a.type === b.type)
  );
}

function chipTitle(p: HistoryPick): string {
  const what = p.property ?? 'every live value';
  if (p.scope === 'type') {
    return `Every ${p.type} (including instances added later): ${what}`;
  }
  if (p.scope === 'nested') {
    return `${p.instance} and everything nested under it (including instances added later): ${what}`;
  }
  return `${p.instance}: ${what}`;
}

const getStyles = (theme: GrafanaTheme2) => {
  const tb = timebase(theme);
  return {
    field: css({ display: 'flex', flexDirection: 'column', gap: theme.spacing(0.5), minWidth: 0, flex: 1 }),
    chips: css({ display: 'flex', flexWrap: 'wrap', gap: theme.spacing(0.5), alignItems: 'center' }),
    chip: css({
      display: 'inline-flex',
      alignItems: 'center',
      gap: theme.spacing(0.5),
      padding: theme.spacing(0.25, 0.5, 0.25, 1),
      borderRadius: theme.shape.radius.pill,
      background: colorManipulator.alpha(tb.blue, 0.1),
      border: `1px solid ${colorManipulator.alpha(tb.blue, 0.45)}`,
      fontSize: theme.typography.bodySmall.fontSize,
      maxWidth: 420,
    }),
    scopeIcon: css({ color: tb.text, flexShrink: 0 }),
    on: css({ color: tb.text }),
    instance: css({
      fontWeight: theme.typography.fontWeightMedium,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    }),
    prop: css({ color: theme.colors.text.secondary, whiteSpace: 'nowrap' }),
  };
};

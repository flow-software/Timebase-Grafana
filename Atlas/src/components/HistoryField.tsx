import React, { useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Button, IconButton, Input, Stack, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { HistoryPick, TreeNodeData } from '../types';
import { HistoryBrowser } from './HistoryBrowser';

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
 * The History selection: chips for what is picked, a Select button that
 * opens the tree with properties, and a box for ids and $variables that are
 * not in the tree ("$machine-Production" or "$machine-Production/FeedRate").
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
    if (pick && !value.some((p) => p.instance === pick.instance && p.property === pick.property)) {
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
          width={34}
          value={text}
          placeholder="or type an id or $variable[/Property]"
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
            <span key={`${p.instance}/${p.property ?? ''}`} className={styles.chip} title={chipTitle(p)}>
              <span className={styles.instance}>{p.instance}</span>
              <span className={styles.prop}>{p.property ?? 'all live values'}</span>
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
        title={`History series in ${domain}${branch ? ` @ ${branch}` : ''}`}
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

/** "id" or "id/Property"; the part after the last "/" is a property only when it is a plain name. */
export function parsePick(text: string): HistoryPick | undefined {
  const t = text.trim();
  if (!t) {
    return undefined;
  }
  const i = t.lastIndexOf('/');
  if (i > 0 && /^[A-Za-z_][A-Za-z0-9_]*$/.test(t.slice(i + 1))) {
    return { instance: t.slice(0, i), property: t.slice(i + 1) };
  }
  return { instance: t };
}

function chipTitle(p: HistoryPick): string {
  return p.property ? `${p.instance} ${p.property}` : `${p.instance}: every live value`;
}

const getStyles = (theme: GrafanaTheme2) => ({
  field: css({ display: 'flex', flexDirection: 'column', gap: theme.spacing(0.5), minWidth: 0, flex: 1 }),
  chips: css({ display: 'flex', flexWrap: 'wrap', gap: theme.spacing(0.5), alignItems: 'center' }),
  chip: css({
    display: 'inline-flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    padding: theme.spacing(0.25, 0.5, 0.25, 1),
    borderRadius: theme.shape.radius.pill,
    background: theme.colors.background.secondary,
    border: `1px solid ${theme.colors.border.weak}`,
    fontSize: theme.typography.bodySmall.fontSize,
    maxWidth: 360,
  }),
  instance: css({
    fontWeight: theme.typography.fontWeightMedium,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  }),
  prop: css({ color: theme.colors.text.secondary, whiteSpace: 'nowrap' }),
});

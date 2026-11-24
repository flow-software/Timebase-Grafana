import React, { useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { colorManipulator, GrafanaTheme2 } from '@grafana/data';
import { Button, Icon, IconButton, Stack, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { EnumInfo, TreeNodeData } from '../types';
import { InstanceBrowser } from './InstanceBrowser';
import { timebase } from './timebase';

interface Props {
  datasource: DataSource;
  branch: string;
  domain: string;
  /** The picked enumeration's local name. */
  value?: string;
  onChange: (e: EnumInfo | undefined) => void;
}

const SEP = '\u0000';

/** The enumerations as a tree for the shared modal: each enumeration, its members underneath. */
export function enumTree(list: EnumInfo[]): TreeNodeData[] {
  return list.flatMap((e) => [
    { id: e.name, label: e.label, type: 'enumeration' },
    ...e.members.map((m) => ({ id: e.name + SEP + m.key, label: m.label, type: 'member', parentId: e.name })),
  ]);
}

/**
 * One enumeration, picked the same way as instances: a Select button opens
 * the Atlas modal (enumerations with their members underneath), and the pick
 * shows as a chip.
 */
export function EnumField({ datasource, branch, domain, value, onChange }: Props) {
  const styles = useStyles2(getStyles);
  const [open, setOpen] = useState(false);
  const worldKey = `${branch}/${domain}`;
  const [state, setState] = useState<{ key: string; list?: EnumInfo[]; error?: string; loading: boolean }>();

  const load = () => {
    setState({ key: worldKey, loading: true });
    datasource
      .getEnums(branch, domain)
      .then((list) => setState({ key: worldKey, list, loading: false }))
      .catch((e) => setState({ key: worldKey, loading: false, error: e?.data?.message ?? e?.message ?? String(e) }));
  };
  const current = state?.key === worldKey ? state : undefined;
  // With a pick, load up front so the chip shows its label and members.
  const loadedKey = state?.key;
  useEffect(() => {
    if (!value || !domain || loadedKey === worldKey) {
      return;
    }
    let live = true;
    datasource
      .getEnums(branch, domain)
      .then(
        (list) => live && setState((s) => (s?.key === worldKey && s.list ? s : { key: worldKey, list, loading: false }))
      )
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [datasource, value, branch, domain, worldKey, loadedKey]);
  const byName = new Map((current?.list ?? []).map((e) => [e.name, e]));
  const picked = value ? byName.get(value) : undefined;

  return (
    <div className={styles.field}>
      <Stack gap={0.5} alignItems="center" wrap="wrap">
        <Button
          variant="secondary"
          icon="list-ul"
          disabled={!domain}
          tooltip={domain ? 'Pick an enumeration of this domain' : 'Pick a domain first'}
          onClick={() => {
            setOpen(true);
            if (!current || (!current.list && !current.loading)) {
              load();
            }
          }}
        >
          {value ? 'Change' : 'Select enumeration'}
        </Button>
        {value ? (
          <span className={styles.chip} title={picked?.members.map((m) => `${m.key} = ${m.label}`).join('\n')}>
            <Icon name="list-ul" size="xs" className={styles.icon} />
            <span className={styles.name}>{picked?.label ?? value}</span>
            {picked && (
              <span className={styles.meta}>
                {picked.members
                  .slice(0, 4)
                  .map((m) => m.label)
                  .join(', ')}
                {picked.members.length > 4 ? ', …' : ''}
              </span>
            )}
            <IconButton name="times" size="xs" aria-label={`Clear ${value}`} onClick={() => onChange(undefined)} />
          </span>
        ) : (
          <span className={styles.meta}>no enumeration picked</span>
        )}
      </Stack>
      <InstanceBrowser
        isOpen={open}
        title={`Enumerations in ${domain}${branch ? ` @ ${branch}` : ''}`}
        nodes={current?.list ? enumTree(current.list) : undefined}
        loading={!!current?.loading}
        error={current?.error}
        selected={value}
        noun="enumerations"
        what="enumerations"
        filterPlaceholder="Filter by enumeration or member"
        canSelect={(n) => n.type === 'enumeration'}
        metaOf={(n) => {
          if (n.type === 'member') {
            return `key ${n.id.split(SEP)[1]}`;
          }
          const e = byName.get(n.id);
          return e ? `${e.members.length} members${e.keyKind ? ` · ${e.keyKind} keys` : ''}` : '';
        }}
        onSelect={(id) => {
          setOpen(false);
          onChange(byName.get(id));
        }}
        onRefresh={load}
        onDismiss={() => setOpen(false)}
      />
    </div>
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
      maxWidth: 520,
    }),
    icon: css({ color: tb.green, flexShrink: 0 }),
    name: css({ fontWeight: theme.typography.fontWeightMedium, whiteSpace: 'nowrap' }),
    meta: css({
      color: theme.colors.text.secondary,
      fontSize: theme.typography.bodySmall.fontSize,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    }),
  };
};

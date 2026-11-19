import React, { useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { colorManipulator, GrafanaTheme2 } from '@grafana/data';
import { Button, Dropdown, Icon, IconButton, Input, Menu, Stack, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { ancestorsOf, shortType } from '../tree';
import { HistoryPick, SelectionVariables, TreeNodeData } from '../types';
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

  // With dashboard variables, the tree is loaded up front so chips can offer them.
  const hasVars = datasource.hasDashboardVariables();
  useEffect(() => {
    if (!hasVars || !domain) {
      return;
    }
    const key = `${branch}/${domain}`;
    let live = true;
    datasource
      .getTree(branch, domain)
      .then((nodes) => live && setTree((t) => (t?.key === key && t.nodes ? t : { key, nodes, loading: false })))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [datasource, hasVars, branch, domain]);

  const current = tree?.key === worldKey ? tree : undefined;
  const variables = current?.nodes && hasVars ? datasource.selectionVariables(current.nodes) : undefined;
  const shown = all ? value : value.slice(0, MAX_CHIPS);
  const replaceAt = (i: number, pick: HistoryPick) => {
    const rest = value.filter((_, j) => j !== i);
    onChange(rest.some((q) => samePick(q, pick)) ? rest : value.map((q, j) => (j === i ? pick : q)));
  };

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
              ) : p.scope === 'nested' && p.nestedType ? (
                <>
                  <Icon name="sitemap" size="xs" className={styles.scopeIcon} />
                  <span className={styles.instance}>
                    every {shortType(p.nestedType)} under {p.instance}
                  </span>
                </>
              ) : (
                <>
                  {p.scope === 'nested' && <Icon name="sitemap" size="xs" className={styles.scopeIcon} />}
                  <span className={styles.instance}>{p.instance}</span>
                  {p.scope === 'nested' && <span className={styles.prop}>+ nested</span>}
                </>
              )}
              <span className={styles.prop}>{p.property ?? 'all live values'}</span>
              {variables && (
                <VariableSuggestions
                  options={suggestions(p, current?.nodes ?? [], variables)}
                  onPick={(pick) => replaceAt(i, pick)}
                />
              )}
              {p.scope !== 'type' && !p.nestedType && (
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
        variables={variables}
        resolve={(picks) => datasource.resolvePicks(picks)}
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
    (a.nestedType ?? '') === (b.nestedType ?? '') &&
    (a.scope !== 'type' || a.type === b.type)
  );
}

export interface Suggestion {
  label: string;
  pick: HistoryPick;
}

/**
 * Dashboard variables that could stand in a pick: an instance that is one of
 * an instance variable's values ("Use $machine instead of CNC01"), an
 * instance nested under one ("Use every CNCProduction under $machine"), or a
 * property that is one of a property variable's values.
 */
export function suggestions(p: HistoryPick, nodes: TreeNodeData[], vars: SelectionVariables): Suggestion[] {
  const out: Suggestion[] = [];
  const { type: _type, ...rest } = p;
  if (p.scope !== 'type' && !p.instance.includes('$')) {
    const node = nodes.find((n) => n.id === p.instance);
    const above = ancestorsOf(nodes, p.instance);
    for (const v of vars.instances) {
      if (v.values.includes(p.instance)) {
        out.push({ label: `Use $${v.name} instead of ${p.instance}`, pick: { ...rest, instance: '$' + v.name } });
      } else if (!p.scope && node && above.some((a) => v.values.includes(a))) {
        out.push({
          label: `Use every ${node.type} under $${v.name}`,
          pick: {
            instance: '$' + v.name,
            scope: 'nested',
            nestedType: node.type,
            ...(p.property ? { property: p.property } : {}),
          },
        });
      }
    }
  }
  if (p.property && !p.property.includes('$')) {
    for (const v of vars.properties) {
      if (v.values.includes(p.property)) {
        out.push({ label: `Use $${v.name} instead of ${p.property}`, pick: { ...p, property: '$' + v.name } });
      }
    }
  }
  return out;
}

export function VariableSuggestions({ options, onPick }: { options: Suggestion[]; onPick: (p: HistoryPick) => void }) {
  if (options.length === 0) {
    return null;
  }
  return (
    <Dropdown
      overlay={
        <Menu>
          {options.map((o) => (
            <Menu.Item key={o.label} label={o.label} icon="dollar-alt" onClick={() => onPick(o.pick)} />
          ))}
        </Menu>
      }
    >
      <IconButton name="dollar-alt" size="xs" tooltip="Use a dashboard variable here" />
    </Dropdown>
  );
}

function chipTitle(p: HistoryPick): string {
  const what = p.property ?? 'every live value';
  if (p.scope === 'type') {
    return `Every ${p.type} (including instances added later): ${what}`;
  }
  if (p.scope === 'nested' && p.nestedType) {
    return `Every ${shortType(p.nestedType)} nested under ${p.instance} (including instances added later): ${what}`;
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

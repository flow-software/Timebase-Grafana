import React, { useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { colorManipulator, GrafanaTheme2 } from '@grafana/data';
import { Button, Icon, IconButton, Input, Stack, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { nodeName } from '../tree';
import { TreeNodeData } from '../types';
import { suggestions, VariableSuggestions } from './HistoryField';
import { InstanceBrowser } from './InstanceBrowser';
import { timebase } from './timebase';

interface Props {
  datasource: DataSource;
  branch: string;
  domain: string;
  value?: string;
  /** Shown when nothing is picked, e.g. "whole domain". */
  placeholder: string;
  onChange: (id: string) => void;
}

/**
 * One instance, in the same style as the Data selection: a Select button
 * that opens the tree (with dashboard variables listed first), a box for
 * typed ids and $variables, and the pick as a chip. The chip's $ button
 * offers a variable that holds the instance ("Use $machine instead of CNC02").
 */
export function InstanceField({ datasource, branch, domain, value, placeholder, onChange }: Props) {
  const styles = useStyles2(getStyles);
  const [text, setText] = useState('');
  const [open, setOpen] = useState(false);
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

  // With dashboard variables, the tree is loaded up front so the chip can offer them.
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

  const openBrowser = () => {
    setOpen(true);
    if (tree?.key !== worldKey || (!tree.nodes && !tree.loading)) {
      load();
    }
  };

  const commit = (id: string) => {
    const v = id.trim();
    setText('');
    if (v && v !== (value ?? '')) {
      onChange(v);
    }
  };

  const current = tree?.key === worldKey ? tree : undefined;
  const variables = current?.nodes && hasVars ? datasource.selectionVariables(current.nodes) : undefined;
  const picked = current?.nodes?.find((n) => n.id === value);
  const isVar = !!value && value.includes('$');
  // Only straight swaps make sense for a start point (not "every X under $var").
  const swaps =
    value && variables && current?.nodes
      ? suggestions({ instance: value }, current.nodes, { instances: variables.instances, properties: [] }).filter(
          (s) => !s.pick.scope
        )
      : [];

  return (
    <div className={styles.field}>
      <Stack gap={0.5} alignItems="center" wrap="wrap">
        <Button
          variant="secondary"
          icon="sitemap"
          onClick={openBrowser}
          disabled={!domain}
          tooltip={domain ? 'Pick an instance or a dashboard variable in the tree' : 'Pick a domain first'}
        >
          {value ? 'Change' : 'Select instance'}
        </Button>
        <Input
          width={30}
          value={text}
          placeholder="or type an id or $variable"
          onChange={(e) => setText(e.currentTarget.value)}
          onBlur={() => text.trim() && commit(text)}
          onKeyDown={(e) => e.key === 'Enter' && commit(text)}
        />
        {value ? (
          <span className={styles.chip} title={isVar ? `Dashboard variable ${value}` : value}>
            {isVar && <Icon name="dollar-alt" size="xs" className={styles.varIcon} />}
            <span className={styles.name}>{picked ? nodeName(picked) : value}</span>
            {picked && picked.label && picked.label !== picked.id && <span className={styles.meta}>{picked.id}</span>}
            {picked && <span className={styles.meta}>{picked.type}</span>}
            <VariableSuggestions options={swaps} onPick={(p) => onChange(p.instance)} />
            <IconButton name="times" size="xs" aria-label={`Clear ${value}`} onClick={() => onChange('')} />
          </span>
        ) : (
          <span className={styles.meta}>{placeholder}</span>
        )}
      </Stack>
      <InstanceBrowser
        isOpen={open}
        title={`Instances in ${domain}${branch ? ` @ ${branch}` : ''}`}
        nodes={current?.nodes}
        loading={!!current?.loading}
        error={current?.error}
        selected={value}
        variables={variables?.instances}
        onSelect={(id) => {
          setOpen(false);
          commit(id);
        }}
        onRefresh={() => load(true)}
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
      maxWidth: 420,
    }),
    varIcon: css({ color: tb.orange, flexShrink: 0 }),
    name: css({
      fontWeight: theme.typography.fontWeightMedium,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    }),
    meta: css({
      color: theme.colors.text.secondary,
      whiteSpace: 'nowrap',
      fontSize: theme.typography.bodySmall.fontSize,
    }),
  };
};

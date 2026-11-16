import React, { useState } from 'react';
import { Button, Input, Stack } from '@grafana/ui';

import { DataSource } from '../datasource';
import { nodeName } from '../tree';
import { TreeNodeData } from '../types';
import { InstanceBrowser } from './InstanceBrowser';

interface Props {
  datasource: DataSource;
  branch: string;
  domain: string;
  value?: string;
  placeholder: string;
  onChange: (id: string) => void;
}

/**
 * An instance id: typed (ids and $variables) or picked from the browse tree.
 * The tree is fetched when the modal first opens for a world.
 */
export function InstanceField({ datasource, branch, domain, value, placeholder, onChange }: Props) {
  const [text, setText] = useState(value ?? '');
  const [open, setOpen] = useState(false);
  const [tree, setTree] = useState<{ key: string; nodes?: TreeNodeData[]; error?: string; loading: boolean }>();
  const worldKey = `${branch}/${domain}`;

  // Follow outside changes (a saved query loaded, Edit as Cypher and back).
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    setText(value ?? '');
  }

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

  const commit = (id: string) => {
    const v = id.trim();
    if (v !== (value ?? '')) {
      onChange(v);
    }
  };

  const current = tree?.key === worldKey ? tree : undefined;
  const picked = current?.nodes?.find((n) => n.id === value);

  return (
    <Stack gap={0.5} alignItems="center">
      <Input
        width={36}
        value={text}
        placeholder={placeholder}
        onChange={(e) => setText(e.currentTarget.value)}
        onBlur={() => commit(text)}
        onKeyDown={(e) => e.key === 'Enter' && commit(text)}
        suffix={
          picked && picked.label && picked.label !== picked.id ? (
            <span style={{ opacity: 0.7 }}>{nodeName(picked)}</span>
          ) : undefined
        }
      />
      <Button
        variant="secondary"
        icon="sitemap"
        onClick={openBrowser}
        disabled={!domain}
        tooltip={domain ? undefined : 'Pick a domain first'}
      >
        Browse
      </Button>
      <InstanceBrowser
        isOpen={open}
        title={`Instances in ${domain}${branch ? ` @ ${branch}` : ''}`}
        nodes={current?.nodes}
        loading={!!current?.loading}
        error={current?.error}
        selected={value}
        onSelect={(id) => {
          setOpen(false);
          setText(id);
          commit(id);
        }}
        onRefresh={() => load(true)}
        onDismiss={() => setOpen(false)}
      />
    </Stack>
  );
}

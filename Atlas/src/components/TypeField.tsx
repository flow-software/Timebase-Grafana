import React, { useState } from 'react';
import { Button, Input, Stack } from '@grafana/ui';

import { DataSource } from '../datasource';
import { TreeNodeData, TypeOption } from '../types';
import { InstanceBrowser } from './InstanceBrowser';

interface Props {
  datasource: DataSource;
  branch: string;
  domain: string;
  /** Entity types with instances, already loaded by the query editor. */
  types: TypeOption[];
  value?: string;
  onChange: (type: string) => void;
}

const TYPE_PREFIX = 'type:';

/**
 * An entity type: typed (names and $variables) or picked in the same modal as
 * instances. Types are the top level; each opens to show its instances, which
 * are there to help recognise the type and cannot be picked themselves.
 */
export function TypeField({ datasource, branch, domain, types, value, onChange }: Props) {
  const [text, setText] = useState(value ?? '');
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    setText(value ?? '');
  }
  const [open, setOpen] = useState(false);
  const [tree, setTree] = useState<{ key: string; nodes?: TreeNodeData[]; error?: string; loading: boolean }>();
  const worldKey = `${branch}/${domain}`;

  const load = (refresh = false) => {
    setTree({ key: worldKey, loading: true });
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

  const commit = (t: string) => {
    const v = t.trim();
    if (v !== (value ?? '')) {
      onChange(v);
    }
  };

  const current = tree?.key === worldKey ? tree : undefined;
  // Types as roots, instances under their type.
  const nodes: TreeNodeData[] | undefined = current?.nodes && [
    ...types.map((t) => ({
      id: TYPE_PREFIX + t.name,
      label: t.name,
      type: `${t.count} ${t.count === 1 ? 'instance' : 'instances'} · ${t.uri}`,
    })),
    ...current.nodes.map((n) => ({ ...n, parentId: TYPE_PREFIX + n.type })),
  ];

  return (
    <Stack gap={0.5} alignItems="center">
      <Input
        width={36}
        value={text}
        placeholder="entity type or $variable"
        onChange={(e) => setText(e.currentTarget.value)}
        onBlur={() => commit(text)}
        onKeyDown={(e) => e.key === 'Enter' && commit(text)}
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
        title={`Entity types in ${domain}${branch ? ` @ ${branch}` : ''}`}
        nodes={nodes}
        loading={!!current?.loading}
        error={current?.error}
        selected={value ? TYPE_PREFIX + value : undefined}
        canSelect={(n) => n.id.startsWith(TYPE_PREFIX)}
        metaOf={(n) => (n.id.startsWith(TYPE_PREFIX) ? n.type : n.label && n.label !== n.id ? n.id : '')}
        noun="types"
        onSelect={(id) => {
          const t = id.slice(TYPE_PREFIX.length);
          setOpen(false);
          setText(t);
          commit(t);
        }}
        onRefresh={() => load(true)}
        onDismiss={() => setOpen(false)}
      />
    </Stack>
  );
}

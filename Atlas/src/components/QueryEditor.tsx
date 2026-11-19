import React, { useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { DataFrame, QueryEditorProps, SelectableValue } from '@grafana/data';
import {
  Alert,
  Button,
  CodeEditor,
  Combobox,
  InlineField,
  InlineSwitch,
  Input,
  RadioButtonGroup,
  Stack,
} from '@grafana/ui';

import { DataSource } from '../datasource';
import { HistoryField } from './HistoryField';
import { InstanceField } from './InstanceField';
import { historyCypher, toCypher, valuesCypher } from '../cypher';
import {
  AtlasOptions,
  AtlasQuery,
  AtlasQueryType,
  Aggregation,
  ColorBy,
  HistoryPick,
  CypherFormat,
  LineColorBy,
  RelationshipColor,
  ValuesLayout,
  World,
} from '../types';

type Props = QueryEditorProps<DataSource, AtlasQuery, AtlasOptions>;

/** Editor tabs. Data covers two query types: values (Current) and history (Trend). */
type Tab = 'graph' | 'data' | 'cypher';

const tabs: Array<SelectableValue<Tab>> = [
  { label: 'Graph', value: 'graph', description: 'Entities and relationships, for the Node Graph panel' },
  { label: 'Data', value: 'data', description: 'Current values or trends of the instances and properties you pick' },
  { label: 'Cypher', value: 'cypher', description: 'Write your own read-only Cypher query' },
];

const dataModes: Array<SelectableValue<AtlasQueryType>> = [
  { label: 'Current', value: 'values', description: 'The latest value of each property, for tables, stats and gauges' },
  { label: 'Trend', value: 'history', description: 'Stored values over the panel time range, for time series' },
];

const formats: Array<SelectableValue<CypherFormat>> = [
  { label: 'Auto', value: 'auto', description: 'Node graph when the result has relationships, otherwise a table' },
  { label: 'Table', value: 'table' },
  { label: 'Node graph', value: 'graph' },
  {
    label: 'Time series',
    value: 'timeseries',
    description: 'One series per combination of the text columns (e.g. entity, property), with the time column',
  },
];

const layouts: Array<SelectableValue<ValuesLayout>> = [
  { label: 'Row per property', value: 'rows', description: 'Property, value, quality and time on each row' },
  { label: 'Row per instance', value: 'columns', description: 'A column per property, to compare instances' },
];

const colorModes: Array<SelectableValue<ColorBy>> = [
  { label: 'Data quality', value: 'quality', description: 'Ring split by the quality of the live values' },
  { label: 'Entity type', value: 'type', description: 'One colour per entity type' },
  { label: 'None', value: 'none' },
];

const depths: Array<SelectableValue<number>> = [1, 2, 3, 4].map((d) => ({ label: String(d), value: d }));

const LABEL_WIDTH = 14;

export function QueryEditor({ query: saved, onChange, onRunQuery, datasource, data }: Props) {
  // Older "instances" queries are shown, and saved on the next change, as values of a type.
  const query: AtlasQuery = upgrade(saved);
  const queryType = query.queryType ?? 'graph';
  const tab: Tab =
    queryType === 'values' || queryType === 'history' ? 'data' : queryType === 'cypher' ? 'cypher' : 'graph';
  const hasSelection = (query.selection?.length ?? 0) > 0;
  const scope = query.scope ?? 'instance';
  const layout = query.layout ?? (scope === 'type' ? 'columns' : 'rows');
  const branch = query.branch || datasource.defaultBranch;
  const domain = query.domain || datasource.defaultDomain;

  const [worlds, setWorlds] = useState<World[]>([]);
  const [loadError, setLoadError] = useState<string>();

  useEffect(() => {
    datasource
      .getWorlds()
      .then(setWorlds)
      .catch((e) => setLoadError(messageOf(e)));
  }, [datasource]);

  const update = (patch: Partial<AtlasQuery>, run = true) => {
    onChange({ ...query, ...patch });
    if (run) {
      onRunQuery();
    }
  };

  // A handful of worlds: no need to memoise.
  const branchOptions = unique(worlds.map((w) => w.branch)).map((b) => ({ label: b, value: b }));
  const domainOptions = worlds
    .filter((w) => w.branch === branch)
    .map((w) => ({
      label: w.domain,
      value: w.domain,
      description: w.instanceCount !== undefined ? `${w.instanceCount} instances` : undefined,
    }));
  const asCypher = queryType === 'cypher' ? undefined : toCypher(query);
  // Data selections need the browse tree (types, properties, nested and type picks), so they are converted on click.
  const dataSelection = tab === 'data' && hasSelection;
  const [cypherError, setCypherError] = useState<string>();
  const editAsCypher = () => {
    if (!dataSelection) {
      if (asCypher) {
        update({ queryType: 'cypher', cypher: asCypher, format: 'auto' });
      }
      return;
    }
    setCypherError(undefined);
    datasource
      .getTree(branch, domain)
      .then((nodes) => {
        const trend = queryType === 'history';
        const vars = datasource.cypherVars();
        const r = trend ? historyCypher(query, nodes, vars) : valuesCypher(query, nodes, vars);
        if (r.cypher) {
          update({ queryType: 'cypher', cypher: r.cypher, format: trend ? 'timeseries' : 'auto' });
        } else {
          setCypherError(r.error);
        }
      })
      .catch((e) => setCypherError(messageOf(e)));
  };
  const canCypher = dataSelection || !!asCypher;

  return (
    <Stack direction="column" gap={1}>
      <Stack wrap="wrap" gap={1}>
        <InlineField label="Query type" labelWidth={LABEL_WIDTH}>
          <RadioButtonGroup
            options={tabs}
            value={tab}
            onChange={(v) => v !== tab && update({ queryType: v === 'data' ? 'history' : v })}
          />
        </InlineField>
        {tab === 'data' && (
          <InlineField label="Show" tooltip="The same selection, as its latest values or as a trend over time">
            <RadioButtonGroup options={dataModes} value={queryType} onChange={(v) => update({ queryType: v })} />
          </InlineField>
        )}
      </Stack>

      <Stack wrap="wrap" gap={1}>
        <InlineField label="Branch" labelWidth={LABEL_WIDTH} tooltip="Empty uses the data source default">
          <Combobox
            width={20}
            isClearable
            createCustomValue
            placeholder={datasource.defaultBranch}
            options={branchOptions}
            value={query.branch || null}
            onChange={(o) => update({ branch: o?.value ?? '' })}
          />
        </InlineField>
        <InlineField label="Domain" labelWidth={10} tooltip="Empty uses the data source default">
          <Combobox
            width={30}
            isClearable
            createCustomValue
            placeholder={datasource.defaultDomain || 'pick a domain'}
            options={domainOptions}
            value={query.domain || null}
            onChange={(o) => update({ domain: o?.value ?? '' })}
          />
        </InlineField>
      </Stack>

      {queryType === 'graph' && (
        <Stack wrap="wrap" gap={1}>
          <InlineField
            label="Start at"
            labelWidth={LABEL_WIDTH}
            tooltip="Empty shows the whole domain. Pick an instance to show only what is connected to it."
          >
            <InstanceField
              datasource={datasource}
              branch={branch}
              domain={domain}
              value={query.instanceId}
              placeholder="whole domain"
              onChange={(id) => update({ instanceId: id })}
            />
          </InlineField>
          {query.instanceId && (
            <InlineField label="Depth" tooltip="How many relationships to follow from the start instance">
              <RadioButtonGroup options={depths} value={query.depth ?? 1} onChange={(v) => update({ depth: v })} />
            </InlineField>
          )}
          <LimitField query={query} onChange={update} placeholder="2000" tooltip="Most relationships to return" />
        </Stack>
      )}

      {(queryType === 'graph' ||
        (queryType === 'cypher' && (query.format ?? 'auto') !== 'table' && query.format !== 'timeseries')) && (
        <GraphStyle
          query={query}
          onChange={update}
          datasource={datasource}
          branch={branch}
          domain={domain}
          shownTypes={relationshipTypesIn(data?.series, query.refId)}
        />
      )}

      {tab === 'data' && (
        <>
          <Stack wrap="wrap" gap={1}>
            <InlineField
              grow
              className={css({ minWidth: 0, flex: '1 1 0', '& > *:last-child': { minWidth: 0, flex: '1 1 0' } })}
              label="Selection"
              labelWidth={LABEL_WIDTH}
              tooltip="Instances (every property) or single properties, picked in the tree. Current shows their latest values; Trend draws one series per instance and property."
            >
              <HistoryField
                datasource={datasource}
                branch={branch}
                domain={domain}
                value={query.selection ?? []}
                onChange={(selection) => update({ selection, properties: undefined })}
              />
            </InlineField>
          </Stack>
          {queryType === 'history' ? (
            <HistoryOptions query={query} onChange={update} />
          ) : (
            <Stack wrap="wrap" gap={1}>
              <InlineField label="Layout" labelWidth={LABEL_WIDTH}>
                <RadioButtonGroup options={layouts} value={layout} onChange={(v) => update({ layout: v })} />
              </InlineField>
              {layout === 'columns' && (
                <>
                  <InlineField label="Quality" tooltip="Add a quality code column next to each live value">
                    <InlineSwitch
                      value={!!query.includeQuality}
                      onChange={(e) => update({ includeQuality: e.currentTarget.checked })}
                    />
                  </InlineField>
                  <InlineField label="Timestamps" tooltip="Add a time column next to each live value">
                    <InlineSwitch
                      value={!!query.includeTime}
                      onChange={(e) => update({ includeTime: e.currentTarget.checked })}
                    />
                  </InlineField>
                </>
              )}
            </Stack>
          )}
        </>
      )}

      {queryType === 'cypher' && (
        <>
          <CodeEditor
            language="cypher"
            height={160}
            showLineNumbers
            showMiniMap={false}
            value={query.cypher ?? ''}
            onBlur={(text) => update({ cypher: text })}
            onSave={(text) => update({ cypher: text })}
          />
          <Stack wrap="wrap" gap={1} alignItems="center">
            <InlineField label="Format" labelWidth={LABEL_WIDTH}>
              <RadioButtonGroup
                options={formats}
                value={query.format ?? 'auto'}
                onChange={(v) => update({ format: v })}
              />
            </InlineField>
            <span style={{ opacity: 0.75, fontSize: 12 }}>
              Read-only. Use <code>$name</code> for dashboard variables; they are sent as Cypher parameters (a
              multi-value variable becomes a list, for <code>IN $name</code>). Runs when the editor loses focus or on
              Ctrl+S.
            </span>
          </Stack>
        </>
      )}

      {queryType !== 'cypher' && (
        <Stack>
          <Button
            size="sm"
            variant="secondary"
            icon="brackets-curly"
            disabled={!canCypher}
            tooltip={canCypher ? 'Switch to Cypher mode with this query filled in' : noCypherReason(queryType)}
            onClick={editAsCypher}
          >
            Edit as Cypher
          </Button>
        </Stack>
      )}
      {cypherError && queryType !== 'cypher' && (
        <Alert severity="warning" title="Could not write this as Cypher" onRemove={() => setCypherError(undefined)}>
          {cypherError}
        </Alert>
      )}

      {loadError && (
        <Alert severity="warning" title="Could not load the pickers">
          {loadError}
        </Alert>
      )}
    </Stack>
  );
}

const lineModes: Array<SelectableValue<LineColorBy>> = [
  { label: 'Containment / other', value: 'kind', description: 'Grey solid = containment, orange dashed = other' },
  { label: 'Relationship type', value: 'type', description: 'One colour per relationship type' },
];

function GraphStyle({
  query,
  onChange,
  datasource,
  branch,
  domain,
  shownTypes,
}: {
  shownTypes?: Set<string>;
  query: AtlasQuery;
  onChange: (p: Partial<AtlasQuery>) => void;
  datasource: DataSource;
  branch: string;
  domain: string;
}) {
  const lines = query.lineColorBy ?? 'kind';
  const key = `${branch}/${domain}`;
  const [rel, setRel] = useState<{ key: string; types: RelationshipColor[] }>();
  useEffect(() => {
    if (lines !== 'type' || !domain) {
      return;
    }
    let cancelled = false;
    datasource
      .getRelationshipColors(branch, domain)
      .then((types) => !cancelled && setRel({ key, types }))
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [datasource, branch, domain, key, lines]);
  // Only the types drawn in the panel now; all of them before the first run.
  const legend = (rel?.key === key ? rel.types : []).filter((t) => !shownTypes || shownTypes.has(t.uri));

  return (
    <Stack direction="column" gap={0.5}>
      <Stack wrap="wrap" gap={1}>
        <InlineField
          label="Colour"
          labelWidth={LABEL_WIDTH}
          tooltip="Node rings. Data quality: green Good, yellow Uncertain, red Bad, grey no data, blue no live values."
        >
          <RadioButtonGroup
            options={colorModes}
            value={query.colorBy ?? 'quality'}
            onChange={(v) => onChange({ colorBy: v })}
          />
        </InlineField>
        <InlineField
          label="Lines"
          tooltip="Containment lines are always solid and thin; other relationships dashed and thicker."
        >
          <RadioButtonGroup options={lineModes} value={lines} onChange={(v) => onChange({ lineColorBy: v })} />
        </InlineField>
        <InlineField
          label="Relationship names"
          tooltip="Show each relationship as a small labelled node, so its name is always visible. Without this the panel shows names only on hover."
        >
          <InlineSwitch
            value={!!query.relationshipNodes}
            onChange={(e) => onChange({ relationshipNodes: e.currentTarget.checked })}
          />
        </InlineField>
      </Stack>
      {lines === 'kind' ? (
        <LineKey
          items={[
            { name: 'Containment', color: '#6E6E6E', dashed: false },
            { name: 'Other relationship', color: '#FF9830', dashed: true },
          ]}
        />
      ) : (
        <LineKey items={legend.map((t) => ({ key: t.uri, name: t.name, color: t.color, dashed: !t.isContainment }))} />
      )}
    </Stack>
  );
}

/** A small key for line colours; the Node Graph legend only covers node rings. */
function LineKey({ items }: { items: Array<{ key?: string; name: string; color: string; dashed: boolean }> }) {
  if (items.length === 0) {
    return null;
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 14px', fontSize: 12, opacity: 0.85, paddingLeft: 4 }}>
      {items.map((it) => (
        <span key={it.key ?? it.name} style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <svg width="26" height="8" aria-hidden>
            <line
              x1="1"
              y1="4"
              x2="25"
              y2="4"
              stroke={it.color}
              strokeWidth={it.dashed ? 2 : 1.5}
              strokeDasharray={it.dashed ? '6 3' : undefined}
            />
          </svg>
          {it.name}
        </span>
      ))}
    </div>
  );
}

/** Relationship type URIs in this query's edges frame, or undefined before a result exists. */
function relationshipTypesIn(series: DataFrame[] | undefined, refId: string): Set<string> | undefined {
  const edges = series?.find((f) => f.name === 'edges' && (f.refId === undefined || f.refId === refId));
  const field = edges?.fields.find((f) => f.name === 'detail__type');
  if (!field) {
    return undefined;
  }
  return new Set(Array.from(field.values as ArrayLike<string>));
}

const aggregations: Array<SelectableValue<Aggregation>> = [
  {
    label: 'Auto',
    value: 'auto',
    description: 'One line that smooths noise but keeps stops and spikes at their true values',
  },
  { label: 'Raw', value: 'raw', description: 'Every stored value (capped at 200,000 per property)' },
  { label: 'Minute', value: 'minute', description: 'Average per minute' },
  { label: 'Hour', value: 'hour', description: 'Average per hour' },
  { label: 'Day', value: 'day', description: 'Average per day' },
];

function HistoryOptions({ query, onChange }: { query: AtlasQuery; onChange: (p: Partial<AtlasQuery>) => void }) {
  const agg = query.aggregation ?? 'auto';
  return (
    <Stack wrap="wrap" gap={1}>
      <InlineField
        label="Aggregation"
        labelWidth={LABEL_WIDTH}
        tooltip="Text and boolean properties are always read raw."
      >
        <RadioButtonGroup options={aggregations} value={agg} onChange={(v) => onChange({ aggregation: v })} />
      </InlineField>
      {(agg === 'minute' || agg === 'hour' || agg === 'day') && (
        <InlineField label="Min/max" tooltip="Add min and max series next to the bucket average">
          <InlineSwitch
            value={!!query.includeMinMax}
            onChange={(e) => onChange({ includeMinMax: e.currentTarget.checked })}
          />
        </InlineField>
      )}
      {agg !== 'minute' && agg !== 'hour' && agg !== 'day' && (
        <InlineField label="Quality" tooltip="Add the OPC quality code of each raw value as a series">
          <InlineSwitch
            value={!!query.includeQuality}
            onChange={(e) => onChange({ includeQuality: e.currentTarget.checked })}
          />
        </InlineField>
      )}
    </Stack>
  );
}

/**
 * Queries saved by older versions, shown (and saved on the next change) in
 * the current shape: "instances" becomes values of a type, Values of one
 * instance and History of one instance's properties become a selection.
 * Nested instances or a whole type cannot be listed without reading Atlas,
 * so those are left as they are (and still run).
 */
export function upgrade(q: AtlasQuery): AtlasQuery {
  if (q.queryType === 'instances') {
    return { ...q, queryType: 'values', scope: 'type', layout: q.layout ?? 'columns' };
  }
  if (q.queryType === 'values' && !q.selection?.length) {
    const scope = q.scope ?? 'instance';
    if (scope === 'type' && q.entityType) {
      return { ...q, selection: [{ instance: '', type: q.entityType, scope: 'type' }], layout: q.layout ?? 'columns' };
    }
    if (q.instanceId) {
      const pick: HistoryPick =
        scope === 'children' ? { instance: q.instanceId, scope: 'nested' } : { instance: q.instanceId };
      return { ...q, selection: [pick], layout: q.layout ?? 'rows' };
    }
  }
  if (q.queryType === 'history' && !q.selection?.length && q.properties?.length) {
    const scope = q.scope ?? 'instance';
    if (scope === 'type' && q.entityType) {
      const type = q.entityType;
      return {
        ...q,
        selection: q.properties.map((property) => ({ instance: '', type, property, scope: 'type' as const })),
        properties: undefined,
      };
    }
    if (scope === 'children' && q.instanceId) {
      const instance = q.instanceId;
      return {
        ...q,
        selection: q.properties.map((property) => ({ instance, property, scope: 'nested' as const })),
        properties: undefined,
      };
    }
  }
  if (
    q.queryType === 'history' &&
    !q.selection?.length &&
    q.properties?.length &&
    (q.scope ?? 'instance') === 'instance' &&
    q.instanceId
  ) {
    const instance = q.instanceId;
    return { ...q, selection: q.properties.map((property) => ({ instance, property })), properties: undefined };
  }
  return q;
}

function LimitField({
  query,
  onChange,
  placeholder,
  tooltip,
}: {
  query: AtlasQuery;
  onChange: (p: Partial<AtlasQuery>) => void;
  placeholder: string;
  tooltip: string;
}) {
  return (
    <InlineField label="Limit" tooltip={tooltip}>
      <Input
        type="number"
        width={10}
        min={1}
        placeholder={placeholder}
        defaultValue={query.limit || ''}
        onBlur={(e) => {
          const n = parseInt(e.currentTarget.value, 10);
          const limit = Number.isFinite(n) && n > 0 ? n : undefined;
          if (limit !== query.limit) {
            onChange({ limit });
          }
        }}
      />
    </InlineField>
  );
}

function noCypherReason(queryType: string): string {
  switch (queryType) {
    case 'graph':
      return 'Atlas returns internal ids for multi-hop paths, so depth 2 to 4 has no single Cypher query; use depth 1';
    default:
      return 'Select instances or properties first ($variables cannot be written into Cypher)';
  }
}

function unique(list: string[]): string[] {
  return Array.from(new Set(list));
}

function messageOf(e: unknown): string {
  const err = e as { data?: { message?: string }; message?: string };
  return err?.data?.message ?? err?.message ?? String(e);
}

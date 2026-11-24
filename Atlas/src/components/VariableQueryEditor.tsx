import React, { useEffect, useState } from 'react';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { Combobox, InlineField, RadioButtonGroup, Stack } from '@grafana/ui';

import { DataSource } from '../datasource';
import { withoutProperties } from '../tree';
import { AtlasOptions, AtlasQuery, VariableReturns, World } from '../types';
import { EnumField } from './EnumField';
import { HistoryField } from './HistoryField';
import { RelationshipField } from './RelationshipField';
import { QueryEditor } from './QueryEditor';

type Props = QueryEditorProps<DataSource, AtlasQuery, AtlasOptions, AtlasQuery>;

type Source = 'selection' | 'query';

const sources: Array<SelectableValue<Source>> = [
  { label: 'Selection', value: 'selection', description: 'Pick instances in the Atlas tree, as in the Data tab' },
  {
    label: 'Query',
    value: 'query',
    description: 'Any Graph, Data or Cypher query; its first column becomes the values',
  },
];

const returnsOptions: Array<SelectableValue<VariableReturns>> = [
  { label: 'Instances', value: 'instances', description: 'The instance ids, shown with their names' },
  { label: 'Properties', value: 'properties', description: 'The names of the live properties picked' },
  {
    label: 'Enumeration members',
    value: 'enum',
    description: 'The members of an Atlas enumeration: labels shown, keys as values (for Cypher filters)',
  },
  {
    label: 'Relationship types',
    value: 'relationships',
    description: 'Relationship types for a Graph query: names shown, type URIs as values',
  },
];

const LABEL_WIDTH = 14;

/**
 * Variable editor. Selection variables use the Data tab's Select modal: "every
 * CNCMachine" gives CNC01..CNC05 and follows machines added later; nested
 * picks give everything under an instance. Query keeps the full query editor
 * for Cypher and for variables saved before Selection existed.
 */
export function VariableQueryEditor(props: Props) {
  const { query, onChange, datasource } = props;
  const source: Source = query.queryType === 'variable' || !query.queryType ? 'selection' : 'query';
  const q: AtlasQuery = { ...query, refId: query.refId || 'variable' };

  return (
    <Stack direction="column" gap={1}>
      <InlineField label="From" labelWidth={LABEL_WIDTH}>
        <RadioButtonGroup
          options={sources}
          value={source}
          onChange={(v) =>
            onChange(
              v === 'selection' ? { ...q, queryType: 'variable' } : { ...q, queryType: 'cypher', format: 'table' }
            )
          }
        />
      </InlineField>
      {source === 'selection' ? (
        <SelectionVariable query={q} onChange={onChange} datasource={datasource} />
      ) : (
        <QueryEditor {...props} query={q} onRunQuery={props.onRunQuery ?? (() => {})} />
      )}
    </Stack>
  );
}

function SelectionVariable({
  query,
  onChange,
  datasource,
}: {
  query: AtlasQuery;
  onChange: (q: AtlasQuery) => void;
  datasource: DataSource;
}) {
  const branch = query.branch || datasource.defaultBranch;
  const domain = query.domain || datasource.defaultDomain;
  const returns = query.returns ?? 'instances';
  const [worlds, setWorlds] = useState<World[]>([]);

  useEffect(() => {
    datasource
      .getWorlds()
      .then(setWorlds)
      .catch(() => setWorlds([]));
  }, [datasource]);

  // Grafana's "Run query" refreshes its preview of values.
  const update = (patch: Partial<AtlasQuery>) => onChange({ ...query, queryType: 'variable', ...patch });
  const branchOptions = Array.from(new Set(worlds.map((w) => w.branch))).map((b) => ({ label: b, value: b }));
  const domainOptions = worlds.filter((w) => w.branch === branch).map((w) => ({ label: w.domain, value: w.domain }));

  return (
    <Stack direction="column" gap={1}>
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
      <InlineField
        label="Values"
        labelWidth={LABEL_WIDTH}
        tooltip="Instances: use as $name in a Data selection, e.g. $machine with + nested. Properties: use after the slash, e.g. CNC01-Production/$prop."
      >
        <RadioButtonGroup
          options={returnsOptions}
          value={returns}
          onChange={(v) =>
            update(
              v === 'instances' && query.selection
                ? { returns: v, selection: withoutProperties(query.selection) }
                : { returns: v }
            )
          }
        />
      </InlineField>
      {returns === 'enum' ? (
        <InlineField
          label="Enumeration"
          labelWidth={LABEL_WIDTH}
          tooltip="Use the variable in Cypher, e.g. AND v.Value IN $mode. Number and boolean keys are sent as numbers and booleans, so they match the stored values."
        >
          <EnumField
            datasource={datasource}
            branch={branch}
            domain={domain}
            value={query.enumType}
            onChange={(e) => update({ enumType: e?.name ?? '', enumKeyKind: e?.keyKind })}
          />
        </InlineField>
      ) : returns === 'relationships' ? (
        <InlineField
          label="Offer"
          labelWidth={LABEL_WIDTH}
          tooltip="The relationship types in the dropdown. Empty offers every type of the domain. Use the variable in a Graph query's Relationships."
        >
          <RelationshipField
            datasource={datasource}
            branch={branch}
            domain={domain}
            value={query.relationships ?? []}
            placeholder="every relationship type"
            onChange={(relationships) => update({ relationships })}
          />
        </InlineField>
      ) : (
        <InlineField label="Selection" labelWidth={LABEL_WIDTH} grow>
          <HistoryField
            datasource={datasource}
            branch={branch}
            domain={domain}
            value={returns === 'instances' ? withoutProperties(query.selection ?? []) : (query.selection ?? [])}
            onChange={(selection) => update({ selection })}
            instancesOnly={returns === 'instances'}
            defaultView="types"
          />
        </InlineField>
      )}
    </Stack>
  );
}

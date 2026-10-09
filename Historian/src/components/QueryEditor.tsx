import React, { useEffect, useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2, QueryEditorProps } from '@grafana/data';
import { getTemplateSrv } from '@grafana/runtime';
import { Button, ComboboxOption, InlineField, InlineSwitch, MultiCombobox, Stack, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { I3xOptions, I3xQuery, ObjectOption } from '../types';
import { TagBrowser } from './TagBrowser';

type Props = QueryEditorProps<DataSource, I3xQuery, I3xOptions>;

export function QueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const [objects, setObjects] = useState<ObjectOption[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string>();
  const [browsing, setBrowsing] = useState(false);
  const styles = useStyles2(getStyles);

  useEffect(() => {
    let cancelled = false;
    datasource
      .getObjects()
      .then((list) => !cancelled && setObjects(list))
      .catch((err) => !cancelled && setLoadError(err?.data?.message ?? err?.message ?? 'Could not load tags'))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [datasource]);

  const elementIds = useMemo(() => query.elementIds ?? [], [query.elementIds]);

  const options = useMemo<Array<ComboboxOption<string>>>(() => {
    const variables = getTemplateSrv()
      .getVariables()
      .map((v) => ({ label: `$${v.name}`, value: `$${v.name}`, description: 'Dashboard variable' }));
    const tags = objects
      .filter((o) => !o.isFolder)
      .map((o) => ({
        label: o.displayName,
        value: o.elementId,
        // Native tags carry a description and unit; i3X ones only their id.
        description: [o.description, o.unit && `(${o.unit})`].filter(Boolean).join(' ') || o.elementId,
      }));
    // Keep ids already in the query selectable even if the server no longer lists them.
    const known = new Set([...variables, ...tags].map((o) => o.value));
    const extra = elementIds.filter((id) => !known.has(id)).map((id) => ({ label: id, value: id }));
    return [...variables, ...tags, ...extra];
  }, [objects, elementIds]);

  const update = (patch: Partial<I3xQuery>) => {
    onChange({ ...query, queryType: 'history', ...patch });
    onRunQuery();
  };

  return (
    <Stack direction="column" gap={0.5}>
      <InlineField
        label="Tags"
        labelWidth={12}
        grow
        invalid={!!loadError}
        error={loadError}
        htmlFor="query-editor-tags"
        tooltip="Open the tree browser to pick tags by folder, or search and type in the box: an ElementId (dataset:tag.path) or a $variable, then Enter."
      >
        {/* The browser button leads the row so it is never pushed off to the far edge of a wide panel. */}
        <div className={styles.tagsRow}>
          <Button
            variant="secondary"
            icon="folder-open"
            className={styles.browseButton}
            disabled={loading || !!loadError}
            onClick={() => setBrowsing(true)}
          >
            Tree browser
          </Button>
          <div className={styles.tagsInput}>
            <MultiCombobox
              id="query-editor-tags"
              options={options}
              value={elementIds}
              loading={loading}
              createCustomValue
              customValueDescription="Use this ElementId"
              isClearable
              placeholder={loading ? 'Loading tags…' : 'Search or type tags'}
              onChange={(selected) => update({ elementIds: selected.map((s) => s.value).filter(Boolean) })}
            />
          </div>
        </div>
      </InlineField>
      {browsing && (
        // Mounted only while open, so it starts from the query's current tags each time.
        <TagBrowser
          isOpen
          objects={objects}
          selected={elementIds}
          onDismiss={() => setBrowsing(false)}
          onApply={(ids) => {
            setBrowsing(false);
            update({ elementIds: ids });
          }}
        />
      )}
      <InlineField
        label="Options"
        labelWidth={12}
        tooltip="Live: stream new values into the panel as they arrive, after loading the panel's time range; alert rules always read history. Quality field: add each sample's quality (Good, Bad, Uncertain) as an extra field; not available while live, and leave off for alert rules. Bad quality as gaps: blank out samples whose quality is not Good, so stats and alerts ignore them."
      >
        <Stack gap={1}>
          <InlineSwitch
            id="query-editor-live"
            label="Live"
            showLabel
            value={!!query.live}
            onChange={(e) => update({ live: e.currentTarget.checked })}
          />
          <InlineSwitch
            id="query-editor-include-quality"
            label="Quality field"
            showLabel
            disabled={!!query.live}
            value={!!query.includeQuality && !query.live}
            onChange={(e) => update({ includeQuality: e.currentTarget.checked })}
          />
          <InlineSwitch
            id="query-editor-bad-as-null"
            label="Bad quality as gaps"
            showLabel
            value={!!query.badQualityAsNull}
            onChange={(e) => update({ badQualityAsNull: e.currentTarget.checked })}
          />
        </Stack>
      </InlineField>
    </Stack>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  tagsRow: css({
    display: 'flex',
    alignItems: 'flex-start',
    gap: theme.spacing(1),
    width: '100%',
  }),
  // Same height as the tag box beside it; flexShrink keeps the label whole on narrow panels.
  browseButton: css({ flexShrink: 0, height: theme.spacing(4) }),
  tagsInput: css({ flex: 1, minWidth: 0 }),
});

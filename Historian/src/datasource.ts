import {
  CoreApp,
  DataQueryRequest,
  DataQueryResponse,
  DataSourceInstanceSettings,
  LiveChannelScope,
  ScopedVars,
} from '@grafana/data';
import { DataSourceWithBackend, getGrafanaLiveSrv, getTemplateSrv } from '@grafana/runtime';
import { merge, Observable } from 'rxjs';

import { liveChannelPath } from './live';
import { DEFAULT_QUERY, I3xOptions, I3xQuery, ObjectOption } from './types';

// Joins the values of a multi-value variable so they can be split apart
// again. ElementIds can contain commas and spaces, so neither is safe.
const SEPARATOR = '\u001f';

// Upper bound on rows kept per live series, whatever the time range.
const LIVE_MAX_ROWS = 20000;

export class DataSource extends DataSourceWithBackend<I3xQuery, I3xOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<I3xOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<I3xQuery> {
    return DEFAULT_QUERY;
  }

  /**
   * Live queries stream through Grafana Live, one channel per tag; the rest
   * go through the backend as normal history queries. Alert rules never come
   * through here, so they always use history.
   */
  query(request: DataQueryRequest<I3xQuery>): Observable<DataQueryResponse> {
    const live = request.targets.filter((t) => t.live && this.filterQuery(t));
    if (live.length === 0) {
      return super.query(request);
    }

    const from = request.range.from.valueOf();
    const to = request.range.to.valueOf();
    const streams: Array<Observable<DataQueryResponse>> = live.flatMap((target) => {
      const q = this.applyTemplateVariables(target, request.scopedVars);
      return (q.elementIds ?? []).map((elementId) =>
        getGrafanaLiveSrv().getDataStream({
          key: `${request.requestId}-${target.refId}-${elementId}`,
          addr: {
            scope: LiveChannelScope.DataSource,
            // Grafana 13 reads "stream"; 12.x reads "namespace".
            stream: this.uid,
            namespace: this.uid,
            path: liveChannelPath(elementId, q.badQualityAsNull),
            // History for the panel's range is sent first, then live samples.
            data: { from, to },
          },
          // Keep the panel's time window: the line scrolls instead of growing.
          buffer: { maxDelta: to - from, maxLength: LIVE_MAX_ROWS },
        })
      );
    });

    const rest = request.targets.filter((t) => !live.includes(t));
    if (rest.length > 0) {
      streams.push(super.query({ ...request, targets: rest }));
    }
    return merge(...streams);
  }

  /** Expands $variables; a multi-value variable becomes one ElementId per value. */
  applyTemplateVariables(query: I3xQuery, scopedVars: ScopedVars): I3xQuery {
    const srv = getTemplateSrv();
    const elementIds = (query.elementIds ?? []).flatMap((id) =>
      srv
        .replace(id, scopedVars, (value: string | string[]) => (Array.isArray(value) ? value.join(SEPARATOR) : value))
        .split(SEPARATOR)
    );
    return { ...query, elementIds };
  }

  filterQuery(query: I3xQuery): boolean {
    return !query.hide && (query.elementIds?.length ?? 0) > 0;
  }

  /** Tags known to the historian, for the query editor's picker. */
  getObjects(): Promise<ObjectOption[]> {
    return this.getResource<ObjectOption[]>('objects');
  }
}

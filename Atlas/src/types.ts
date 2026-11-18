import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

/**
 * graph: Node Graph of the whole domain or around one instance.
 * values: current values of one instance, an instance and what it contains, or every instance of a type.
 * cypher: a read-only Cypher query written by the user.
 * instances: saved by older versions; read as values with scope "type".
 */
export type AtlasQueryType = 'graph' | 'values' | 'history' | 'cypher' | 'instances';

/** History: raw points, or minute / hour / day buckets. Auto picks from the panel's time range. */
export type Aggregation = 'auto' | 'raw' | 'minute' | 'hour' | 'day';

/** Values: which instances to read. */
export type ValuesScope = 'instance' | 'children' | 'type';

/** Values: rows = one row per property; columns = one row per instance. */
export type ValuesLayout = 'rows' | 'columns';

/** Graph: quality = ring by the quality of live values; type = one colour per entity type. */
export type ColorBy = 'quality' | 'type' | 'none';

export type LineColorBy = 'kind' | 'type';

/** GET resources/relationship-types: each type with its line colour. */
export interface RelationshipColor {
  uri: string;
  name: string;
  isContainment: boolean;
  color: string;
}

export type CypherFormat = 'auto' | 'table' | 'graph' | 'timeseries';

export interface AtlasQuery extends DataQuery {
  queryType?: AtlasQueryType;
  /** Empty means the data source's default. May contain $variables. */
  branch?: string;
  domain?: string;
  /** Graph (optional start point) and values. May contain $variables. */
  instanceId?: string;
  /** Values with scope "type": short type name, e.g. CNCProduction. */
  entityType?: string;
  scope?: ValuesScope;
  layout?: ValuesLayout;
  /**
   * History: the series to read, picked in the browse modal. A pick without a
   * property reads every live property of the instance. Instance ids may be
   * $variables; a multi-value variable gives one pick per value.
   */
  selection?: HistoryPick[];
  /** History saved before selection existed: live properties of instanceId / entityType. */
  properties?: string[];
  aggregation?: Aggregation;
  /** History buckets: add min and max next to the average. */
  includeMinMax?: boolean;
  /** Graph and Cypher node graphs. */
  colorBy?: ColorBy;
  /** Draw relationships as small labelled nodes so their names are always visible. */
  relationshipNodes?: boolean;
  /** Line colours: kind = grey containment / orange other; type = one colour per relationship type. */
  lineColorBy?: LineColorBy;
  /** Graph around an instance: hops to follow, 1 to 4. */
  depth?: number;
  /** Graph: relationships; instances: rows. 0 means the plugin default. */
  limit?: number;
  /** Instances table: add a quality and a time column per live value. */
  includeQuality?: boolean;
  includeTime?: boolean;
  /** Cypher mode. $name refers to a Cypher parameter; Grafana variables are passed as parameters. */
  cypher?: string;
  /** Filled in at query time from Grafana variables. Not edited by the user. */
  parameters?: Record<string, unknown>;
  format?: CypherFormat;
}

/** One History pick: a property of an instance, or the whole instance. */
export interface HistoryPick {
  instance: string;
  property?: string;
  /** Entity type when picked from the tree; used for "Edit as Cypher" only. */
  type?: string;
}

export const DEFAULT_QUERY: Partial<AtlasQuery> = {
  queryType: 'graph',
  depth: 1,
};

export type AuthType = 'clientCredentials' | 'bearerToken' | 'none';

/** Settings stored in jsonData. The Atlas base URL is the data source's top-level url. */
export interface AtlasOptions extends DataSourceJsonData {
  authType?: AuthType;
  tokenUrl?: string;
  clientId?: string;
  scope?: string;
  defaultBranch?: string;
  defaultDomain?: string;
  /** Standard Grafana TLS keys, read by the backend's HTTP client. */
  tlsSkipVerify?: boolean;
  tlsAuthWithCACert?: boolean;
}

/** Secrets: write-only from the browser, decrypted only in the backend. */
export interface AtlasSecureJsonData {
  clientSecret?: string;
  bearerToken?: string;
  tlsCACert?: string;
}

/** GET resources/worlds */
export interface World {
  branch: string;
  domain: string;
  instanceCount?: number;
  isStale?: boolean;
}

/** GET resources/types */
export interface TypeOption {
  uri: string;
  name: string;
  count: number;
}

/** GET resources/instances */
export interface InstanceOption {
  id: string;
  type: string;
}

/** GET resources/tree: one instance, nested by containment as in Atlas. */
export interface TreeNodeData {
  id: string;
  label?: string;
  type: string;
  parentId?: string;
  /** Live properties (with stored history) and their value kind. */
  live?: LiveProperty[];
}

export interface LiveProperty {
  name: string;
  /** record: a compound with its own timestamp (e.g. MaintainX LiveAsset), read as one column per part. */
  kind: 'number' | 'text' | 'boolean' | 'record' | 'unknown';
}

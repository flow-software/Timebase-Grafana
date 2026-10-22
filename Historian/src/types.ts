import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type I3xQueryType = 'history';

export interface I3xQuery extends DataQuery {
  queryType?: I3xQueryType;
  /** i3X ElementIds, e.g. "default:Boilers.Boiler #1.BoilerStatus.Pressure". May contain $variables. */
  elementIds?: string[];
  /** Add a Quality field next to each value. */
  includeQuality?: boolean;
  /** Blank out values whose quality is not Good. */
  badQualityAsNull?: boolean;
  /** Stream new values into the panel as they arrive (Grafana Live). */
  live?: boolean;
}

export const DEFAULT_QUERY: Partial<I3xQuery> = {
  queryType: 'history',
  elementIds: [],
};

export type AuthType = 'clientCredentials' | 'bearerToken' | 'none';

/** Which historian API a datasource talks to. Unset means the native Timebase API. */
export type ApiType = 'timebase' | 'i3x';

/** Settings stored in jsonData. The API base URL is the datasource's top-level url. */
export interface I3xOptions extends DataSourceJsonData {
  apiType?: ApiType;
  authType?: AuthType;
  tokenUrl?: string;
  clientId?: string;
  scope?: string;
  /** Standard Grafana TLS keys, read by the backend's HTTP client. */
  tlsSkipVerify?: boolean;
  tlsAuthWithCACert?: boolean;
}

/** Secrets: write-only from the browser, decrypted only in the backend. */
export interface I3xSecureJsonData {
  clientSecret?: string;
  bearerToken?: string;
  tlsCACert?: string;
}

/** One entry from the backend's GET resources/objects. */
export interface ObjectOption {
  elementId: string;
  displayName: string;
  type: string;
  parentId?: string;
  /** Datasets and folders: they hold tags but have no values to query. */
  isFolder: boolean;
  /** The historian's own health tags; reported by the native API only. */
  isSystem?: boolean;
  /** Native API only. */
  description?: string;
  /** Engineering unit; native API only. */
  unit?: string;
  /** Tag metadata such as Site or Area; native API only. */
  fields?: Record<string, string>;
}

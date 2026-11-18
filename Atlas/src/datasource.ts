import { CoreApp, DataSourceInstanceSettings, DataSourceVariableSupport, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv, TemplateSrv } from '@grafana/runtime';

import { cypherParameterNames } from './cypher';
import {
  AtlasOptions,
  AtlasQuery,
  DEFAULT_QUERY,
  InstanceOption,
  PropertyOption,
  RelationshipColor,
  TreeNodeData,
  TypeOption,
  World,
} from './types';

/** Dashboard variables use the normal query editor; the first column becomes the values. */
class AtlasVariableSupport extends DataSourceVariableSupport<DataSource> {}

export class DataSource extends DataSourceWithBackend<AtlasQuery, AtlasOptions> {
  readonly defaultBranch: string;
  readonly defaultDomain: string;

  constructor(
    instanceSettings: DataSourceInstanceSettings<AtlasOptions>,
    private readonly templateSrv: TemplateSrv = getTemplateSrv()
  ) {
    super(instanceSettings);
    this.defaultBranch = instanceSettings.jsonData.defaultBranch || 'main';
    this.defaultDomain = instanceSettings.jsonData.defaultDomain || '';
    this.variables = new AtlasVariableSupport();
  }

  getDefaultQuery(_: CoreApp): Partial<AtlasQuery> {
    return DEFAULT_QUERY;
  }

  /**
   * Expands $variables in the picker fields. In Cypher text they are left
   * alone and sent as Cypher parameters instead, so a variable's value can
   * never change the query itself. A multi-value variable becomes a list,
   * for use with IN.
   */
  applyTemplateVariables(query: AtlasQuery, scopedVars: ScopedVars): AtlasQuery {
    const srv = this.templateSrv;
    const r = (s?: string) => (s ? srv.replace(s, scopedVars) : s);
    const out: AtlasQuery = {
      ...query,
      branch: r(query.branch),
      domain: r(query.domain),
      instanceId: r(query.instanceId),
      entityType: r(query.entityType),
    };

    if (query.queryType === 'cypher' && query.cypher) {
      const params: Record<string, unknown> = {};
      for (const name of cypherParameterNames(query.cypher)) {
        if (!srv.containsTemplate('$' + name)) {
          continue;
        }
        let value: unknown;
        srv.replace('$' + name, scopedVars, (v: string | string[]) => {
          value = v;
          return '';
        });
        params[name] = value;
      }
      out.parameters = params;
    }
    return out;
  }

  filterQuery(query: AtlasQuery): boolean {
    if (query.hide) {
      return false;
    }
    switch (query.queryType ?? 'graph') {
      case 'instances':
        return !!query.entityType;
      case 'values':
        return query.scope === 'type' ? !!query.entityType : !!query.instanceId;
      case 'history':
        return (
          (query.properties?.length ?? 0) > 0 && (query.scope === 'type' ? !!query.entityType : !!query.instanceId)
        );
      case 'cypher':
        return !!query.cypher?.trim();
      default:
        return true;
    }
  }

  getWorlds(): Promise<World[]> {
    return this.getResource<World[]>('worlds');
  }

  getTypes(branch: string, domain: string): Promise<TypeOption[]> {
    return this.getResource<TypeOption[]>('types', { branch: this.expand(branch), domain: this.expand(domain) });
  }

  getInstances(branch: string, domain: string, type?: string): Promise<InstanceOption[]> {
    return this.getResource<InstanceOption[]>('instances', {
      branch: this.expand(branch),
      domain: this.expand(domain),
      type: type ?? '',
    });
  }

  /** Every instance of a world with its containment parent, for the browse tree. Cached for a minute in the backend. */
  getTree(branch: string, domain: string, refresh = false): Promise<TreeNodeData[]> {
    return this.getResource<TreeNodeData[]>('tree', {
      branch: this.expand(branch),
      domain: this.expand(domain),
      ...(refresh ? { refresh: '1' } : {}),
    });
  }

  /** Relationship types with the colour their lines get when lines are coloured by type. */
  getRelationshipColors(branch: string, domain: string): Promise<RelationshipColor[]> {
    return this.getResource<RelationshipColor[]>('relationship-types', {
      branch: this.expand(branch),
      domain: this.expand(domain),
    });
  }

  /** Live properties of an instance, nested instances or a type, for History. */
  getProperties(
    branch: string,
    domain: string,
    scope: string,
    instance?: string,
    type?: string
  ): Promise<PropertyOption[]> {
    return this.getResource<PropertyOption[]>('properties', {
      branch: this.expand(branch),
      domain: this.expand(domain),
      scope,
      instance: this.expand(instance ?? ''),
      type: this.expand(type ?? ''),
    });
  }

  private expand(s: string): string {
    return s ? this.templateSrv.replace(s) : s;
  }
}

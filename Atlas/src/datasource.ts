import { CoreApp, DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv, TemplateSrv } from '@grafana/runtime';

import { CypherVars, cypherParameterNames } from './cypher';
import { AtlasVariableSupport } from './variables';
import {
  AtlasOptions,
  AtlasQuery,
  DEFAULT_QUERY,
  HistoryPick,
  InstanceOption,
  SelectionVariables,
  RelationshipColor,
  TreeNodeData,
  TypeOption,
  World,
} from './types';

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
    this.variables = new AtlasVariableSupport(this);
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

    if (query.selection?.length) {
      out.selection = query.selection.flatMap((p) => this.expandPick(p, scopedVars));
    }

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
        return (query.selection?.length ?? 0) > 0 || (query.scope === 'type' ? !!query.entityType : !!query.instanceId);
      case 'history':
        return (
          (query.selection?.length ?? 0) > 0 ||
          ((query.properties?.length ?? 0) > 0 && (query.scope === 'type' ? !!query.entityType : !!query.instanceId))
        );
      case 'cypher':
        return !!query.cypher?.trim();
      case 'variable':
        return false; // runs in the browser, through the variable support
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

  /**
   * Expands the variables in a pick's instance and property. When the first
   * variable in either has several values, the pick becomes one pick per
   * value, with the text around it kept: "$machine-Production" with CNC01 and
   * CNC02 gives CNC01-Production and CNC02-Production; "/$prop" with FeedRate
   * and RPM gives one pick per property.
   */
  private expandPick(p: HistoryPick, scopedVars: ScopedVars): HistoryPick[] {
    const type = p.type && this.templateSrv.replace(p.type, scopedVars);
    return this.expandText(p.instance, scopedVars).flatMap((instance) =>
      p.property === undefined
        ? [{ ...p, instance, type }]
        : this.expandText(p.property, scopedVars).map((property) => ({ ...p, instance, property, type }))
    );
  }

  /**
   * The dashboard variables that can stand in a selection: those whose
   * current values are instance ids in the tree, and those whose values are
   * all property names. Empty outside a dashboard (Explore).
   */
  selectionVariables(nodes: TreeNodeData[]): SelectionVariables {
    const ids = new Set(nodes.map((n) => n.id));
    const props = new Set(nodes.flatMap((n) => (n.live ?? []).map((l) => l.name)));
    const out: SelectionVariables = { instances: [], properties: [] };
    for (const v of this.templateSrv.getVariables()) {
      const values = this.expandText('$' + v.name, {}).filter((x) => x && !x.startsWith('$'));
      if (values.some((x) => ids.has(x))) {
        out.instances.push({ name: v.name, values: values.filter((x) => ids.has(x)) });
      } else if (values.length > 0 && values.every((x) => props.has(x))) {
        out.properties.push({ name: v.name, values });
      }
    }
    return out;
  }

  /** Whether the current dashboard has variables (none in Explore). */
  hasDashboardVariables(): boolean {
    return this.templateSrv.getVariables().length > 0;
  }

  /** The picks with their variables replaced by the current values, as the query will run them. */
  resolvePicks(picks: HistoryPick[]): HistoryPick[] {
    return picks.flatMap((p) => this.expandPick(p, {}));
  }

  /** Dashboard variables for "Edit as Cypher": their current values and whether they hold several. */
  cypherVars(): CypherVars {
    return {
      values: (text) => this.expandText(text, {}),
      isMulti: (name) => {
        const v = this.templateSrv.getVariables().find((x) => x.name === name) as
          { multi?: boolean; includeAll?: boolean } | undefined;
        return !!(v?.multi || v?.includeAll);
      },
    };
  }

  /** The text with its variables replaced; one result per value of the first multi-value variable. */
  private expandText(text: string, scopedVars: ScopedVars): string[] {
    const srv = this.templateSrv;
    const m = /^(.*?)(\$\{[^}]+\}|\$[A-Za-z0-9_]+|\[\[[A-Za-z0-9_]+\]\])(.*)$/.exec(text);
    if (!m || !srv.containsTemplate(m[2])) {
      return [srv.replace(text, scopedVars)];
    }
    const [, before, token, after] = m;
    let values: string[] = [];
    srv.replace(token, scopedVars, (v: string | string[]) => {
      values = Array.isArray(v) ? v : [v];
      return '';
    });
    const head = srv.replace(before, scopedVars);
    const tail = srv.replace(after, scopedVars);
    return values.map((v) => head + v + tail);
  }

  private expand(s: string): string {
    return s ? this.templateSrv.replace(s) : s;
  }
}

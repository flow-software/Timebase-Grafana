import { createDataFrame, CustomVariableSupport, DataQueryRequest, DataQueryResponse, FieldType } from '@grafana/data';
import { from, Observable } from 'rxjs';

import type { DataSource } from './datasource';
import { VariableQueryEditor } from './components/VariableQueryEditor';
import { enumOptions, relationshipOptions } from './enums';
import { variableValues } from './tree';
import { AtlasQuery } from './types';

/**
 * Dashboard variables. "Selection" variables are picked in the same modal as
 * the Data tab and give the instance ids (or property names) of the
 * selection, resolved against the tree when the dashboard loads, so "every
 * CNCMachine" follows new machines. Any other query (Cypher, older saved
 * variables) runs as a panel query and its first column becomes the values.
 */
export class AtlasVariableSupport extends CustomVariableSupport<DataSource, AtlasQuery> {
  editor = VariableQueryEditor;

  constructor(private readonly datasource: DataSource) {
    super();
  }

  query(request: DataQueryRequest<AtlasQuery>): Observable<DataQueryResponse> {
    const q = request.targets[0];
    if (q?.queryType && q.queryType !== 'variable') {
      return this.datasource.query(request);
    }
    return from(this.selectionValues(q, request));
  }

  private async selectionValues(q: AtlasQuery, request: DataQueryRequest<AtlasQuery>): Promise<DataQueryResponse> {
    const resolved = this.datasource.applyTemplateVariables(q, request.scopedVars);
    const branch = resolved.branch || this.datasource.defaultBranch;
    const domain = resolved.domain || this.datasource.defaultDomain;
    const { text, value } =
      resolved.returns === 'enum'
        ? enumOptions((await this.datasource.getEnums(branch, domain)).find((e) => e.name === resolved.enumType))
        : resolved.returns === 'relationships'
          ? relationshipOptions(await this.datasource.getRelationshipColors(branch, domain), q.relationships)
          : variableValues(resolved, await this.datasource.getTree(branch, domain));
    return {
      data: [
        createDataFrame({
          refId: q.refId,
          fields: [
            { name: 'text', type: FieldType.string, values: text },
            { name: 'value', type: FieldType.string, values: value },
          ],
        }),
      ],
    };
  }
}

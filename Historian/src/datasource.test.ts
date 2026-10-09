import { DataSourceInstanceSettings } from '@grafana/data';

import { DataSource } from './datasource';
import { I3xOptions } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({
    replace: (target: string, _vars: unknown, format?: (v: string | string[]) => string) => {
      const vars: Record<string, string | string[]> = {
        // Names with commas and spaces, which a csv format would split wrongly.
        $tags: ['default:Boilers.Boiler #1, top', 'default:Line 2.Speed'],
        $site: 'default',
      };
      return target.replace(/\$\w+/g, (name) => {
        const v = vars[name];
        if (v === undefined) {
          return name;
        }
        return format ? format(v) : String(v);
      });
    },
  }),
}));

const ds = new DataSource({ jsonData: {} } as DataSourceInstanceSettings<I3xOptions>);

describe('applyTemplateVariables', () => {
  it('expands a multi-value variable into one ElementId per value', () => {
    const q = ds.applyTemplateVariables({ refId: 'A', elementIds: ['$tags', 'default:Fixed'] }, {});
    expect(q.elementIds).toEqual(['default:Boilers.Boiler #1, top', 'default:Line 2.Speed', 'default:Fixed']);
  });

  it('substitutes a single-value variable inside an ElementId', () => {
    const q = ds.applyTemplateVariables({ refId: 'A', elementIds: ['$site:FL001.State'] }, {});
    expect(q.elementIds).toEqual(['default:FL001.State']);
  });
});

describe('filterQuery', () => {
  it('skips empty and hidden queries', () => {
    expect(ds.filterQuery({ refId: 'A', elementIds: [] })).toBe(false);
    expect(ds.filterQuery({ refId: 'A', elementIds: ['x'], hide: true })).toBe(false);
    expect(ds.filterQuery({ refId: 'A', elementIds: ['x'] })).toBe(true);
  });
});

import { loadFieldPicks, saveFieldPicks } from './filterStore';

describe('filterStore', () => {
  beforeEach(() => window.sessionStorage.clear());

  it('keeps filters per data source', () => {
    saveFieldPicks('a', { Area: ['Filling'], Site: [] });
    expect(loadFieldPicks('a')).toEqual({ Area: ['Filling'], Site: [] });
    expect(loadFieldPicks('b')).toEqual({});
  });

  it('forgets a data source whose filters are all removed', () => {
    saveFieldPicks('a', { Area: ['Filling'] });
    saveFieldPicks('a', {});
    expect(window.sessionStorage.length).toBe(0);
  });

  it('ignores stored values of the wrong shape', () => {
    window.sessionStorage.setItem('timebase-datasource.treeFilters.a', '{"Area":"Filling","Site":["X",3]}');
    expect(loadFieldPicks('a')).toEqual({ Site: ['X'] });
    window.sessionStorage.setItem('timebase-datasource.treeFilters.a', 'not json');
    expect(loadFieldPicks('a')).toEqual({});
  });
});

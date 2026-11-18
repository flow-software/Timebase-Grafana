import { parsePick } from './HistoryField';
import { upgrade } from './QueryEditor';

describe('parsePick', () => {
  it('reads an id, or an id with a property after the last slash', () => {
    expect(parsePick(' $machine-Production ')).toEqual({ instance: '$machine-Production' });
    expect(parsePick('$machine-Production/FeedRate')).toEqual({
      instance: '$machine-Production',
      property: 'FeedRate',
    });
    expect(parsePick('a/b.c')).toEqual({ instance: 'a/b.c' });
    expect(parsePick('  ')).toBeUndefined();
  });
});

describe('upgrade', () => {
  it('turns older one-instance History into a selection', () => {
    const q = upgrade({ refId: 'A', queryType: 'history', instanceId: 'X', properties: ['A', 'B'] });
    expect(q.selection).toEqual([
      { instance: 'X', property: 'A' },
      { instance: 'X', property: 'B' },
    ]);
    expect(q.properties).toBeUndefined();
  });

  it('leaves History of a type as saved', () => {
    const q = upgrade({ refId: 'A', queryType: 'history', scope: 'type', entityType: 'T', properties: ['A'] });
    expect(q.selection).toBeUndefined();
    expect(q.properties).toEqual(['A']);
  });
});

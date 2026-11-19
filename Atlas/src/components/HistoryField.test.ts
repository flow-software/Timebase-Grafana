import { parsePick } from './HistoryField';
import { upgrade } from './QueryEditor';

describe('parsePick', () => {
  it('takes a $variable as the property', () => {
    expect(parsePick('CNC01-Production/$prop')).toEqual({ instance: 'CNC01-Production', property: '$prop' });
  });

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

  it('turns older History of a type or a nested branch into type and nested picks', () => {
    const t = upgrade({ refId: 'A', queryType: 'history', scope: 'type', entityType: 'T', properties: ['A'] });
    expect(t.selection).toEqual([{ instance: '', type: 'T', property: 'A', scope: 'type' }]);
    expect(t.properties).toBeUndefined();
    const n = upgrade({ refId: 'A', queryType: 'history', scope: 'children', instanceId: 'X', properties: ['A'] });
    expect(n.selection).toEqual([{ instance: 'X', property: 'A', scope: 'nested' }]);
  });

  it('turns older Values into a selection', () => {
    expect(upgrade({ refId: 'A', queryType: 'values', instanceId: 'X' }).selection).toEqual([{ instance: 'X' }]);
    expect(upgrade({ refId: 'A', queryType: 'values', scope: 'children', instanceId: '$m' }).selection).toEqual([
      { instance: '$m', scope: 'nested' },
    ]);
    const t = upgrade({ refId: 'A', queryType: 'values', scope: 'type', entityType: 'T' });
    expect(t.selection).toEqual([{ instance: '', type: 'T', scope: 'type' }]);
    expect(t.layout).toBe('columns');
  });
});

describe('parsePick', () => {
  it('reads type picks', () => {
    expect(parsePick('type:CNCProduction/FeedRate')).toEqual({
      instance: '',
      type: 'CNCProduction',
      scope: 'type',
      property: 'FeedRate',
    });
    expect(parsePick('type:CNCMachine')).toEqual({ instance: '', type: 'CNCMachine', scope: 'type' });
  });
});

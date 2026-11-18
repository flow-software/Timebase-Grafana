import { cypherParameterNames, cypherString, toCypher } from './cypher';

const BACKSLASH = String.fromCharCode(92);

describe('cypherParameterNames', () => {
  it('finds parameters in order, once each', () => {
    expect(cypherParameterNames('MATCH (n) WHERE n.entityId IN $machines AND n.x = $x OR n.y = $machines')).toEqual([
      'machines',
      'x',
    ]);
  });

  it('ignores $ inside string literals and backticks', () => {
    const q = 'RETURN "cost in $usd", \'$5\', `$odd`, $real';
    expect(cypherParameterNames(q)).toEqual(['real']);
  });

  it('handles an escaped quote inside a literal', () => {
    const q = 'RETURN "a' + BACKSLASH + '"$no", $yes';
    expect(cypherParameterNames(q)).toEqual(['yes']);
  });
});

describe('toCypher', () => {
  it('whole-domain graph', () => {
    expect(toCypher({ refId: 'A', queryType: 'graph' })).toBe('MATCH (a)-[r]->(b)\nRETURN a, r, b\nLIMIT 2000');
  });

  it('one hop around an instance, quoting the id', () => {
    expect(toCypher({ refId: 'A', queryType: 'graph', instanceId: 'a"b', limit: 50 })).toBe(
      'MATCH (a)-[r]-(b)\nWHERE a.entityId = "a' + BACKSLASH + '"b"\nRETURN a, r, b\nLIMIT 50'
    );
  });

  it('has no single query for multi-hop graphs or unsafe type names', () => {
    expect(toCypher({ refId: 'A', queryType: 'graph', instanceId: 'x', depth: 2 })).toBeUndefined();
    expect(toCypher({ refId: 'A', queryType: 'instances', entityType: 'X) DELETE n' })).toBeUndefined();
  });

  it('instances table', () => {
    expect(toCypher({ refId: 'A', queryType: 'instances', entityType: 'CNCProduction' })).toBe(
      'MATCH (n:CNCProduction)\nRETURN n\nLIMIT 1000'
    );
  });
});

describe('cypherString', () => {
  it('escapes backslashes and quotes', () => {
    const input = 'a' + BACKSLASH + 'b"c';
    expect(cypherString(input)).toBe('"a' + BACKSLASH + BACKSLASH + 'b' + BACKSLASH + '"c"');
  });
});

describe('toCypher history', () => {
  it('writes a typed UNWIND with the panel time range for one property of a type', () => {
    const c = toCypher({
      refId: 'A',
      queryType: 'history',
      scope: 'type',
      entityType: 'CNCProduction',
      properties: ['FeedRate'],
    });
    expect(c).toContain('MATCH (n:CNCProduction)');
    expect(c).toContain('UNWIND n.FeedRate AS v');
    expect(c).toContain('datetime($from)');
  });

  it('writes the raw read for one picked property', () => {
    const c = toCypher({
      refId: 'A',
      queryType: 'history',
      selection: [{ instance: 'CNC01-Production', property: 'SpindleTemp', type: 'CNCProduction' }],
    });
    expect(c).toContain('MATCH (n:CNCProduction) WHERE n.entityId = "CNC01-Production"');
    expect(c).toContain('RETURN v.Timestamp AS time, v.Value AS SpindleTemp');
  });

  it('has no single query for whole instances, several picks or an unknown type', () => {
    expect(toCypher({ refId: 'A', queryType: 'history', selection: [{ instance: 'x', type: 'T' }] })).toBeUndefined();
    expect(
      toCypher({
        refId: 'A',
        queryType: 'history',
        selection: [
          { instance: 'x', property: 'A', type: 'T' },
          { instance: 'y', property: 'A', type: 'T' },
        ],
      })
    ).toBeUndefined();
    expect(
      toCypher({ refId: 'A', queryType: 'history', selection: [{ instance: '$m', property: 'A' }] })
    ).toBeUndefined();
  });

  it('has no single query for instances or several properties', () => {
    expect(
      toCypher({ refId: 'A', queryType: 'history', scope: 'instance', instanceId: 'x', properties: ['A'] })
    ).toBeUndefined();
    expect(
      toCypher({ refId: 'A', queryType: 'history', scope: 'type', entityType: 'T', properties: ['A', 'B'] })
    ).toBeUndefined();
  });
});

import { cypherParameterNames, cypherString, historyCypher, toCypher } from './cypher';

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

  it('has no single query for instances or several properties', () => {
    expect(
      toCypher({ refId: 'A', queryType: 'history', scope: 'instance', instanceId: 'x', properties: ['A'] })
    ).toBeUndefined();
    expect(
      toCypher({ refId: 'A', queryType: 'history', scope: 'type', entityType: 'T', properties: ['A', 'B'] })
    ).toBeUndefined();
  });
});

describe('historyCypher', () => {
  const nodes = [
    {
      id: 'CNC01-Production',
      type: 'CNCProduction',
      live: [
        { name: 'FeedRate', kind: 'number' as const },
        { name: 'JobNumber', kind: 'text' as const },
      ],
    },
    { id: 'CNC02-Production', type: 'CNCProduction', live: [{ name: 'FeedRate', kind: 'number' as const }] },
    { id: 'CNC01-SpindleMotor', type: 'MotorType', live: [{ name: 'RPM', kind: 'number' as const }] },
  ];

  it('writes one typed read per type and property, joined with UNION ALL', () => {
    const { cypher, error } = historyCypher(
      {
        refId: 'A',
        queryType: 'history',
        selection: [
          { instance: 'CNC01-Production' },
          { instance: 'CNC02-Production', property: 'FeedRate' },
          { instance: 'CNC01-SpindleMotor' },
        ],
      },
      nodes
    );
    expect(error).toBeUndefined();
    expect(cypher).toContain('MATCH (n:CNCProduction) WHERE n.entityId IN ["CNC01-Production", "CNC02-Production"]');
    expect(cypher).toContain('MATCH (n:MotorType) WHERE n.entityId IN ["CNC01-SpindleMotor"]');
    expect(cypher!.split('UNION ALL')).toHaveLength(2);
    expect(cypher).toContain('relative(v.Timestamp, $bucket) AS time');
    expect(cypher).toContain('// Left out, not numbers: CNC01-Production JobNumber (text)');
    expect(cypher).not.toContain(' AS instance');
  });

  it('reads raw values for Raw and fixed buckets for Hour', () => {
    const sel = [{ instance: 'CNC02-Production', property: 'FeedRate' }];
    expect(
      historyCypher({ refId: 'A', queryType: 'history', aggregation: 'raw', selection: sel }, nodes).cypher
    ).toContain('v.Timestamp AS time, v.Value AS value');
    expect(
      historyCypher(
        { refId: 'A', queryType: 'history', aggregation: 'hour', includeMinMax: true, selection: sel },
        nodes
      ).cypher
    ).toContain('relative(v.Timestamp, "h") AS time, avg(v.Value) AS value, min(v.Value) AS min');
  });

  it('explains what cannot be converted', () => {
    expect(historyCypher({ refId: 'A', queryType: 'history', selection: [{ instance: '$m' }] }, nodes).error).toContain(
      '$m'
    );
    expect(
      historyCypher(
        { refId: 'A', queryType: 'history', selection: [{ instance: 'CNC01-Production', property: 'JobNumber' }] },
        nodes
      ).error
    ).toContain('no number values');
  });
});

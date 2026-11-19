import { cypherParameterNames, cypherString, historyCypher, toCypher, valuesCypher } from './cypher';

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

describe('nested and type picks', () => {
  const nodes = [
    { id: 'CNC01', type: 'CNCMachine' },
    {
      id: 'CNC01-Production',
      type: 'CNCProduction',
      parentId: 'CNC01',
      live: [{ name: 'FeedRate', kind: 'number' as const }],
    },
    { id: 'CNC02', type: 'CNCMachine' },
    {
      id: 'CNC02-Production',
      type: 'CNCProduction',
      parentId: 'CNC02',
      live: [{ name: 'FeedRate', kind: 'number' as const }],
    },
  ];

  it('expands a type pick for Trend', () => {
    const { cypher } = historyCypher(
      {
        refId: 'A',
        queryType: 'history',
        selection: [{ instance: '', type: 'CNCProduction', property: 'FeedRate', scope: 'type' }],
      },
      nodes
    );
    expect(cypher).toContain('WHERE n.entityId IN ["CNC01-Production", "CNC02-Production"]');
  });

  it('reads a whole type with a typed MATCH for Current, and a nested pick by ids', () => {
    expect(
      valuesCypher(
        { refId: 'A', queryType: 'values', selection: [{ instance: '', type: 'CNCMachine', scope: 'type' }] },
        nodes
      ).cypher
    ).toBe('MATCH (n:CNCMachine)\nRETURN n');
    expect(
      valuesCypher({ refId: 'A', queryType: 'values', selection: [{ instance: 'CNC01', scope: 'nested' }] }, nodes)
        .cypher
    ).toContain('IN ["CNC01", "CNC01-Production"]');
  });
});

describe('variables in Edit as Cypher', () => {
  const nodes = [
    {
      id: 'CNC01-Production',
      type: 'CNCProduction',
      live: [
        { name: 'FeedRate', kind: 'number' as const },
        { name: 'SpindleTemp', kind: 'number' as const },
        { name: 'JobNumber', kind: 'text' as const },
      ],
    },
    { id: 'CNC02-Production', type: 'CNCProduction', live: [{ name: 'FeedRate', kind: 'number' as const }] },
  ];
  const vars = {
    values: (t: string) => (t.includes('$machines') ? ['CNC01', 'CNC02'].map((m) => t.replace('$machines', m)) : [t]),
    isMulti: (name: string) => name === 'machines' || name === 'prop',
  };

  it('keeps instance and property variables as Cypher parameters', () => {
    const { cypher, error } = historyCypher(
      { refId: 'A', queryType: 'history', selection: [{ instance: '$machines-Production', property: '$prop' }] },
      nodes,
      vars
    );
    expect(error).toBeUndefined();
    expect(cypher).toContain('MATCH (n:CNCProduction) WHERE n.entityId IN [m IN $machines | m + "-Production"]');
    expect(cypher).toContain('AND "FeedRate" IN $prop');
    expect(cypher).toContain('AND "SpindleTemp" IN $prop');
    expect(cypher).not.toContain('"JobNumber" IN');
    expect(cypher!.split('UNION ALL')).toHaveLength(2);
  });

  it('wraps a single-value variable in a list', () => {
    const one = { ...vars, isMulti: () => false };
    const { cypher } = historyCypher(
      { refId: 'A', queryType: 'history', selection: [{ instance: 'CNC01-Production', property: '$prop' }] },
      nodes,
      one
    );
    expect(cypher).toContain('WHERE n.entityId IN ["CNC01-Production"]');
    expect(cypher).toContain('AND "FeedRate" IN [$prop]');
  });

  it('writes Current with a variable id list', () => {
    expect(
      valuesCypher({ refId: 'A', queryType: 'values', selection: [{ instance: '$machines-Production' }] }, nodes, vars)
        .cypher
    ).toBe('MATCH (n)\nWHERE n.entityId IN [m IN $machines | m + "-Production"]\nRETURN n');
  });
});

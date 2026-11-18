import { ancestorsOf, buildTree, filterTree, liveDescendants, pickTree } from './tree';
import { TreeNodeData } from './types';

const nodes: TreeNodeData[] = [
  { id: 'CNC01', label: 'CNC01', type: 'CNCMachine' },
  { id: 'CNC01-Channel', label: 'Channel', type: 'ChannelType', parentId: 'CNC01' },
  { id: 'CNC01-PositionX', label: 'Position X', type: 'PositionType', parentId: 'CNC01-Channel' },
  { id: 'SQL-TestDB', type: 'SqlConnection' },
  { id: '110-FQ-001.PV', type: 'SqlTag', parentId: 'SQL-TestDB' },
  { id: 'orphan', type: 'X', parentId: 'missing' },
];

describe('buildTree', () => {
  it('nests by parentId and keeps orphans as roots', () => {
    const roots = buildTree(nodes);
    expect(roots.map((r) => r.node.id)).toEqual(['CNC01', 'SQL-TestDB', 'orphan']);
    expect(roots[0].children[0].children[0].node.id).toBe('CNC01-PositionX');
    expect(roots[1].children[0].node.id).toBe('110-FQ-001.PV');
  });
});

describe('ancestorsOf', () => {
  it('walks up to the root', () => {
    expect(ancestorsOf(nodes, 'CNC01-PositionX')).toEqual(['CNC01-Channel', 'CNC01']);
  });
});

describe('filterTree', () => {
  it('keeps matches with their ancestors', () => {
    const { roots, matches } = filterTree(buildTree(nodes), 'position');
    expect(matches).toBe(1);
    expect(roots.map((r) => r.node.id)).toEqual(['CNC01']);
    expect(roots[0].children[0].children.map((c) => c.node.id)).toEqual(['CNC01-PositionX']);
  });

  it('matches type names', () => {
    expect(filterTree(buildTree(nodes), 'sqltag').matches).toBe(1);
  });
});

describe('pickTree', () => {
  const live: TreeNodeData[] = [
    { id: 'CNC01', type: 'CNCMachine' },
    {
      id: 'CNC01-Production',
      type: 'CNCProduction',
      parentId: 'CNC01',
      live: [
        { name: 'FeedRate', kind: 'number' },
        { name: 'JobNumber', kind: 'text' },
      ],
    },
    { id: 'CNC01-Info', type: 'Info', parentId: 'CNC01' },
    { id: 'Site', type: 'Site' },
  ];

  it('drops branches without live values', () => {
    const { roots } = pickTree(buildTree(live), '', true);
    expect(roots.map((r) => r.node.id)).toEqual(['CNC01']);
    expect(roots[0].children.map((c) => c.node.id)).toEqual(['CNC01-Production']);
  });

  it('keeps everything without onlyLive', () => {
    expect(pickTree(buildTree(live), '', false).roots.map((r) => r.node.id)).toEqual(['CNC01', 'Site']);
  });

  it('filters by property name and shows only matching properties', () => {
    const { roots, matches } = pickTree(buildTree(live), 'feed', true);
    expect(matches).toBe(1);
    expect(roots[0].children[0].props.map((p) => p.name)).toEqual(['FeedRate']);
  });

  it('an instance match shows all its properties', () => {
    const { roots } = pickTree(buildTree(live), 'production', true);
    expect(roots[0].children[0].props).toHaveLength(2);
  });

  it('lists nested instances with live values', () => {
    const { roots } = pickTree(buildTree(live), '', false);
    expect(liveDescendants(roots[0])).toEqual(['CNC01-Production']);
  });
});

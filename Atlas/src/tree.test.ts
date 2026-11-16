import { ancestorsOf, buildTree, filterTree } from './tree';
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

import { buildTree, filterTree, TreeNode } from './tree';
import { ObjectOption } from './types';

const tag = (elementId: string, parentId?: string): ObjectOption => ({
  elementId,
  displayName: elementId.replace(/^[^:]*:/, ''),
  type: 'Tag',
  parentId,
  isFolder: false,
});
const folder = (elementId: string, parentId?: string, displayName?: string): ObjectOption => ({
  elementId,
  displayName: displayName ?? elementId,
  type: 'Folder',
  parentId,
  isFolder: true,
});

/** Renders a tree as indented lines: "+" folder, "-" tag. */
function outline(nodes: TreeNode[], depth = 0): string[] {
  return nodes.flatMap((n) => [
    `${'  '.repeat(depth)}${n.isTag ? '-' : '+'} ${n.label}`,
    ...outline(n.children, depth + 1),
  ]);
}

describe('buildTree', () => {
  it('splits Timebase dotted tag paths into folders under the dataset', () => {
    // Shapes taken from Timebase Historian 2.0.813: every tag's parent is the dataset.
    const tree = buildTree([
      folder('default', undefined, 'default'),
      tag('default:Boilers.Boiler #1.BoilerStatus.Temperature.Top', 'default'),
      tag('default:Boilers.Boiler #1.BoilerStatus.Pressure', 'default'),
      tag('default:Boilers.Boiler #2.Manufacturer', 'default'),
      tag('default:131-TT-001.PV', 'default'),
    ]);

    expect(outline(tree)).toEqual([
      '+ default',
      '  + 131-TT-001',
      '    - PV',
      '  + Boilers',
      '    + Boiler #1',
      '      + BoilerStatus',
      '        + Temperature',
      '          - Top',
      '        - Pressure',
      '    + Boiler #2',
      '      - Manufacturer',
    ]);
    expect(tree[0].tagIds).toHaveLength(4);
  });

  it('uses i3X parent links for servers with opaque ids, without splitting names', () => {
    const tree = buildTree([
      folder('site-1', undefined, 'Plant A'),
      folder('area-7', 'site-1', 'Line 1.5'),
      { elementId: 'uuid-1', displayName: 'Speed v2.1', type: 'Tag', parentId: 'area-7', isFolder: false },
    ]);

    expect(outline(tree)).toEqual(['+ Plant A', '  + Line 1.5', '    - Speed v2.1']);
    expect(tree[0].children[0].children[0].elementId).toBe('uuid-1');
  });

  it('merges a real folder with the virtual folder of the same path', () => {
    const tree = buildTree([
      folder('default'),
      folder('default:OpcPlc.Special', 'default'),
      tag('default:OpcPlc.Special.X', 'default:OpcPlc.Special'),
      tag('default:OpcPlc.Fast.Y', 'default'),
    ]);

    expect(outline(tree)).toEqual(['+ default', '  + OpcPlc', '    + Fast', '      - Y', '    + Special', '      - X']);
    expect(tree[0].children[0].children[1].elementId).toBe('default:OpcPlc.Special');
  });

  it('keeps a tag and a folder that share a name, folders first', () => {
    const tree = buildTree([
      folder('default'),
      tag('default:Boiler.DeviceHealth', 'default'),
      tag('default:Boiler.DeviceHealth.Code', 'default'),
    ]);

    expect(outline(tree)).toEqual(['+ default', '  + Boiler', '    + DeviceHealth', '      - Code', '    - DeviceHealth']);
  });

  it('survives parent cycles and missing parents', () => {
    const tree = buildTree([
      folder('a', 'b', 'A'),
      folder('b', 'a', 'B'),
      tag('x', 'nowhere'),
    ]);

    expect(outline(tree).sort()).toEqual(['  + A', '+ B', '- x'].sort());
  });

  it('sorts numbers naturally', () => {
    const tree = buildTree([folder('d'), tag('d:L10', 'd'), tag('d:L2', 'd'), tag('d:L1', 'd')]);
    expect(tree[0].children.map((c) => c.label)).toEqual(['L1', 'L2', 'L10']);
  });
});

describe('filterTree', () => {
  const tree = buildTree([
    folder('default'),
    tag('default:Boilers.Boiler #1.Pressure', 'default'),
    tag('default:Boilers.Boiler #2.Pressure', 'default'),
    tag('default:Line1.Speed', 'default'),
  ]);

  it('keeps matches with their ancestors', () => {
    expect(outline(filterTree(tree, 'speed'))).toEqual(['+ default', '  + Line1', '    - Speed']);
  });

  it('keeps the whole subtree of a matching folder', () => {
    const out = filterTree(tree, 'boiler #1');
    expect(outline(out)).toEqual(['+ default', '  + Boilers', '    + Boiler #1', '      - Pressure']);
    expect(out[0].tagIds).toEqual(['default:Boilers.Boiler #1.Pressure']);
  });

  it('returns the tree unchanged for an empty query', () => {
    expect(filterTree(tree, '  ')).toBe(tree);
  });
});

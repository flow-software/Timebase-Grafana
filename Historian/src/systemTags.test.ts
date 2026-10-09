import { systemTagMatcher } from './systemTags';
import { ObjectOption } from './types';

const objects: ObjectOption[] = [
  { elementId: 'opcplc', displayName: 'opcplc', type: 'Dataset', isFolder: true },
  { elementId: 'opcplc:Process.CPU', displayName: 'Process.CPU', type: 'Tag', parentId: 'opcplc', isFolder: false },
  { elementId: 'opcplc:Time.Second', displayName: 'Time.Second', type: 'Tag', parentId: 'opcplc', isFolder: false },
  // User tags whose paths start like system folders must stay visible.
  { elementId: 'opcplc:Process.Line1.CPU', displayName: 'Process.Line1.CPU', type: 'Tag', parentId: 'opcplc', isFolder: false },
  { elementId: 'opcplc:Boilers.Pressure', displayName: 'Boilers.Pressure', type: 'Tag', parentId: 'opcplc', isFolder: false },
  // Same name, but not directly under a dataset.
  { elementId: 'area:Process.CPU', displayName: 'Process.CPU', type: 'Tag', parentId: 'area', isFolder: false },
  { elementId: 'area', displayName: 'area', type: 'Folder', isFolder: true },
];

describe('systemTagMatcher', () => {
  const isSystem = systemTagMatcher(objects);

  it('matches the historian health tags directly under a dataset', () => {
    expect(objects.filter(isSystem).map((o) => o.elementId)).toEqual(['opcplc:Process.CPU', 'opcplc:Time.Second']);
  });

  it('never matches datasets or folders', () => {
    expect(isSystem(objects[0])).toBe(false);
  });

  it('trusts the native API flag, whatever the name', () => {
    const tag: ObjectOption = {
      elementId: 'plant:Health.New',
      displayName: 'Health.New',
      type: 'Tag',
      parentId: 'plant',
      isFolder: false,
      isSystem: true,
    };
    expect(systemTagMatcher([tag])(tag)).toBe(true);
  });
});

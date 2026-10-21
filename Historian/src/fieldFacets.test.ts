import { fieldFacets, matchesPicks } from './fieldFacets';
import { ObjectOption } from './types';

const tag = (name: string, fields?: Record<string, string>): ObjectOption => ({
  elementId: `plant:${name}`,
  displayName: name,
  type: 'Tag',
  parentId: 'plant',
  isFolder: false,
  fields,
});

const tags = [
  tag('A', { Site: 'Southlake', Area: 'Filling' }),
  tag('B', { Site: 'Southlake', Area: 'Packing' }),
  tag('C', { site: 'Dallas', Area: 'Filling' }),
  tag('D'),
];

describe('matchesPicks', () => {
  it('needs one picked value per key with picks, ignoring key case', () => {
    expect(matchesPicks(tags[2].fields, { Site: ['Dallas', 'Austin'] })).toBe(true);
    expect(matchesPicks(tags[0].fields, { Site: ['Dallas'] })).toBe(false);
    expect(matchesPicks(tags[0].fields, { Site: ['Southlake'], Area: ['Packing'] })).toBe(false);
    expect(matchesPicks(tags[3].fields, { Site: ['Southlake'] })).toBe(false);
  });

  it('ignores keys with nothing picked', () => {
    expect(matchesPicks(tags[3].fields, { Site: [] })).toBe(true);
  });
});

describe('fieldFacets', () => {
  it('lists each key once with value counts', () => {
    expect(fieldFacets(tags, {})).toEqual([
      {
        key: 'Area',
        values: [
          { value: 'Filling', count: 2 },
          { value: 'Packing', count: 1 },
        ],
      },
      {
        key: 'Site',
        values: [
          { value: 'Dallas', count: 1 },
          { value: 'Southlake', count: 2 },
        ],
      },
    ]);
  });

  it("counts a key's values over tags passing the other keys' picks", () => {
    const area = fieldFacets(tags, { Site: ['Dallas'] }).find((f) => f.key === 'Area');
    expect(area?.values).toEqual([{ value: 'Filling', count: 1 }]);
    // A key's own picks do not narrow its options.
    const site = fieldFacets(tags, { Site: ['Dallas'] }).find((f) => f.key === 'Site');
    expect(site?.values.map((v) => v.value)).toEqual(['Dallas', 'Southlake']);
  });

  it('keeps a picked value listed when nothing has it any more', () => {
    const area = fieldFacets(tags, { Site: ['Dallas'], Area: ['Packing'] }).find((f) => f.key === 'Area');
    expect(area?.values).toEqual([
      { value: 'Filling', count: 1 },
      { value: 'Packing', count: 0 },
    ]);
  });
});

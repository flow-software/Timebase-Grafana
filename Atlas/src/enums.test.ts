import { enumOptions, relationshipOptions, typedKeys } from './enums';

describe('enumeration variables', () => {
  it('sends number and boolean keys typed, text keys as text', () => {
    expect(typedKeys(['1', '3'], 'number')).toEqual([1, 3]);
    expect(typedKeys('2', 'number')).toBe(2);
    expect(typedKeys(['true'], 'boolean')).toEqual([true]);
    expect(typedKeys(['Running'], 'text')).toEqual(['Running']);
    expect(typedKeys(['$__all', ''], 'number')).toEqual(['$__all', '']);
  });

  it('shows labels and keeps keys as values', () => {
    const e = {
      name: 'ModeEnum',
      label: 'Mode',
      keyKind: 'number' as const,
      members: [{ key: '1', label: 'Automatic' }],
    };
    expect(enumOptions(e)).toEqual({ text: ['Automatic'], value: ['1'] });
    expect(enumOptions(undefined)).toEqual({ text: [], value: [] });
  });
});

describe('relationship type variables', () => {
  const types = [
    { uri: 'urn:x:contains', name: 'contains', isContainment: true, color: '#999' },
    { uri: 'urn:x:feeds', name: 'feeds', isContainment: false, color: '#f90' },
  ];
  it('offers every type, or only the picked ones', () => {
    expect(relationshipOptions(types)).toEqual({
      text: ['contains', 'feeds'],
      value: ['urn:x:contains', 'urn:x:feeds'],
    });
    expect(relationshipOptions(types, ['urn:x:feeds'])).toEqual({ text: ['feeds'], value: ['urn:x:feeds'] });
  });
});

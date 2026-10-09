import { urlForApi } from './url';

describe('urlForApi', () => {
  it('adds /i3x/v1 for i3X', () => {
    expect(urlForApi('https://historian:4512', 'i3x')).toBe('https://historian:4512/i3x/v1');
    expect(urlForApi('https://historian:4512/', 'i3x')).toBe('https://historian:4512/i3x/v1');
    expect(urlForApi('https://historian:4512/i3x/v1', 'i3x')).toBe('https://historian:4512/i3x/v1');
  });

  it('removes API paths for the native API', () => {
    expect(urlForApi('https://historian:4512/i3x/v1/', 'timebase')).toBe('https://historian:4512');
    expect(urlForApi('https://historian:4512/api', 'timebase')).toBe('https://historian:4512');
    expect(urlForApi('https://proxy.example.com/timebase', 'timebase')).toBe('https://proxy.example.com/timebase');
  });

  it('leaves an empty URL empty', () => {
    expect(urlForApi('', 'i3x')).toBe('');
    expect(urlForApi(undefined, 'timebase')).toBe('');
  });
});

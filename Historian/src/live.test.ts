import { liveChannelPath } from './live';

describe('liveChannelPath', () => {
  // The same vectors are pinned in pkg/plugin/stream_test.go, so both sides agree.
  it('encodes tag names the backend can decode', () => {
    expect(liveChannelPath('default:Boilers.Boiler #1.BoilerStatus.Pressure')).toBe(
      'tag/-/ZGVmYXVsdDpCb2lsZXJzLkJvaWxlciAjMS5Cb2lsZXJTdGF0dXMuUHJlc3N1cmU'
    );
    expect(liveChannelPath('plant:Température.°F', true)).toBe('tag/n/cGxhbnQ6VGVtcMOpcmF0dXJlLsKwRg');
  });

  it('only uses characters Grafana Live allows in a path', () => {
    expect(liveChannelPath('a:b c/d+e?f=g#h')).toMatch(/^[A-Za-z0-9_\-/=.]+$/);
  });
});

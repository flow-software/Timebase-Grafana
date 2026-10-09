import { ApiType } from './types';

/**
 * Rewrites a URL for the chosen API: i3X lives under /i3x/v1, the native API
 * is addressed from the historian's base URL. Empty stays empty so the
 * placeholder still shows.
 */
export function urlForApi(url: string | undefined, api: ApiType): string {
  const trimmed = (url ?? '').trim();
  if (!trimmed) {
    return '';
  }
  const base = trimmed
    .replace(/\/+$/, '')
    .replace(/\/i3x\/v1$/i, '')
    .replace(/\/api$/i, '');
  return api === 'i3x' ? `${base}/i3x/v1` : base;
}

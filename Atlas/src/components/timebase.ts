import { GrafanaTheme2 } from '@grafana/data';

/**
 * Timebase brand colours (Timebase.Core.React.UI styles/tokens.css), used as
 * accents in our own pieces of the editor. Grafana's own components keep the
 * Grafana theme. On light themes the darker shade is used for text so it
 * stays readable.
 */
export function timebase(theme: GrafanaTheme2) {
  const dark = theme.isDark;
  return {
    /** Timebase Blue, primary UI. */
    blue: '#31AEE4',
    /** Text in Timebase blue: Control Blue on light themes. */
    text: dark ? '#31AEE4' : '#1B81AD',
    /** Lucky Green (success); Jungle Green on light themes. */
    green: dark ? '#34D399' : '#06624E',
    /** Vivid Orange (warning). */
    orange: dark ? '#F59E0B' : '#B45309',
    /** Lucky Blue (info); Control Blue on light themes. */
    cyan: dark ? '#4CD6F5' : '#1B81AD',
  };
}

/**
 * Builds the Grafana Live channel path for one tag: "tag/<flags>/<id>".
 * Channel paths only allow [A-Za-z0-9_-/=.], and tag names contain spaces,
 * '#' and ':', so the ElementId travels as unpadded base64url of its UTF-8
 * bytes. flags is "n" when samples whose quality is not Good are blanked.
 * The backend parses this in pkg/plugin/stream.go.
 */
export function liveChannelPath(elementId: string, badQualityAsNull?: boolean): string {
  return `tag/${badQualityAsNull ? 'n' : '-'}/${base64Url(elementId)}`;
}

function base64Url(text: string): string {
  let binary = '';
  for (const byte of new TextEncoder().encode(text)) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

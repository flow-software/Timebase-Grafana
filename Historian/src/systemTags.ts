import { ObjectOption } from './types';

/**
 * Health tags the Timebase Historian creates in every dataset (seen on
 * Historian 2.0.813). Matched by exact name directly under a dataset, so a
 * user folder that happens to be called "Process" or "System" is not hidden.
 */
const HISTORIAN_SYSTEM_TAGS = new Set([
  'Dataset.Blocks',
  'Dataset.Blocks.Active',
  'Dataset.Data.Failure',
  'Dataset.ObjectBlocks',
  'Dataset.ObjectBlocks.Active',
  'Dataset.Reads',
  'Dataset.Size',
  'Dataset.State',
  'Dataset.Tags',
  'Dataset.Writes',
  'Dataset.Writes.Late',
  'Diagnostic.Fast.Failure',
  'Diagnostic.Medium.Failure',
  'Maintenance.Failure',
  'Process.CPU',
  'Process.Memory',
  'System.Space.Available',
  'Tag.Data.Failure',
  'Tag.Info.Failure',
  'Time.Hour',
  'Time.Minute',
  'Time.Second',
]);

/**
 * Returns a predicate that is true for the historian's own health tags. The
 * native API flags them itself; i3X does not, so there they are matched by name.
 */
export function systemTagMatcher(objects: ObjectOption[]): (o: ObjectOption) => boolean {
  const datasets = new Set(objects.filter((o) => o.type === 'Dataset').map((o) => o.elementId));
  return (o) => {
    if (o.isSystem) {
      return true;
    }
    if (o.isFolder || !o.parentId || !datasets.has(o.parentId)) {
      return false;
    }
    const prefix = `${o.parentId}:`;
    return o.elementId.startsWith(prefix) && HISTORIAN_SYSTEM_TAGS.has(o.elementId.slice(prefix.length));
  };
}

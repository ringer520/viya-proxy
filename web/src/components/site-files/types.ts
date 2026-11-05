// Shared shapes for the static-site file browser so the drawer, the preview
// pane and the upload composable all agree on the wire format.
export type Entry = {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified: string;
};

export interface FileShare {
  id: number;
  name: string;
  domain: string;
  type: "static" | "files";
  root_path: string;
  path: string;
  enabled: boolean;
  browse: boolean;
  webdav: boolean;
  webdav_write: boolean;
  auth_enabled: boolean;
  auth_user: string;
  listener_id: string;
  url: string;
  tls_mode: string;
}

export interface MountCommand {
  id: string;
  label: string;
  command: string;
}

// Pure helpers kept outside the Vue component so they can be unit-tested in a
// node environment without a DOM.
export function shareAddress(share: FileShare): string {
  return share.url || `https://${share.domain}${share.path || ""}`;
}

export function mountCommands(share: FileShare): MountCommand[] {
  const url = shareAddress(share);
  const user = share.auth_user || "USERNAME";
  return [
    { id: "windows", label: "Windows", command: `net use Z: "${url}" /user:${user} /persistent:yes` },
    { id: "macos", label: "macOS", command: `mount_webdav -i "${url}" /Volumes/share` },
    { id: "linux", label: "Linux", command: `sudo mount -t davfs "${url}" /mnt/share` },
    { id: "rclone", label: "rclone", command: `rclone config create share webdav url ${url} user ${user} vendor other` },
  ];
}

export function shareWarnings(share: FileShare): string[] {
  const warnings: string[] = [];
  if (share.webdav && !share.auth_enabled) {
    warnings.push("no-auth");
  }
  if (share.webdav && share.webdav_write && !share.auth_enabled) {
    warnings.push("write-no-auth");
  }
  if (!share.enabled) {
    warnings.push("disabled");
  }
  return warnings;
}

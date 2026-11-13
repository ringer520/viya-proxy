import { describe, expect, it } from "vitest";
import {
  shareAddress,
  mountCommands,
  shareWarnings,
} from "../src/components/file-shares/types";
import type { FileShare } from "../src/components/file-shares/types";

function share(overrides: Partial<FileShare> = {}): FileShare {
  return {
    id: 1,
    name: "Share",
    domain: "share.example.com",
    type: "files",
    root_path: "/srv/share",
    path: "",
    enabled: true,
    browse: true,
    webdav: true,
    webdav_write: false,
    auth_enabled: true,
    auth_user: "alice",
    listener_id: "https-main",
    url: "https://share.example.com",
    tls_mode: "auto",
    ...overrides,
  };
}

describe("Files & Shares address helpers", () => {
  it("uses the backend URL verbatim", () => {
    expect(shareAddress(share())).toBe("https://share.example.com");
  });

  it("keeps a non-default port in the address", () => {
    expect(shareAddress(share({ url: "https://nas.example.com:8443" })))
      .toBe("https://nas.example.com:8443");
  });

  it("keeps a plain-HTTP listener address", () => {
    expect(shareAddress(share({ url: "http://nas.example.com:8080/docs" })))
      .toBe("http://nas.example.com:8080/docs");
  });

  // A missing listener leaves the backend URL empty. Fabricating
  // https://<domain> here used to produce four mount commands that could never
  // work -- and could point at the wrong scheme or port.
  it("does not invent an address when the listener URL is missing", () => {
    expect(shareAddress(share({ url: "", path: "/docs" }))).toBe("");
    expect(shareAddress(share({ url: "   " }))).toBe("");
    expect(shareAddress(share({ url: "" }))).not.toContain("share.example.com");
  });
});

describe("mountCommands", () => {
  it("builds commands for all four platforms using the share address", () => {
    const cmds = mountCommands(share());
    expect(cmds.map((c) => c.id)).toEqual(["windows", "macos", "linux", "rclone"]);
    for (const cmd of cmds) {
      expect(cmd.command).toContain("https://share.example.com");
      expect(cmd.command.length).toBeGreaterThan(10);
    }
  });

  it("uses a placeholder when no account is configured", () => {
    const cmds = mountCommands(share({ auth_user: "" }));
    expect(cmds[0].command).toContain("USERNAME");
  });

  it("returns no commands when the listener URL is missing", () => {
    expect(mountCommands(share({ url: "", path: "/docs" }))).toEqual([]);
    expect(mountCommands(share({ url: "   " }))).toEqual([]);
  });

  it("never falls back to the bare domain in a command", () => {
    for (const cmd of mountCommands(share({ url: "", tls_mode: "none" }))) {
      expect(cmd.command).not.toContain("https://share.example.com");
    }
  });
});

describe("shareWarnings", () => {
  it("stays empty for a protected read-only share", () => {
    expect(shareWarnings(share())).toEqual([]);
  });

  it("flags anonymous shares", () => {
    expect(shareWarnings(share({ auth_enabled: false }))).toEqual(["no-auth"]);
  });

  it("escalates to write-without-auth when writes are enabled", () => {
    expect(shareWarnings(share({ auth_enabled: false, webdav_write: true })))
      .toEqual(["no-auth", "write-no-auth"]);
  });

  it("flags disabled shares", () => {
    expect(shareWarnings(share({ enabled: false }))).toEqual(["disabled"]);
  });

  it("flags plain-HTTP shares even when authenticated", () => {
    expect(shareWarnings(share({ tls_mode: "none" }))).toEqual(["plaintext"]);
  });

  it("reports both plaintext and anonymous access together", () => {
    expect(shareWarnings(share({ tls_mode: "none", auth_enabled: false })))
      .toEqual(["no-auth", "plaintext"]);
  });

  it("does not flag a non-WebDAV static site as anonymous", () => {
    expect(shareWarnings(share({ type: "static", webdav: false, auth_enabled: false }))).toEqual([]);
  });
});

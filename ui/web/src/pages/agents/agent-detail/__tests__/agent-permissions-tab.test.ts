/**
 * Drift guard between FE config_permissions form and BE validator at
 * internal/gateway/methods/config_permissions.go. Earlier the FE shipped
 * `file_writer` + `context_files` after the BE switched to a stricter
 * allow-list, which silently 400'd every file-permission grant from the UI.
 */
import { describe, it, expect } from "vitest";
import { FILE_GATES } from "../agent-permissions-tab";

// Mirror of the BE allow-list. Update both sides if the BE accepts more types.
const BE_ACCEPTED_CONFIG_TYPES = new Set([
  "write_file",
  "edit_file",
  "delete_file",
  "cron",
  "heartbeat",
  "*",
]);

describe("agent-permissions-tab :: FE/BE configType parity", () => {
  it("FILE_GATES is exactly the three split file actions", () => {
    expect([...FILE_GATES]).toEqual(["write_file", "edit_file", "delete_file"]);
  });

  it("every FILE_GATE is on the BE allow-list", () => {
    for (const gate of FILE_GATES) {
      expect(BE_ACCEPTED_CONFIG_TYPES.has(gate)).toBe(true);
    }
  });

  it("BE allow-list does not include legacy file_writer or context_files", () => {
    expect(BE_ACCEPTED_CONFIG_TYPES.has("file_writer")).toBe(false);
    expect(BE_ACCEPTED_CONFIG_TYPES.has("context_files")).toBe(false);
  });
});

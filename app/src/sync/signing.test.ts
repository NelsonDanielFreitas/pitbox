import { describe, expect, it } from "vitest";

import { OpKind, registrationMessage, signingMessage, toHex } from "./signing";

// Same inputs and expected hex as backend/internal/sync/domain/signing_vector_test.go.
const team = "0190a1b2-0000-7000-8000-000000000002";
const device = "0190a1b2-0000-7000-8000-000000000003";
const user = "0190a1b2-0000-7000-8000-000000000005";

describe("signing message", () => {
  it("matches the Go vector byte for byte", async () => {
    const message = await signingMessage(team, device, {
      opId: "0190a1b2-0000-7000-8000-000000000001",
      entity: "garage.car",
      entityId: "0190a1b2-0000-7000-8000-000000000004",
      kind: OpKind.patch,
      fieldsJson: new TextEncoder().encode('{"nickname":"Ação <&>"}'),
      baseVersion: 3,
      clientTsMs: 1793000000123,
    });
    expect(toHex(message)).toBe(
      "00000011706974626f782d73796e632d6f702d7631000000100190a1b2000070008000000000000001000000100190a1b2000070008000000000000002000000100190a1b20000700080000000000000030000000a6761726167652e636172000000100190a1b200007000800000000000000400000008000000000000000200000008000000000000000300000008000001a17720ca7b000000209750d7600148f1d08b70618c9d064965f49c443f76628e83d959306ad908d30b",
    );
  });

  it("encodes an absent base_version as -1", async () => {
    const withBase = await signingMessage(team, device, base(1));
    const without = await signingMessage(team, device, base(null));
    expect(toHex(without)).not.toBe(toHex(withBase));
    expect(toHex(without)).toContain("ffffffffffffffff");
  });

  it("matches the Go registration vector", () => {
    const key = new Uint8Array([0x30, 0x59, 0x01, 0x02, 0x03]);
    expect(toHex(registrationMessage(team, user, key))).toBe(
      "00000015706974626f782d73796e632d6465766963652d7631000000100190a1b2000070008000000000000002000000100190a1b2000070008000000000000005000000053059010203",
    );
  });

  it("refuses ids that are not uuids", async () => {
    await expect(signingMessage("nope", device, base(1))).rejects.toThrow();
  });
});

function base(baseVersion: number | null) {
  return {
    opId: "0190a1b2-0000-7000-8000-000000000001",
    entity: "garage.car",
    entityId: "0190a1b2-0000-7000-8000-000000000004",
    kind: OpKind.patch,
    fieldsJson: new TextEncoder().encode("{}"),
    baseVersion,
    clientTsMs: 1,
  };
}

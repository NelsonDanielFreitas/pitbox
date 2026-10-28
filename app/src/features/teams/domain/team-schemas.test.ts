import { describe, expect, it } from "vitest";

import { createTeamFormSchema } from "./team-schemas";
import { canManageTeam, canViewTeamSettings, hasStrongFactor } from "./roles";

describe("createTeamFormSchema", () => {
  it("trims name and club and leaves an empty category for the server default", () => {
    expect(createTeamFormSchema.parse({ name: "  Freitas Racing  ", category: "", club: "  " })).toEqual({
      name: "Freitas Racing",
      category: "",
      club: undefined,
    });
  });

  it("rejects empty or overlong names", () => {
    expect(createTeamFormSchema.safeParse({ name: "  ", category: "", club: "" }).success).toBe(false);
    expect(createTeamFormSchema.safeParse({ name: "x".repeat(81), category: "", club: "" }).success).toBe(false);
  });
});

describe("team permissions and factors", () => {
  it("keeps team management visible to members except guests and limits mutations to owners", () => {
    expect(canViewTeamSettings("owner")).toBe(true);
    expect(canViewTeamSettings("mechanic")).toBe(true);
    expect(canViewTeamSettings("pilot")).toBe(true);
    expect(canViewTeamSettings("guest")).toBe(false);
    expect(canManageTeam("owner")).toBe(true);
    expect(canManageTeam("mechanic")).toBe(false);
    expect(canManageTeam("pilot")).toBe(false);
  });

  it("recognizes only Zitadel's strong AMR values", () => {
    expect(hasStrongFactor(["pwd"])).toBe(false);
    expect(hasStrongFactor(["pwd", "user"])).toBe(true);
    expect(hasStrongFactor(["otp"])).toBe(true);
    expect(hasStrongFactor(["mfa"])).toBe(false);
  });
});

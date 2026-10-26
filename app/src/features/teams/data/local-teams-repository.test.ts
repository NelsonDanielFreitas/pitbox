import { describe, expect, it, vi } from "vitest";

import { query } from "@/db/client";
import { CorruptLocalDataError } from "@/db/errors";

import { LocalTeamsRepository } from "./local-teams-repository";

vi.mock("@/db/client", () => ({ query: vi.fn() }));

const validRow = {
  id: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60",
  name: "Equipa Alpha",
  category: "1/8 TT nitro",
  club: null,
  country_code: "PT",
  role: "owner",
};

describe("LocalTeamsRepository", () => {
  it("maps valid rows to the domain", async () => {
    vi.mocked(query).mockResolvedValue([validRow]);
    await expect(new LocalTeamsRepository().listMyTeams()).resolves.toEqual([
      {
        id: validRow.id,
        name: validRow.name,
        category: validRow.category,
        countryCode: validRow.country_code,
        club: undefined,
        role: validRow.role,
      },
    ]);
  });

  it("rejects a corrupt row instead of returning it", async () => {
    vi.mocked(query).mockResolvedValue([{ ...validRow, role: "admin", country_code: "Portugal" }]);
    await expect(new LocalTeamsRepository().listMyTeams()).rejects.toBeInstanceOf(
      CorruptLocalDataError,
    );
  });
});

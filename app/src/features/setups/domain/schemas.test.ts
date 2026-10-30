import { describe, expect, it } from "vitest";

import {
  feedbackFromRow,
  feedbackInputSchema,
  parseFeedbackInput,
  parseStoredData,
  setupFormSchema,
  setupRowSchema,
  setupVersionRowSchema,
  versionFromRow,
} from "./schemas";
import { canGiveFeedback, canReadSetups, canWriteSetups } from "./roles";

const id = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60";

describe("setupFormSchema", () => {
  it("trims the name and turns blank notes into null", () => {
    expect(setupFormSchema.parse({ name: "  Seca  ", notes: "  " })).toEqual({ name: "Seca", notes: null });
  });

  it.each([
    [{ name: "   ", notes: "" }, "setups.validation.required"],
    [{ name: "x".repeat(121), notes: "" }, "setups.validation.tooLong"],
    [{ name: "ok", notes: "x".repeat(2001) }, "setups.validation.tooLong"],
  ])("refuses %j", (input, message) => {
    const result = setupFormSchema.safeParse(input);
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe(message);
  });
});

describe("feedback input", () => {
  it("keeps the ratings given and drops the rest", () => {
    expect(parseFeedbackInput({ ratings: { traction: 4 }, trackZone: " Curva 3 ", note: "" })).toEqual({
      ratings: { traction: 4 },
      trackZone: "Curva 3",
      note: null,
    });
  });

  it("accepts a note alone", () => {
    expect(parseFeedbackInput({ ratings: {}, trackZone: "", note: "Nervoso" }).note).toBe("Nervoso");
  });

  it.each([
    [{ ratings: {}, trackZone: "Curva", note: "" }, "setups.validation.feedbackEmpty"],
    [{ ratings: { traction: 6 }, trackZone: "", note: "" }, "setups.validation.rating"],
    [{ ratings: { traction: 0 }, trackZone: "", note: "" }, "setups.validation.rating"],
    [{ ratings: { traction: 2.5 }, trackZone: "", note: "" }, "setups.validation.rating"],
    [{ ratings: { traction: 3 }, trackZone: "x".repeat(81), note: "" }, "setups.validation.tooLong"],
  ])("refuses %j", (input, message) => {
    const result = feedbackInputSchema.safeParse(input);
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe(message);
  });

  it("rejects an unknown scale", () => {
    expect(feedbackInputSchema.safeParse({ ratings: { speed: 3 }, trackZone: "", note: "x" }).success).toBe(false);
  });
});

describe("rows", () => {
  it("refuses a setup row with extra columns", () => {
    const row = {
      id,
      car_id: id,
      name: "A",
      notes: null,
      current_version: 1,
      created_at: "t",
      updated_at: "t",
      created_by: null,
      local_rev: 0,
    };
    expect(setupRowSchema.safeParse(row).success).toBe(true);
    expect(setupRowSchema.safeParse({ ...row, extra: 1 }).success).toBe(false);
  });

  it("parses a version row and its data", () => {
    const row = setupVersionRowSchema.parse({
      setup_id: id,
      version: 2,
      data: '{"diffs":{"front_cst":500}}',
      schema_version: 1,
      based_on_setup_id: id,
      based_on_version: 1,
      change_summary: null,
      created_at: "t",
      created_by: null,
    });
    expect(versionFromRow(row)).toMatchObject({
      version: 2,
      data: { diffs: { front_cst: 500 } },
      basedOn: { setupId: id, version: 1 },
    });
  });

  it.each(["[]", "null", "3", "not json"])("treats stored data %s as corrupt", (raw) => {
    expect(() => parseStoredData(raw)).toThrow();
  });

  it("maps feedback columns back to scales", () => {
    const row = {
      id,
      setup_id: id,
      version: 1,
      author_id: id,
      steering_entry: 2,
      steering_mid: null,
      steering_exit: null,
      traction: 5,
      jump_stability: null,
      braking: null,
      engine_low_end: null,
      engine_top_end: 1,
      track_zone: null,
      note: "x",
      created_at: "t",
    };
    expect(feedbackFromRow(row).ratings).toEqual({ steeringEntry: 2, traction: 5, engineTopEnd: 1 });
  });
});

describe("roles", () => {
  it.each([
    ["owner", true, true, true],
    ["mechanic", true, true, true],
    ["pilot", true, false, true],
  ] as const)("%s", (role, read, write, feedback) => {
    expect([canReadSetups(role), canWriteSetups(role), canGiveFeedback(role)]).toEqual([read, write, feedback]);
  });
});

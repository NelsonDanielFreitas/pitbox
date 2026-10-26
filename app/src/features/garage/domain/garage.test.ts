import { describe, expect, it } from "vitest";

import { formatQuantity, centiToInput, parseDecimalToCenti } from "./format";
import { canReadGarage, canWriteGarage } from "./roles";
import {
  carFormSchema,
  engineFormSchema,
  partFormSchema,
  partUpdateFormSchema,
  tyreFormSchema,
} from "./schemas";

const validCar = {
  make: "Mugen",
  model: "MBX8",
  nickname: "",
  transponderNumber: "1234567",
  modelYear: "2024",
  serialNumber: "",
  notes: "",
  status: "active" as const,
};

describe("garage schemas", () => {
  it("normalises optional fields and decimal values", () => {
    const car = carFormSchema.parse(validCar);
    expect(car.nickname).toBeNull();
    expect(car.modelYear).toBe(2024);

    const part = partFormSchema.parse({
      kind: "fuel",
      brand: "OS",
      reference: "",
      description: "Combustível",
      unit: "l",
      minStock: "1,5",
      initialQty: "2",
      hasServiceLife: false,
    });
    expect(part.minStock).toBe(150);
    expect(part.initialQty).toBe(200);
  });

  it("rejects invalid years, long text and whole-unit fractions", () => {
    expect(carFormSchema.safeParse({ ...validCar, modelYear: "1800" }).success).toBe(false);
    expect(carFormSchema.safeParse({ ...validCar, make: "x".repeat(81) }).success).toBe(false);
    expect(
      partFormSchema.safeParse({
        kind: "glow_plug",
        brand: "",
        reference: "",
        description: "Vela",
        unit: "un",
        minStock: "1,5",
        initialQty: "1",
        hasServiceLife: false,
      }).success,
    ).toBe(false);
    expect(
      partUpdateFormSchema.safeParse({
        kind: "glow_plug",
        brand: "",
        reference: "",
        description: "Vela",
        unit: "un",
        minStock: "2",
        hasServiceLife: false,
      }).success,
    ).toBe(true);
  });

  it("accepts valid dates and rejects impossible ones", () => {
    const valid = engineFormSchema.parse({
      make: "Novarossi",
      model: "Plus",
      serialNumber: "",
      purchasedOn: "2026-02-28",
      status: "active",
      notes: "",
    });
    expect(valid.purchasedOn).toBe("2026-02-28");
    expect(
      engineFormSchema.safeParse({
        make: "Novarossi",
        model: "Plus",
        serialNumber: "",
        purchasedOn: "2026-02-31",
        status: "active",
        notes: "",
      }).success,
    ).toBe(false);
  });

  it("uppercases tyre labels", () => {
    const tyre = tyreFormSchema.parse({
      label: " j4 ",
      make: "Pro-Line",
      model: "Badlands",
      compound: "",
      insertType: "",
      gluedOn: "2026-10-26",
      status: "new",
      wearNotes: "",
    });
    expect(tyre.label).toBe("J4");
  });

  it("keeps the decimal input conversion exact", () => {
    expect(parseDecimalToCenti("1,25")).toBe(125);
    expect(parseDecimalToCenti("10.5")).toBe(1050);
    expect(centiToInput(125, "l")).toBe("1,25");
    expect(centiToInput(200, "un")).toBe("2");
    expect(formatQuantity(150, "l", "pt-PT")).toBe("1,5 L");
  });
});

describe("garage roles", () => {
  it("allows pilots to read and only mechanics/owners to write", () => {
    expect(canReadGarage("pilot")).toBe(true);
    expect(canWriteGarage("pilot")).toBe(false);
    expect(canWriteGarage("mechanic")).toBe(true);
    expect(canWriteGarage("owner")).toBe(true);
    expect(canReadGarage("guest")).toBe(false);
    expect(canReadGarage("unknown")).toBe(false);
  });
});

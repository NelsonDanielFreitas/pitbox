
export type FieldKind = "int" | "number" | "string" | "bool" | "uuid";

export interface FieldDef {
  /** Dotted path inside the setup data, e.g. `shocks.front.oil_cst`. */
  path: string;
  /** Key inside its group, e.g. `oil_cst`. */
  key: string;
  kind: FieldKind;
  min?: number;
  max?: number;
  maxLen?: number;
  /** Unit shown next to the value; absent for text, flags and ids. */
  unit?: string;
}

export interface GroupDef {
  /** Dotted path of the group, e.g. `shocks.front`. */
  id: string;
  fields: readonly FieldDef[];
}

function group(id: string, fields: readonly Omit<FieldDef, "path">[]): GroupDef {
  return { id, fields: fields.map((f) => ({ ...f, path: `${id}.${f.key}` })) };
}

const int = (key: string, unit: string | undefined, min: number, max: number): Omit<FieldDef, "path"> =>
  ({ key, kind: "int", min, max, ...(unit ? { unit } : {}) });
const num = (key: string, unit: string, min: number, max: number): Omit<FieldDef, "path"> =>
  ({ key, kind: "number", min, max, unit });
const text = (key: string, maxLen: number): Omit<FieldDef, "path"> => ({ key, kind: "string", maxLen });
const flag = (key: string): Omit<FieldDef, "path"> => ({ key, kind: "bool" });
const uuid = (key: string): Omit<FieldDef, "path"> => ({ key, kind: "uuid" });

/**
 * The single field table of setup schema v1. It mirrors
 * `backend/internal/setup/domain/schema_v1.json`; `catalogue.test.ts` fails when they drift.
 * The server is the source of truth (it wins over the functional spec).
 */
export const setupGroups: readonly GroupDef[] = [
  group("diffs", [
    int("front_cst", "cSt", 100, 1000000),
    int("center_cst", "cSt", 100, 1000000),
    int("rear_cst", "cSt", 100, 1000000),
    num("front_ml", "ml", 0, 1000),
    num("center_ml", "ml", 0, 1000),
    num("rear_ml", "ml", 0, 1000),
    text("fill_notes", 500),
  ]),
  group("shocks.front", [
    int("oil_cst", "cSt", 100, 2000),
    int("piston_holes", undefined, 1, 10),
    num("piston_diameter_mm", "mm", 0.1, 5),
    text("spring_brand", 40),
    text("spring_color", 40),
    num("spring_rate_gf_mm", "gf/mm", 0, 200),
    num("limiters_mm", "mm", 0, 200),
    num("length_mm", "mm", 0, 200),
    text("tower_position", 40),
    text("arm_position", 40),
    text("membrane_type", 40),
  ]),
  group("shocks.rear", [
    int("oil_cst", "cSt", 100, 2000),
    int("piston_holes", undefined, 1, 10),
    num("piston_diameter_mm", "mm", 0.1, 5),
    text("spring_brand", 40),
    text("spring_color", 40),
    num("spring_rate_gf_mm", "gf/mm", 0, 200),
    num("limiters_mm", "mm", 0, 200),
    num("length_mm", "mm", 0, 200),
    text("tower_position", 40),
    text("arm_position", 40),
    text("membrane_type", 40),
  ]),
  group("geometry", [
    num("front_camber_deg", "°", -5, 2),
    num("rear_camber_deg", "°", -5, 2),
    num("front_toe_deg", "°", -5, 5),
    num("rear_toe_deg", "°", -5, 5),
    num("caster_deg", "°", 0, 30),
    num("ride_height_mm", "mm", 0, 100),
    num("front_droop_mm", "mm", 0, 50),
    num("rear_droop_mm", "mm", 0, 50),
    text("ackermann", 40),
    text("rear_hub_position", 40),
  ]),
  group("bars", [
    num("front_mm", "mm", 0, 10),
    flag("front_removed"),
    num("rear_mm", "mm", 0, 10),
    flag("rear_removed"),
  ]),
  group("aero", [
    text("wing_model", 80),
    num("wing_angle_deg", "°", -10, 30),
    num("gurney_mm", "mm", 0, 20),
    text("body", 80),
  ]),
  group("transmission", [
    int("clutch_bell_teeth", "teeth", 8, 60),
    int("spur_teeth", "teeth", 8, 60),
    text("clutch_shoe_material", 40),
    text("clutch_spring", 40),
    num("clutch_clearance_mm", "mm", 0, 10),
  ]),
  group("engine", [
    uuid("engine_id"),
    num("high_needle_turns", "turns", 0, 20),
    num("low_needle_turns", "turns", 0, 20),
    text("glow_plug_brand", 80),
    text("glow_plug_temp", 40),
    text("fuel_brand", 80),
    num("fuel_nitro_pct", "%", 0, 50),
    text("exhaust", 80),
    text("manifold", 80),
    text("air_filter", 80),
    num("target_head_temp_c", "°C", 0, 200),
  ]),
  group("tyres", [
    uuid("tyre_set_id"),
    num("glue_pressure_kpa", "kPa", 0, 500),
    text("prep_additive", 80),
    num("prep_time_min", "min", 0, 120),
  ]),
  group("weight", [
    num("total_g", "g", 0, 10000),
    num("ballast_g", "g", 0, 10000),
    text("ballast_position", 80),
  ]),
  group("radio", [
    num("steering_epa_pct", "%", 0, 150),
    num("steering_expo_pct", "%", -100, 100),
    num("steering_dual_rate_pct", "%", 0, 150),
    num("throttle_epa_pct", "%", 0, 150),
    num("throttle_expo_pct", "%", -100, 100),
    num("throttle_dual_rate_pct", "%", 0, 150),
    num("brake_pct", "%", 0, 150),
  ]),
];

export const setupFields: readonly FieldDef[] = setupGroups.flatMap((g) => g.fields);

const byPath = new Map(setupFields.map((f) => [f.path, f]));

export function fieldByPath(path: string): FieldDef | undefined {
  return byPath.get(path);
}

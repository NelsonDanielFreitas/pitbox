/** Lowercases and strips accents so “vela” finds “Vélã”. */
export function normalizeSearch(value: string): string {
  return value.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().trim();
}

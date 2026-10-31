// Phone profile (ADR 0029): the machine's tailnet name, e.g. "pc.tail1234.ts.net".
// Same rule as scripts/tailnet.py (keep them in step): lowercase labels ending in .ts.net, no
// scheme, port or path. The dev server only accepts this one extra Host, never a wildcard.
const LABEL = "[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?";
const TAILNET_HOST = new RegExp(`^(?:${LABEL}\\.)+ts\\.net$`);

export function parseTailnetHost(raw: string | undefined): string | undefined {
  if (raw === undefined || raw === "") return undefined;
  if (raw.length > 253 || !TAILNET_HOST.test(raw)) {
    throw new Error(
      "PITBOX_TAILNET_HOST must be just the machine name in lowercase ending in .ts.net " +
        "(e.g. pc.tail1234.ts.net), without http://, port or path",
    );
  }
  return raw;
}

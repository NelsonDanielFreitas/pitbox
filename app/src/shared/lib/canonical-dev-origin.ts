export function canonicalDevOrigin(url: URL, isDev: boolean): string | null {
  if (!isDev || url.hostname !== "127.0.0.1") {
    return null;
  }
  const canonical = new URL(url.href);
  canonical.hostname = "localhost";
  return canonical.href;
}

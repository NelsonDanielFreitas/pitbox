/** SHA-256 of the user id, hex. Names the user's local database on every platform. */
export async function hashUserId(userId: string): Promise<string> {
  const bytes = new TextEncoder().encode(userId);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

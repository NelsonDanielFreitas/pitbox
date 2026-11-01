/**
 * Asks the browser not to evict the local database under storage pressure. Returns whether
 * storage is persistent. The answer is the browser's: a tab that is not installed is often refused.
 */
export async function requestPersistentStorage(): Promise<boolean> {
  try {
    if (!("storage" in navigator) || typeof navigator.storage.persist !== "function") return false;
    if (await navigator.storage.persisted()) return true;
    return await navigator.storage.persist();
  } catch {
    return false;
  }
}

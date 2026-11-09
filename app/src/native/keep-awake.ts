import { KeepAwake } from "@capacitor-community/keep-awake";
import { useEffect } from "react";

import { isNativePlatform } from "@/shared/lib/platform";

const nothingToRelease = (): Promise<void> => Promise.resolve();

/**
 * Keeps the screen on until the returned function is called. Where it cannot (web, or a phone
 * that does not support it) it resolves to a function that does nothing: the screen then behaves
 * as usual, which is a worse experience but never a failure.
 */
export async function keepScreenAwake(): Promise<() => Promise<void>> {
  if (!isNativePlatform()) return nothingToRelease;
  try {
    if (!(await KeepAwake.isSupported()).isSupported) return nothingToRelease;
    await KeepAwake.keepAwake();
  } catch {
    return nothingToRelease;
  }
  return async () => {
    try {
      await KeepAwake.allowSleep();
    } catch {
      // Nothing to do: the flag goes away with the activity.
    }
  };
}

/** The screen stays on while `active` is true (the final mode, F2-08). */
export function useKeepAwake(active: boolean): void {
  useEffect(() => {
    if (!active) return undefined;
    let cancelled = false;
    let release: (() => Promise<void>) | undefined;
    void keepScreenAwake().then((fn) => {
      // Unmounted while the plugin was still answering: give the screen back straight away.
      if (cancelled) void fn();
      else release = fn;
    });
    return () => {
      cancelled = true;
      if (release) void release();
    };
  }, [active]);
}

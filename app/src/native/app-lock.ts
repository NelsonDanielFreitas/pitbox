/** Spec §3.4: the app asks for the phone's biometrics again after 15 minutes in the background. */
export const LOCK_AFTER_MS = 15 * 60 * 1000;

export type LockState = "locked" | "unlocked";

/**
 * Decides when the app is locked. It knows nothing about screens or plugins: the clock comes in
 * and a state comes out, so every rule below is tested without a phone.
 *
 * It fails closed. It starts locked, because after the process dies the time spent away is lost;
 * and any elapsed time that is not a number in [0, limit) locks, including a clock that went
 * backwards. (Moving the clock back by hand can still stretch the wait; it takes the phone in the
 * attacker's hand and unlocking is what the biometrics are for, so it is accepted.)
 */
export class AppLockController {
  private state: LockState;
  private backgroundedAt: number | undefined;
  private readonly listeners = new Set<() => void>();

  constructor(
    private readonly now: () => number = () => Date.now(),
    private readonly lockAfterMs: number = LOCK_AFTER_MS,
    initial: LockState = "locked",
  ) {
    this.state = initial;
  }

  /** Stable snapshot, shaped for `useSyncExternalStore`. */
  getState = (): LockState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  goneToBackground(): void {
    // A second "background" event must not push the start of the countdown forward.
    this.backgroundedAt ??= this.now();
  }

  cameToForeground(): void {
    const since = this.backgroundedAt;
    this.backgroundedAt = undefined;
    if (since === undefined) return;
    const elapsed = this.now() - since;
    if (!(elapsed >= 0 && elapsed < this.lockAfterMs)) this.lock();
  }

  lock(): void {
    this.set("locked");
  }

  unlock(): void {
    this.backgroundedAt = undefined;
    this.set("unlocked");
  }

  private set(next: LockState): void {
    if (this.state === next) return;
    this.state = next;
    for (const listener of this.listeners) listener();
  }
}

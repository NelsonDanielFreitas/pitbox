export type PendingInvitation = { kind: "token"; token: string } | { kind: "invalid" };

let pendingInvitation: PendingInvitation | null = null;

const tokenPattern = /^[A-Za-z0-9_-]{43}$/;

export function captureInvitationTokenFromLocation(): void {
  if (window.location.pathname !== "/invite" || window.location.hash === "") return;

  const fragment = new URLSearchParams(window.location.hash.slice(1));
  const values = fragment.getAll("t");
  const token = values[0];
  const wellFormed = [...fragment.keys()].every((key) => key === "t") && values.length === 1;
  pendingInvitation = wellFormed && token && tokenPattern.test(token) ? { kind: "token", token } : { kind: "invalid" };

  // Remove the bearer token before the router, queries, or page components start.
  window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
}

export function invitationTokenFromLink(value: string, appOrigin: string): string | null {
  try {
    const url = new URL(value.trim());
    if (url.origin !== appOrigin || url.pathname !== "/invite" || url.search !== "") return null;
    const params = new URLSearchParams(url.hash.slice(1));
    if ([...params.keys()].some((key) => key !== "t") || params.getAll("t").length !== 1) return null;
    const token = params.get("t");
    return token && tokenPattern.test(token) ? token : null;
  } catch {
    return null;
  }
}

export function peekPendingInvitation(): PendingInvitation | null {
  return pendingInvitation;
}

export function clearPendingInvitation(): void {
  pendingInvitation = null;
}

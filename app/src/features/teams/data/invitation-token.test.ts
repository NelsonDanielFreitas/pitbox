import { describe, expect, it } from "vitest";

import { captureInvitationTokenFromLocation, clearPendingInvitation, invitationTokenFromLink, peekPendingInvitation } from "./invitation-token";

const token = "A".repeat(43);

describe("invitation token handling", () => {
  it("accepts only an invite link for this app origin", () => {
    expect(invitationTokenFromLink(`http://localhost:5173/invite#t=${token}`, "http://localhost:5173")).toBe(token);
    expect(invitationTokenFromLink(`http://evil.example/invite#t=${token}`, "http://localhost:5173")).toBeNull();
    expect(invitationTokenFromLink(`http://localhost:5173/invite?token=${token}`, "http://localhost:5173")).toBeNull();
    expect(invitationTokenFromLink(`http://localhost:5173/invite#t=${token}&t=${token}`, "http://localhost:5173")).toBeNull();
    expect(invitationTokenFromLink(`http://localhost:5173/invite/${token}`, "http://localhost:5173")).toBeNull();
  });

  it("captures the fragment and removes it from the current history entry", () => {
    window.history.replaceState(window.history.state, "", `/invite#t=${token}`);

    captureInvitationTokenFromLocation();

    expect(window.location.pathname).toBe("/invite");
    expect(window.location.hash).toBe("");
    expect(peekPendingInvitation()).toEqual({ kind: "token", token });
    clearPendingInvitation();
    expect(peekPendingInvitation()).toBeNull();
  });

  it("scrubs malformed invite fragments too", () => {
    window.history.replaceState(window.history.state, "", "/invite#t=bad&debug=1");

    captureInvitationTokenFromLocation();

    expect(window.location.hash).toBe("");
    expect(peekPendingInvitation()).toEqual({ kind: "invalid" });
  });
});

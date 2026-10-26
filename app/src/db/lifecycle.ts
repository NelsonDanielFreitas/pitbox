import { deleteUserDatabase, hashUserId, markLogout } from "./client";
import { announceLogout } from "./session-channel";

export async function clearLocalUserData(userId: string): Promise<void> {
  const userHash = await hashUserId(userId);
  await markLogout(userId);
  announceLogout(userHash);
  await deleteUserDatabase(userId);
}

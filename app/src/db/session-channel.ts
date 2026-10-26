const channelName = "pitbox-db";

interface LogoutMessage {
  type: "logout";
  userHash: string;
  sourceTab: string;
}

const tabId = crypto.randomUUID();

function channel(): BroadcastChannel | undefined {
  return typeof BroadcastChannel === "undefined" ? undefined : new BroadcastChannel(channelName);
}

export function announceLogout(userHash: string): void {
  const bus = channel();
  if (!bus) return;
  const message: LogoutMessage = { type: "logout", userHash, sourceTab: tabId };
  bus.postMessage(message);
  bus.close();
}

export function subscribeToLogout(onLogout: (userHash: string) => void): () => void {
  const bus = channel();
  if (!bus) return () => undefined;
  bus.onmessage = (event: MessageEvent<LogoutMessage>) => {
    if (event.data?.type === "logout" && event.data.sourceTab !== tabId) {
      onLogout(event.data.userHash);
    }
  };
  return () => bus.close();
}

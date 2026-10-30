import type { SetupRepository } from "@/features/setups/data/setup-repository";

export function emptySetupRepository(): SetupRepository {
  return {
    listSetups: () => Promise.resolve([]),
    getSetup: () => Promise.resolve(undefined),
    listVersions: () => Promise.resolve([]),
    getVersion: () => Promise.resolve(undefined),
    createSetup: () => Promise.reject(new Error("not implemented")),
    createVersion: () => Promise.reject(new Error("not implemented")),
    updateSetup: () => Promise.reject(new Error("not implemented")),
    addFeedback: () => Promise.reject(new Error("not implemented")),
    listFeedback: () => Promise.resolve([]),
  };
}

import type { GarageRepository } from "@/features/garage/data/garage-repository";

export function emptyGarageRepository(): GarageRepository {
  return {
    listCars: () => Promise.resolve([]),
    getCar: () => Promise.resolve(undefined),
    createCar: () => Promise.reject(new Error("not implemented")),
    updateCar: () => Promise.reject(new Error("not implemented")),
    listEngines: () => Promise.resolve([]),
    getEngine: () => Promise.resolve(undefined),
    createEngine: () => Promise.reject(new Error("not implemented")),
    updateEngine: () => Promise.reject(new Error("not implemented")),
    listEngineMounts: () => Promise.resolve([]),
    mountEngine: () => Promise.resolve(undefined),
    unmountEngine: () => Promise.resolve(undefined),
    moveEngine: () => Promise.resolve(undefined),
    swapCarEngine: () => Promise.resolve(undefined),
    listTyreSets: () => Promise.resolve([]),
    getTyreSet: () => Promise.resolve(undefined),
    createTyreSet: () => Promise.reject(new Error("not implemented")),
    updateTyreSet: () => Promise.reject(new Error("not implemented")),
    listParts: () => Promise.resolve([]),
    getPart: () => Promise.resolve(undefined),
    createPart: () => Promise.reject(new Error("not implemented")),
    updatePart: () => Promise.reject(new Error("not implemented")),
  };
}

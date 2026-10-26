declare module "wa-sqlite/dist/wa-sqlite.mjs" {
  interface SQLiteModuleOptions {
    wasmBinary?: ArrayBuffer | Uint8Array;
    locateFile?: (path: string) => string;
  }

  const factory: (options?: SQLiteModuleOptions) => Promise<unknown>;
  export default factory;
}

declare module "wa-sqlite/dist/wa-sqlite.wasm?url" {
  const url: string;
  export default url;
}

declare module "wa-sqlite/src/examples/OPFSCoopSyncVFS.js" {
  export class OPFSCoopSyncVFS {
    lastError: unknown;
    mxPathname: number;
    log?: (...args: unknown[]) => void;
    static create(name: string, module: unknown): Promise<OPFSCoopSyncVFS>;
    close(): void;
  }
}

declare module "wa-sqlite/src/examples/MemoryVFS.js" {
  export class MemoryVFS {
    static create(name: string, module: unknown): Promise<MemoryVFS>;
    close(): void;
  }
}

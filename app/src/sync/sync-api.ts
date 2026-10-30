import { createClient, type Client } from "@connectrpc/connect";
import { fromJson, toJson, type JsonObject, type JsonValue } from "@bufbuild/protobuf";
import { timestampFromDate, timestampDate, ValueSchema } from "@bufbuild/protobuf/wkt";

import {
  ChangeKind,
  ConflictResolution,
  OpStatus,
  SyncService,
} from "@/gen/pitbox/sync/v1/sync_service_pb";
import { createPitboxTransport } from "@/shared/api/transport";

import type { OpKindValue } from "./signing";

// The engine talks to this small interface, not to the generated client, so tests can
// replace the server and the wire types stay in one place.

export type PushStatus = "applied" | "merged" | "conflict" | "rejected" | "unknown";

export interface PushOp {
  opId: string;
  entity: string;
  entityId: string;
  kind: OpKindValue;
  fieldsJson: Uint8Array;
  baseVersion: number | null;
  clientTsMs: number;
  signature: Uint8Array;
}

export interface PushResult {
  opId: string;
  status: PushStatus;
  reason: string | null;
  version: number | null;
}

export interface PulledChange {
  entity: string;
  entityId: string;
  deleted: boolean;
  data: JsonObject | null;
}

export interface PullPage {
  changes: PulledChange[];
  nextCursor: string;
  hasMore: boolean;
  catalogVersion: string;
  catalogSnapshot: JsonObject | null;
}

export interface RemoteConflict {
  id: string;
  opId: string;
  entity: string;
  entityId: string;
  field: string;
  serverValue: JsonValue;
  clientValue: JsonValue;
  createdAt: string;
}

export type Resolution = "server" | "client" | "manual";

export interface SyncApi {
  registerDevice(input: {
    teamId: string;
    requestId: string;
    deviceName: string;
    platform: "web" | "android" | "ios";
    publicKey: Uint8Array;
    proof: Uint8Array;
  }): Promise<{ deviceId: string }>;
  issueOfflinePass(input: { teamId: string; deviceId: string }): Promise<{ token: string; expiresAt: Date }>;
  push(input: { teamId: string; deviceId: string; offlinePass: string; operations: PushOp[] }): Promise<PushResult[]>;
  pull(input: { teamId: string; cursor: string; limit: number; catalogVersion: string }): Promise<PullPage>;
  listConflicts(teamId: string): Promise<RemoteConflict[]>;
  resolveConflict(input: {
    teamId: string;
    requestId: string;
    conflictId: string;
    resolution: Resolution;
    value?: JsonValue;
  }): Promise<void>;
}

const statusOf: Record<OpStatus, PushStatus> = {
  [OpStatus.UNSPECIFIED]: "unknown",
  [OpStatus.APPLIED]: "applied",
  [OpStatus.MERGED]: "merged",
  [OpStatus.CONFLICT]: "conflict",
  [OpStatus.REJECTED]: "rejected",
};

const resolutionOf: Record<Resolution, ConflictResolution> = {
  server: ConflictResolution.SERVER,
  client: ConflictResolution.CLIENT,
  manual: ConflictResolution.MANUAL,
};

type Client_ = Pick<
  Client<typeof SyncService>,
  "registerDevice" | "issueOfflinePass" | "push" | "pull" | "listConflicts" | "resolveConflict"
>;

export class ConnectSyncApi implements SyncApi {
  constructor(private readonly client: Client_ = createClient(SyncService, createPitboxTransport())) {}

  async registerDevice(input: Parameters<SyncApi["registerDevice"]>[0]): Promise<{ deviceId: string }> {
    const response = await this.client.registerDevice(input);
    return { deviceId: response.deviceId };
  }

  async issueOfflinePass(input: { teamId: string; deviceId: string }) {
    const response = await this.client.issueOfflinePass(input);
    if (!response.expiresAt) throw new Error("offline pass without expiry");
    return { token: response.token, expiresAt: timestampDate(response.expiresAt) };
  }

  async push(input: Parameters<SyncApi["push"]>[0]): Promise<PushResult[]> {
    const response = await this.client.push({
      teamId: input.teamId,
      deviceId: input.deviceId,
      offlinePass: input.offlinePass,
      operations: input.operations.map((op) => ({
        opId: op.opId,
        entity: op.entity,
        entityId: op.entityId,
        kind: op.kind,
        fieldsJson: op.fieldsJson,
        baseVersion: op.baseVersion ?? undefined,
        clientTs: timestampFromDate(new Date(op.clientTsMs)),
        deviceSignature: op.signature,
      })),
    });
    return response.results.map((r) => ({
      opId: r.opId,
      status: statusOf[r.status] ?? "unknown",
      reason: r.reason ?? null,
      version: r.version ?? null,
    }));
  }

  async pull(input: Parameters<SyncApi["pull"]>[0]): Promise<PullPage> {
    const response = await this.client.pull(input);
    return {
      changes: response.changes.map((c) => ({
        entity: c.entity,
        entityId: c.entityId,
        deleted: c.kind === ChangeKind.DELETE,
        data: c.data ?? null,
      })),
      nextCursor: response.nextCursor,
      hasMore: response.hasMore,
      catalogVersion: response.catalogVersion,
      catalogSnapshot: response.catalogSnapshot ?? null,
    };
  }

  async listConflicts(teamId: string): Promise<RemoteConflict[]> {
    const response = await this.client.listConflicts({ teamId });
    return response.conflicts.map((c) => ({
      id: c.id,
      opId: c.opId,
      entity: c.entity,
      entityId: c.entityId,
      field: c.field,
      serverValue: c.serverValue ? toJson(ValueSchema, c.serverValue) : null,
      clientValue: c.clientValue ? toJson(ValueSchema, c.clientValue) : null,
      createdAt: c.createdAt ? timestampDate(c.createdAt).toISOString() : new Date(0).toISOString(),
    }));
  }

  async resolveConflict(input: Parameters<SyncApi["resolveConflict"]>[0]): Promise<void> {
    await this.client.resolveConflict({
      teamId: input.teamId,
      requestId: input.requestId,
      conflictId: input.conflictId,
      resolution: resolutionOf[input.resolution],
      value: input.value === undefined ? undefined : fromJson(ValueSchema, input.value),
    });
  }
}

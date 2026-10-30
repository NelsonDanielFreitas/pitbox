import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";
import { z } from "zod";

import { query } from "@/db/client";
import { getEngine, getSyncSnapshot, subscribeSync } from "@/sync/runtime";
import type { SyncSnapshot } from "@/sync/engine";

export function useSyncStatus(): SyncSnapshot {
  return useSyncExternalStore(subscribeSync, getSyncSnapshot, getSyncSnapshot);
}

const jsonText = z.string().transform((text, ctx) => {
  try {
    return JSON.parse(text) as unknown;
  } catch {
    ctx.addIssue({ code: "custom", message: "invalid json" });
    return z.NEVER;
  }
});

const conflictRow = z.object({
  id: z.string(),
  entity: z.string(),
  entity_id: z.string(),
  field: z.string(),
  server_value: jsonText,
  client_value: jsonText,
  created_at: z.string(),
});

const failedRow = z.object({
  op_id: z.string(),
  entity: z.string(),
  entity_id: z.string(),
  reason: z.string().nullable(),
  created_at: z.string(),
});

export interface ConflictItem {
  id: string;
  entity: string;
  entityId: string;
  field: string;
  serverValue: unknown;
  clientValue: unknown;
  createdAt: string;
}

export interface FailedItem {
  opId: string;
  entity: string;
  entityId: string;
  reason: string | null;
  createdAt: string;
}

async function listConflicts(teamId: string): Promise<ConflictItem[]> {
  const rows = await query(
    `SELECT id, entity, entity_id, field, server_value, client_value, created_at
       FROM sync_conflict WHERE team_id = $teamId ORDER BY created_at, id`,
    { $teamId: teamId },
  );
  return rows.map((row) => {
    const r = conflictRow.parse(row);
    return {
      id: r.id,
      entity: r.entity,
      entityId: r.entity_id,
      field: r.field,
      serverValue: r.server_value,
      clientValue: r.client_value,
      createdAt: r.created_at,
    };
  });
}

async function listFailed(teamId: string): Promise<FailedItem[]> {
  const rows = await query(
    `SELECT op_id, entity, entity_id, reason, created_at
       FROM sync_outbox WHERE team_id = $teamId AND state = 'rejected' ORDER BY seq`,
    { $teamId: teamId },
  );
  return rows.map((row) => {
    const r = failedRow.parse(row);
    return { opId: r.op_id, entity: r.entity, entityId: r.entity_id, reason: r.reason, createdAt: r.created_at };
  });
}

/** Reads the local copy only; the engine keeps it current. Re-reads when the counts change. */
export function useSyncIssues(teamId: string) {
  const status = useSyncStatus();
  const key = [status.conflicts, status.failed] as const;
  const conflicts = useQuery({ queryKey: ["sync", "conflicts", teamId, ...key], queryFn: () => listConflicts(teamId) });
  const failed = useQuery({ queryKey: ["sync", "failed", teamId, ...key], queryFn: () => listFailed(teamId) });
  return { conflicts, failed };
}

export function useResolveConflict(teamId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { conflictId: string; resolution: "server" | "client" }) => {
      const engine = getEngine();
      if (!engine) throw new Error("sync is not running");
      await engine.resolveConflict(teamId, input.conflictId, input.resolution);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: ["sync"] }),
  });
}

export function useDismissFailed() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (opId: string) => {
      await getEngine()?.dismissFailed(opId);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: ["sync"] }),
  });
}

export function useReregisterDevice(teamId: string) {
  return useMutation({
    mutationFn: async () => {
      await getEngine()?.reregisterDevice(teamId);
    },
  });
}

export function useSyncNow() {
  return () => getEngine()?.requestSync(true);
}

import { z } from "zod";

export const teamRoleSchema = z.enum(["owner", "mechanic", "pilot", "guest", "unknown"]);

export const localTeamRowSchema = z
  .object({
    id: z.uuid(),
    name: z.string().min(1),
    category: z.string().min(1),
    club: z.string().nullable(),
    country_code: z.string().min(2).max(2),
    role: teamRoleSchema,
  })
  .strict();

export type LocalTeamRow = z.infer<typeof localTeamRowSchema>;

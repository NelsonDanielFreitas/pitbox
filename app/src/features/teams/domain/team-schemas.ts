import { z } from "zod";

export const teamRoleSchema = z.enum(["owner", "mechanic", "pilot", "guest", "unknown"]);

export const assignableTeamRoleSchema = z.enum(["mechanic", "pilot", "guest"]);
export type AssignableTeamRole = z.infer<typeof assignableTeamRoleSchema>;

export const teamCategories = [
  "1/8 TT nitro",
  "1/8 Buggy nitro",
  "1/8 Buggy elétrico",
  "1/8 TT elétrico",
] as const;

export const createTeamFormSchema = z.object({
  name: z.string().trim().min(1, { message: "teams.validation.nameRequired" }).max(80, {
    message: "teams.validation.tooLong",
  }),
  category: z.union([z.literal(""), z.enum(teamCategories)]),
  club: z
    .string()
    .trim()
    .max(80, { message: "teams.validation.tooLong" })
    .transform((value) => (value === "" ? undefined : value)),
});

export type CreateTeamFormValues = z.input<typeof createTeamFormSchema>;
export type CreateTeamFormOutput = z.output<typeof createTeamFormSchema>;

export const memberSchema = z
  .object({
    userId: z.uuid(),
    displayName: z.string().min(1).max(80),
    role: teamRoleSchema,
    joinedAt: z.iso.datetime({ offset: true }),
    version: z.number().int().positive(),
  })
  .strict();

export type TeamMember = z.infer<typeof memberSchema>;

export const invitationSchema = z
  .object({
    id: z.uuid(),
    email: z.email().optional(),
    role: assignableTeamRoleSchema,
    expiresAt: z.iso.datetime({ offset: true }),
    createdAt: z.iso.datetime({ offset: true }),
  })
  .strict();

export type TeamInvitation = z.infer<typeof invitationSchema>;

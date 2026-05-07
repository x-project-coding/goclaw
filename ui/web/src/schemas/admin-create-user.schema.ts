import { z } from "zod";

// Mirror backend auth.ValidatePasswordComplexity.
const passwordPolicy = z
  .string()
  .min(12, "min12")
  .refine((v) => /[A-Za-z]/.test(v), "needsLetter")
  .refine((v) => /[0-9]/.test(v), "needsDigit")
  .refine((v) => /[^A-Za-z0-9]/.test(v), "needsSymbol");

// Admin can create member or viewer; admin/root creation is root-only at the BE.
export const adminCreateUserRoleEnum = z.enum(["member", "viewer"]);

export const adminCreateUserSchema = z.object({
  email: z.string().email("invalidEmail"),
  display_name: z.string().min(1, "required").max(64, "displayNameTooLong"),
  password: passwordPolicy,
  role: adminCreateUserRoleEnum,
});

export type AdminCreateUserData = z.infer<typeof adminCreateUserSchema>;
export type AdminCreateUserRole = z.infer<typeof adminCreateUserRoleEnum>;

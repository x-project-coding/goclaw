import { z } from "zod";

// Backend (auth_password.go) enforces ≥12 chars + ≥1 letter + ≥1 digit + ≥1 symbol.
// We mirror it client-side for fast feedback; backend remains source of truth.
const passwordPolicy = z
  .string()
  .min(12, "min12")
  .refine((v) => /[A-Za-z]/.test(v), "needsLetter")
  .refine((v) => /[0-9]/.test(v), "needsDigit")
  .refine((v) => /[^A-Za-z0-9]/.test(v), "needsSymbol");

export const passwordLoginSchema = z.object({
  email: z.string().email("invalidEmail"),
  password: z.string().min(1, "required"),
});

export const bootstrapSchema = z.object({
  email: z.string().email("invalidEmail"),
  password: passwordPolicy,
  displayName: z.string().min(2, "displayNameTooShort").max(64, "displayNameTooLong"),
  // X-Bootstrap-Token printed at gateway startup. Required by backend (loopback + token gate).
  bootstrapToken: z.string().min(1, "required"),
});

export const profileUpdateSchema = z.object({
  displayName: z.string().min(2, "displayNameTooShort").max(64, "displayNameTooLong"),
});

export const passwordChangeSchema = z
  .object({
    currentPassword: z.string().min(1, "required"),
    newPassword: passwordPolicy,
  })
  .refine((d) => d.currentPassword !== d.newPassword, {
    message: "samePassword",
    path: ["newPassword"],
  });

export type PasswordLoginData = z.infer<typeof passwordLoginSchema>;
export type BootstrapData = z.infer<typeof bootstrapSchema>;
export type ProfileUpdateData = z.infer<typeof profileUpdateSchema>;
export type PasswordChangeData = z.infer<typeof passwordChangeSchema>;

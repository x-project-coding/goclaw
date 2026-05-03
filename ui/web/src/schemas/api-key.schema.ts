import { z } from "zod";

export const apiKeyCreateSchema = z.object({
  name: z.string().min(1, "Required").max(100),
  scopes: z.array(z.string()).min(1, "Select at least one scope"),
  expiry: z.string(),
});

export type ApiKeyCreateFormData = z.infer<typeof apiKeyCreateSchema>;

import { useState, useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ComboboxOption } from "@/components/ui/combobox";

/** User search result from channel_contacts. */
export interface UserPickerItem {
  id: string;
  uuid?: string;
  display_name?: string;
  username?: string;
  source: "contact";
  channel_type?: string;
  peer_kind?: string;
  merged_tenant_user_id?: string;
  role?: string;
}

/**
 * User picker hook — searches channel_contacts.
 * - Empty search: returns 30 most recent
 * - With search: debounced server-side ILIKE search
 */
export function useUserPicker(
  search: string,
  peerKind?: string,
  source?: "contact",
  valueMode?: "user_id" | "uuid",
) {
  const http = useHttp();
  const [debouncedSearch, setDebouncedSearch] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search), 150);
    return () => clearTimeout(timer);
  }, [search]);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.users.search({ search: debouncedSearch, peerKind, source, limit: 30 }),
    queryFn: async () => {
      const params: Record<string, string> = { limit: "30" };
      if (debouncedSearch) params.q = debouncedSearch;
      if (peerKind) params.peer_kind = peerKind;
      if (source) params.source = source;
      const res = await http.get<{ results: UserPickerItem[] }>("/v1/users/search", params);
      return res.results ?? [];
    },
    // Always enabled — empty search returns recent contacts
    staleTime: 60_000,
  });

  const results = data ?? [];

  /** Format results as ComboboxOptions with source badges.
   *  Committed value = uuid field when valueMode === "uuid" and uuid is present,
   *  otherwise user_id string. */
  const options: ComboboxOption[] = useMemo(() =>
    results.map((r) => {
      const parts: string[] = [];
      if (r.display_name) parts.push(r.display_name);
      if (r.username) parts.push(`@${r.username}`);
      parts.push(`(${r.id})`);
      if (r.channel_type) parts.push(`[${r.channel_type}]`);
      const value = valueMode === "uuid" && r.uuid ? r.uuid : r.id;
      return { value, label: parts.join(" ") };
    }),
    [results, valueMode],
  );

  return { results, options, loading: isLoading };
}

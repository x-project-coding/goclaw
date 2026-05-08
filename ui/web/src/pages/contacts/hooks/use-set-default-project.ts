import { useCallback } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import { userFriendlyError } from "@/lib/error-utils";
import type { ChannelContact } from "@/types/contact";

interface SetDefaultProjectInput {
  channelContactId: string;
  projectId: string | null;
}

interface SetDefaultProjectResponse {
  ok: boolean;
  channelContactId: string;
  projectId: string;
}

/**
 * Mutation hook for `channels.contacts.set_default_project`.
 *
 * BE payload uses camelCase (channelContactId, projectId). projectId="" or omit
 * clears the binding. Optimistic update patches the cached contacts list so the
 * UI reflects the new picker value before the WS round-trip completes; on error
 * we roll back the cache and surface a toast.
 */
export function useSetDefaultProject() {
  const ws = useWs();
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: async ({ channelContactId, projectId }: SetDefaultProjectInput) =>
      ws.call<SetDefaultProjectResponse>(Methods.CHANNELS_CONTACTS_SET_DEFAULT_PROJECT, {
        channelContactId,
        projectId,
      }),
    onMutate: async ({ channelContactId, projectId }) => {
      await qc.cancelQueries({ queryKey: queryKeys.contacts.all });
      const snapshots: Array<[readonly unknown[], unknown]> = [];
      qc.getQueriesData<{ contacts: ChannelContact[]; total: number }>({
        queryKey: queryKeys.contacts.all,
      }).forEach(([key, data]) => {
        snapshots.push([key, data]);
        if (!data) return;
        qc.setQueryData(key, {
          ...data,
          contacts: data.contacts.map((c) =>
            c.id === channelContactId ? { ...c, default_project_id: projectId } : c,
          ),
        });
      });
      return { snapshots };
    },
    onError: (err, _vars, ctx) => {
      ctx?.snapshots.forEach(([key, data]) => qc.setQueryData(key, data));
      toast.error(
        i18next.t("contacts:defaultProject.toastErrorTitle"),
        userFriendlyError(err),
      );
    },
    onSuccess: () => {
      toast.success(i18next.t("contacts:defaultProject.toastSavedTitle"));
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: queryKeys.contacts.all });
    },
  });

  const setDefaultProject = useCallback(
    (channelContactId: string, projectId: string | null) =>
      mutation.mutateAsync({ channelContactId, projectId }),
    [mutation],
  );

  return { setDefaultProject, saving: mutation.isPending };
}

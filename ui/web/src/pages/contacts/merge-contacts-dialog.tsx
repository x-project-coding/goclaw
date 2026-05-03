import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Merge } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "@/stores/use-toast-store";
import type { ChannelContact } from "@/types/contact";
import { useContactMerge } from "./hooks/use-contact-merge";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";

interface MergeContactsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  selectedContacts: ChannelContact[];
  onSuccess: () => void;
}

export function MergeContactsDialog({
  open,
  onOpenChange,
  selectedContacts,
  onSuccess,
}: MergeContactsDialogProps) {
  const { t } = useTranslation("contacts");
  const { merge } = useContactMerge();

  const [selectedUserId, setSelectedUserId] = useState("");
  const [submitting, setSubmitting] = useState(false);

  // Reset form state when dialog opens
  useEffect(() => {
    if (open) {
      setSelectedUserId("");
    }
  }, [open]);

  const handleSubmit = async () => {
    if (!selectedUserId) return;
    const contactIds = selectedContacts.map((c) => c.id);
    setSubmitting(true);
    try {
      await merge({ contact_ids: contactIds, target_user_id: selectedUserId });
      toast.success(t("merge.dialogTitle"), t("merge.success"));
      onOpenChange(false);
      onSuccess();
    } catch (err) {
      toast.error(t("merge.dialogTitle"), err instanceof Error ? err.message : t("merge.error"));
    } finally {
      setSubmitting(false);
    }
  };

  const canSubmit = !!selectedUserId;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Merge className="h-4 w-4" />
            {t("merge.dialogTitle")}
          </DialogTitle>
          <DialogDescription>{t("merge.dialogDescription")}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <UserPickerCombobox
            value={selectedUserId}
            onChange={setSelectedUserId}
            placeholder={t("merge.selectUser")}
            source="contact"
          />

          {/* Selected contacts summary */}
          <div className="text-xs text-muted-foreground border-t pt-2">
            {t("selectedCount", { count: selectedContacts.length })}
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("merge.cancel", { defaultValue: "Cancel" })}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit || submitting}>
            {t("merge.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

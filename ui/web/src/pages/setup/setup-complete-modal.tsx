import { CheckCircle2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

interface SetupCompleteModalProps {
  open: boolean;
  onGoToOverview: () => void;
}

export function SetupCompleteModal({ open, onGoToOverview }: SetupCompleteModalProps) {
  const { t } = useTranslation("setup");

  return (
    <Dialog open={open}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="mx-auto mb-2 flex h-14 w-14 items-center justify-center rounded-full bg-primary/10 text-primary">
            <CheckCircle2 className="h-8 w-8" />
          </div>
          <DialogTitle className="text-center">{t("complete.title")}</DialogTitle>
          <DialogDescription className="text-center">
            {t("complete.subtitle")}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter className="sm:justify-center">
          <Button onClick={onGoToOverview} className="w-full sm:w-auto">
            {t("complete.cta")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

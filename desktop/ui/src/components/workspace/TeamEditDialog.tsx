import { TeamAgentsEditor } from "@/components/setup/TeamAgentsEditor";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";

interface TeamEditDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}

export function TeamEditDialog({ open, onOpenChange, onSaved }: TeamEditDialogProps) {
  const { t } = useI18n();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("setup.team.editTitle")}</DialogTitle>
          <DialogDescription>{t("setup.team.editDescription")}</DialogDescription>
        </DialogHeader>
        <TeamAgentsEditor
          startFrom="current"
          confirmLabel={t("setup.team.save")}
          onSaved={() => {
            onOpenChange(false);
            onSaved();
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

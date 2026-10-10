import { TeamAgentsEditor } from "@/components/setup/TeamAgentsEditor";
import { useI18n } from "@/hooks/useI18n";
import { writeFirstRunTeamDone } from "@/lib/firstRun";

/**
 * The first-run team step: a template preselects the catalog agents the user
 * starts with, and every agent can be toggled before the choice is written.
 * The same editor reopens later from the sidebar's "Edit team".
 */
export function TeamTemplateStep({ onDone }: { onDone: () => void }) {
  const { t } = useI18n();
  return (
    <TeamAgentsEditor
      startFrom="template"
      confirmLabel={t("setup.team.confirm")}
      onSaved={() => {
        writeFirstRunTeamDone();
        onDone();
      }}
    />
  );
}

import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

/** Nothing can be written or asked without an agent to run the models on; say where to connect one. */
export function AudienceRuntimeNotice() {
  const { t } = useI18n();
  return (
    <Notice variant="warning" title={t("audienceHub.runtime.title")}>
      <p>{t("audienceHub.runtime.body")}</p>
      <Button asChild size="sm" variant="outline" className="mt-3">
        <Link to="/settings/llm">{t("audienceHub.runtime.open")}</Link>
      </Button>
    </Notice>
  );
}

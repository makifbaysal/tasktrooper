import { Download, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import type { AudienceReport, StudyKind } from "@/api";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { buildStudyReportHtml, downloadHtml, openHtml, reportFilename } from "@/lib/studyReportHtml";

interface ExportReportButtonProps {
  report: AudienceReport;
  title: string;
  audienceName?: string;
  kind?: StudyKind;
}

export function ExportReportButton({ report, title, audienceName, kind = report.kind ?? "user_interviews" }: ExportReportButtonProps) {
  const { t, lang } = useI18n();

  const run = (action: (filename: string, html: string) => void, notify: boolean) => {
    try {
      const html = buildStudyReportHtml({ report, title, audienceName, kind, lang, t });
      action(reportFilename(title, kind), html);
      if (notify) toast.success(t("audienceReportKinds.export.done"));
    } catch {
      toast.error(t("audienceReportKinds.export.failed"));
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="outline" onClick={() => run(downloadHtml, true)}>
        <Download />
        {t("audienceReportKinds.export.html")}
      </Button>
      <Button variant="ghost" onClick={() => run(openHtml, false)}>
        <ExternalLink />
        {t("audienceReportKinds.export.open")}
      </Button>
    </div>
  );
}

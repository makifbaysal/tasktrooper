import { Check, ChevronDown, Copy, FileCode2, RefreshCw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import type { DesignSystemFile } from "@/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useDesignSystemFiles } from "@/hooks/useDesignSystem";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

interface DesignSystemFilesCardProps {
  repositoryId: string;
}

/**
 * Organism: the files the repository keeps its design system in (DESIGN.md,
 * the token tree, CSS variables, the inventory), rendered by the server from
 * the effective version. Fetched only once the section is opened.
 */
export function DesignSystemFilesCard({ repositoryId }: DesignSystemFilesCardProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const { files, loading, error, reload } = useDesignSystemFiles(repositoryId, open);

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-start justify-between gap-3 space-y-0 pb-3">
        <div className="min-w-0 space-y-1.5">
          <CardTitle className="text-heading">{t("designSystem.files.title")}</CardTitle>
          <CardDescription>{t("designSystem.files.description")}</CardDescription>
        </div>
        <div className="flex gap-2">
          {open && files !== null && (
            <Button
              size="icon"
              variant="ghost"
              onClick={() => void reload()}
              disabled={loading}
              aria-label={t("common.refresh")}
              title={t("common.refresh")}
            >
              <RefreshCw className={cn(loading && "animate-spin")} />
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
            <ChevronDown className={cn("transition-transform", open && "rotate-180")} />
            {open ? t("designSystem.files.hide") : t("designSystem.files.show")}
          </Button>
        </div>
      </CardHeader>
      {open && (
        <CardContent className="space-y-3">
          {error ? (
            <Notice variant="error" title={t("designSystem.files.loadFailed")}>
              <p>{error}</p>
              <Button size="sm" variant="outline" className="mt-2" onClick={() => void reload()}>
                {t("designSystem.files.retry")}
              </Button>
            </Notice>
          ) : files === null ? (
            <div className="space-y-2">
              <Skeleton className="h-6 w-48" />
              <Skeleton className="h-40 w-full" />
            </div>
          ) : files.length === 0 ? (
            <p className="text-body text-muted-foreground">{t("designSystem.files.empty")}</p>
          ) : (
            files.map((file) => <DesignSystemFileBlock key={file.path} file={file} />)
          )}
        </CardContent>
      )}
    </Card>
  );
}

function DesignSystemFileBlock({ file }: { file: DesignSystemFile }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    if (await copyText(file.content)) {
      setCopied(true);
      toast.success(t("designSystem.files.copied", { path: file.path }));
      window.setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t("designSystem.files.copyFailed", { path: file.path }));
    }
  };

  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <div className="flex items-center gap-2 border-b border-border bg-muted/40 px-3 py-1.5">
        <FileCode2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="min-w-0 flex-1 truncate font-mono text-caption">{file.path}</span>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => void copy()}
          aria-label={t("designSystem.files.copyLabel", { path: file.path })}
        >
          {copied ? <Check /> : <Copy />}
          {t("designSystem.files.copy")}
        </Button>
      </div>
      {file.content ? (
        <pre className="max-h-80 overflow-auto whitespace-pre p-3 font-mono text-micro leading-relaxed">{file.content}</pre>
      ) : (
        <p className="p-3 text-caption text-muted-foreground">{t("designSystem.files.emptyFile")}</p>
      )}
    </div>
  );
}

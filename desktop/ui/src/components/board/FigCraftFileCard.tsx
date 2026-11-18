import { ExternalLink, FolderOpen, PenTool } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type DesignFileManifest, type DesignFileScope } from "@/api";
import { ImageLightbox } from "@/components/attachments/ImageLightbox";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

interface FigCraftFileCardProps {
  scope: DesignFileScope;
  /** The task's, project's or repository's id. */
  id: string;
  /** False while the surrounding view is closed: nothing is fetched. */
  active?: boolean;
  /** Only the header and the page list; a page opens full size. For pages whose body is another view. */
  compact?: boolean;
  className?: string;
}

/**
 * Organism: a design drawn in FigCraft — its pages rendered here, and the file
 * opened in the FigCraft app for every layer. Renders nothing until the file
 * exists, so it can sit on every design surface.
 */
export function FigCraftFileCard({ scope, id, active = true, compact = false, className }: FigCraftFileCardProps) {
  const { t } = useI18n();
  const [manifest, setManifest] = useState<DesignFileManifest | null>(null);
  const [pageId, setPageId] = useState<number | null>(null);
  const [zoomPageId, setZoomPageId] = useState<number | null>(null);

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    api
      .getDesignFile(scope, id)
      .then((m) => {
        if (cancelled) return;
        setManifest(m);
        setPageId((current) => (current !== null && m.pages.some((p) => p.id === current) ? current : (m.pages[0]?.id ?? null)));
      })
      .catch(() => {
        if (!cancelled) setManifest(null);
      });
    return () => {
      cancelled = true;
    };
  }, [scope, id, active]);

  if (!manifest?.exists) return null;

  const open = () =>
    api.openDesignFile(scope, id).catch((e) => toast.error(e instanceof Error ? e.message : t("designSystem.figcraft.openFailed")));
  const reveal = () => api.revealDesignFile(scope, id).catch((e) => toast.error(e instanceof Error ? e.message : String(e)));
  const page = manifest.pages.find((p) => p.id === pageId) ?? null;

  return (
    <Card className={className}>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0 pb-3">
        <div className="flex min-w-0 items-center gap-2">
          <PenTool className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
          <div className="min-w-0">
            <div className="truncate text-body font-medium">{manifest.name || t("designSystem.figcraft.title")}</div>
            <div className="truncate text-caption text-muted-foreground" title={manifest.path}>
              {manifest.path}
            </div>
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button variant="outline" size="sm" onClick={reveal}>
            <FolderOpen />
            {t("designSystem.figcraft.reveal")}
          </Button>
          <Button size="sm" onClick={open}>
            <ExternalLink />
            {t("designSystem.figcraft.open")}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {!manifest.installed ? (
          <Notice variant="info" title={t("designSystem.figcraft.notInstalled")} />
        ) : manifest.error ? (
          <Notice variant="error" title={manifest.error} />
        ) : (
          <>
            {(compact || manifest.pages.length > 1) && (
              <div className="flex flex-wrap gap-1.5" role="tablist" aria-label={t("designSystem.figcraft.page")}>
                {manifest.pages.map((p) => (
                  <Button
                    key={p.id}
                    role="tab"
                    size="sm"
                    variant={!compact && p.id === pageId ? "default" : "outline"}
                    aria-selected={!compact && p.id === pageId}
                    onClick={() => (compact ? setZoomPageId(p.id) : setPageId(p.id))}
                  >
                    {p.name}
                    <span className="opacity-70">{p.frames.length}</span>
                  </Button>
                ))}
              </div>
            )}
            {compact && zoomPageId !== null && (
              <PagePreview
                key={`zoom-${zoomPageId}-${manifest.modified ?? 0}`}
                scope={scope}
                id={id}
                pageId={zoomPageId}
                pageName={manifest.pages.find((p) => p.id === zoomPageId)?.name ?? ""}
                frameCount={manifest.pages.find((p) => p.id === zoomPageId)?.frames.length ?? 0}
                modified={manifest.modified}
                lightboxOnly
                onClose={() => setZoomPageId(null)}
              />
            )}
            {!compact && page && (
              <PagePreview
                key={`${page.id}-${manifest.modified ?? 0}`}
                scope={scope}
                id={id}
                pageId={page.id}
                pageName={page.name}
                frameCount={page.frames.length}
                modified={manifest.modified}
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function PagePreview({
  scope,
  id,
  pageId,
  pageName,
  frameCount,
  modified,
  lightboxOnly = false,
  onClose,
}: {
  lightboxOnly?: boolean;
  onClose?: () => void;
  scope: DesignFileScope;
  id: string;
  pageId: number;
  pageName: string;
  frameCount: number;
  modified?: number;
}) {
  const { t } = useI18n();
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [zoomed, setZoomed] = useState(lightboxOnly);
  const zoom = (open: boolean) => {
    setZoomed(open);
    if (!open) onClose?.();
  };

  useEffect(() => {
    let objectUrl: string | null = null;
    let cancelled = false;
    api
      .fetchDesignFilePreview(scope, id, pageId, modified)
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [scope, id, pageId, modified]);

  if (lightboxOnly) {
    if (failed) return <Notice variant="warning" title={t("designSystem.figcraft.renderFailed")} />;
    if (!url) return <p className="text-caption text-muted-foreground">{t("designSystem.figcraft.loading")}</p>;
    return <ImageLightbox src={url} title={pageName} filename={`${pageName}.png`} open={zoomed} onOpenChange={zoom} />;
  }
  if (failed) return <Notice variant="warning" title={t("designSystem.figcraft.renderFailed")} />;
  if (!url) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-64 w-full" />
        <p className="text-caption text-muted-foreground">{t("designSystem.figcraft.loading")}</p>
      </div>
    );
  }
  return (
    <>
      <button
        type="button"
        onClick={() => setZoomed(true)}
        className="block max-h-[560px] w-full overflow-auto rounded-lg border border-border bg-surface-sunken"
        title={t("designSystem.figcraft.zoomHint")}
      >
        <img src={url} alt={pageName} className="mx-auto w-full max-w-none object-contain object-top" />
      </button>
      <p className="text-caption text-muted-foreground">
        {pageName} · {t("designSystem.figcraft.frames", { count: frameCount })} · {t("designSystem.figcraft.zoomHint")}
      </p>
      <ImageLightbox src={url} title={pageName} filename={`${pageName}.png`} open={zoomed} onOpenChange={zoom} />
    </>
  );
}

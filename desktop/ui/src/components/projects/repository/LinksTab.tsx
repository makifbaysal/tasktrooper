import { Plus, Trash2 } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { api, type ComponentLink, type ComponentRole, type LinkStatus, type ResourceKind, type RepositoryModel } from "@/api";
import { AddLinkDialog } from "@/components/projects/repository/AddLinkDialog";
import { ComponentPickerDialog } from "@/components/projects/repository/ComponentPickerDialog";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import { ExternalResourceDialog } from "@/components/projects/repository/ExternalResourceDialog";
import { EvidenceList } from "@/components/projects/model/EvidenceList";
import { ConfidenceBadge } from "@/components/projects/model/ConfidenceBadge";
import { LinkTargetLabel } from "@/components/projects/model/LinkTargetLabel";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useI18n } from "@/hooks/useI18n";
import { componentLabel, factValue, groupLinks } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface LinksTabProps {
  model: RepositoryModel;
  selectedComponentId: string | null;
  onSelectComponent: (id: string | null) => void;
  onReload: () => void;
}

const STATUS_VARIANT: Record<LinkStatus, NonNullable<BadgeProps["variant"]>> = {
  confirmed: "success",
  suggested: "warning",
  dismissed: "secondary",
};

function targetRole(link: ComponentLink, model: RepositoryModel): ComponentRole | undefined {
  if (!link.to_component_id) return undefined;
  const own = model.components.find((c) => c.id === link.to_component_id);
  if (own) return factValue(own.role);
  return model.linked_components.find((c) => c.id === link.to_component_id)?.role;
}

function targetResourceKind(link: ComponentLink, model: RepositoryModel): ResourceKind | undefined {
  if (!link.to_resource_id) return undefined;
  return model.resources.find((r) => r.id === link.to_resource_id)?.kind;
}

function incomingSourceLabel(link: ComponentLink, model: RepositoryModel): { label: string; role?: ComponentRole } {
  const own = model.components.find((c) => c.id === link.from_component_id);
  if (own) return { label: componentLabel(own, model.repository.name), role: factValue(own.role) };
  const linked = model.linked_components.find((c) => c.id === link.from_component_id);
  if (linked) return { label: `${linked.repository_name}/${linked.path === "." ? "" : linked.path}`, role: linked.role };
  return { label: link.hint ?? "?" };
}

export function LinksTab({ model, selectedComponentId, onSelectComponent, onReload }: LinksTabProps) {
  const { t } = useI18n();
  const [selectedLinkId, setSelectedLinkId] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);

  if (model.components.length === 0) return null;

  const outgoing = selectedComponentId ? groupLinks(model, selectedComponentId).outgoing : model.links;
  const incoming = selectedComponentId ? groupLinks(model, selectedComponentId).incoming : model.incoming_links;
  const selectedLink = [...model.links, ...model.incoming_links].find((l) => l.id === selectedLinkId) ?? null;

  const fromComponent = selectedComponentId ? model.components.find((c) => c.id === selectedComponentId) : undefined;

  return (
    <div className="flex gap-6">
      <ComponentRail
        components={model.components}
        selectedId={selectedComponentId}
        onSelect={onSelectComponent}
        allowAll
        renderTrailing={(c) => {
          const g = groupLinks(model, c.id);
          return `${t("repositoryPage.links.outgoingCount", { count: g.outgoing.length })} · ${t("repositoryPage.links.incomingCount", { count: g.incoming.length })}`;
        }}
      />
      <div className="min-w-0 flex-1 space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold">
            {t("repositoryPage.links.outgoing")} · {outgoing.length}
          </h3>
          {fromComponent && (
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              {t("repositoryPage.links.addLink")}
            </Button>
          )}
        </div>
        <Card className="divide-y divide-border p-0">
          {outgoing.length === 0 ? (
            <p className="p-4 text-body text-muted-foreground">{t("repositoryPage.links.empty")}</p>
          ) : (
            outgoing.map((link) => (
              <LinkRow
                key={link.id}
                link={link}
                selected={link.id === selectedLinkId}
                onSelect={() => setSelectedLinkId(link.id)}
                icon={
                  targetResourceKind(link, model) ? (
                    <ResourceKindIcon kind={targetResourceKind(link, model)!} />
                  ) : targetRole(link, model) ? (
                    <RoleBadge role={targetRole(link, model)!} />
                  ) : undefined
                }
                label={<LinkTargetLabel link={link} model={model} />}
              />
            ))
          )}
        </Card>

        <h3 className="font-semibold">
          {t("repositoryPage.links.incoming")} · {incoming.length}
        </h3>
        <Card className="divide-y divide-border p-0">
          {incoming.length === 0 ? (
            <p className="p-4 text-body text-muted-foreground">{t("repositoryPage.links.empty")}</p>
          ) : (
            incoming.map((link) => {
              const source = incomingSourceLabel(link, model);
              return (
                <LinkRow
                  key={link.id}
                  link={link}
                  selected={link.id === selectedLinkId}
                  onSelect={() => setSelectedLinkId(link.id)}
                  icon={source.role ? <RoleBadge role={source.role} /> : undefined}
                  label={<span className="font-medium">{source.label}</span>}
                />
              );
            })
          )}
        </Card>
      </div>

      {selectedLink && (
        <LinkDetailPanel key={selectedLink.id} link={selectedLink} model={model} onReload={onReload} />
      )}

      {fromComponent && (
        <AddLinkDialog
          open={addOpen}
          onOpenChange={setAddOpen}
          fromComponentId={fromComponent.id}
          fromLabel={componentLabel(fromComponent, model.repository.name)}
          onCreated={onReload}
        />
      )}
    </div>
  );
}

function LinkRow({
  link,
  selected,
  onSelect,
  icon,
  label,
}: {
  link: ComponentLink;
  selected: boolean;
  onSelect: () => void;
  icon?: ReactNode;
  label: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn("flex w-full flex-wrap items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/40", selected && "bg-muted")}
    >
      <span className="flex shrink-0 items-center gap-1.5">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="text-caption text-muted-foreground">{t(`projectModel.linkProtocols.${link.protocol}`)}</span>
      {(link.env_vars?.length ?? 0) > 0 && (
        <span className="font-mono text-micro text-muted-foreground">{link.env_vars!.join(", ")}</span>
      )}
      {link.auto_confirmed && <Badge variant="info">{t("repositoryPage.links.auto")}</Badge>}
      {link.missing && <Badge variant="warning">{t("repositoryPage.links.missing")}</Badge>}
      <Badge variant={STATUS_VARIANT[link.status]}>{t(`projectModel.linkStatuses.${link.status}`)}</Badge>
    </button>
  );
}

function LinkDetailPanel({ link, model, onReload }: { link: ComponentLink; model: RepositoryModel; onReload: () => void }) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [resourceOpen, setResourceOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
      onReload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="w-80 shrink-0 space-y-4">
      <Card className="space-y-3 p-4">
        <LinkTargetLabel link={link} model={model} className="text-body font-semibold" />
        <ConfidenceBadge confidence={link.confidence} />
        {link.reason && (
          <div>
            <p className="text-xs text-muted-foreground">{t("repositoryPage.links.reason")}</p>
            <p className="text-body">{link.reason}</p>
          </div>
        )}
        {(link.evidence?.length ?? 0) > 0 && (
          <div>
            <p className="mb-1 text-xs text-muted-foreground">{t("repositoryPage.links.evidence")}</p>
            <EvidenceList evidence={link.evidence ?? []} />
          </div>
        )}
        <p className="text-xs text-muted-foreground">
          {t("repositoryPage.links.source")}: {link.source === "scan" ? t("repositoryPage.links.sourceScan") : t("repositoryPage.links.sourceUser")}
        </p>

        <div className="flex flex-col gap-1.5 border-t border-border pt-3">
          {link.status !== "confirmed" && (link.to_component_id || link.to_resource_id) && (
            <Button size="sm" disabled={busy} onClick={() => run(() => api.updateLink(link.id, { status: "confirmed" }))}>
              {t("repositoryPage.links.confirm")}
            </Button>
          )}
          {link.status === "dismissed" ? (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => api.updateLink(link.id, { status: "suggested" }))}>
              {t("repositoryPage.links.restore")}
            </Button>
          ) : (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => api.updateLink(link.id, { status: "dismissed" }))}>
              {t("repositoryPage.links.dismiss")}
            </Button>
          )}
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setPickerOpen(true)}>
            {t("repositoryPage.links.retarget")}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setResourceOpen(true)}>
            {t("repositoryPage.links.external")}
          </Button>
          {link.source === "user" && (
            <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={busy} onClick={() => setDeleteOpen(true)}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              {t("repositoryPage.links.delete")}
            </Button>
          )}
        </div>
      </Card>

      <ComponentPickerDialog open={pickerOpen} onOpenChange={setPickerOpen} onPick={(id) => run(() => api.updateLink(link.id, { to_component_id: id }))} />
      <ExternalResourceDialog open={resourceOpen} onOpenChange={setResourceOpen} onSubmit={(resource) => run(() => api.updateLink(link.id, { to_resource: resource }))} />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("repositoryPage.links.deleteConfirmTitle")}
        description={t("repositoryPage.links.deleteConfirmDesc")}
        confirmLabel={t("repositoryPage.links.delete")}
        onConfirm={() => run(() => api.deleteLink(link.id))}
      />
    </div>
  );
}

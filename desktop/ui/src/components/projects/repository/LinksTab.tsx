import { Plus, Trash2 } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type ComponentLink,
  type ComponentRole,
  type LinkStatus,
  type ResourceKind,
  type RepositoryModel,
  type WorkspaceResource,
} from "@/api";
import { AddLinkDialog } from "@/components/projects/repository/AddLinkDialog";
import { ComponentPickerDialog } from "@/components/projects/repository/ComponentPickerDialog";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import { ExternalResourceDialog } from "@/components/projects/repository/ExternalResourceDialog";
import { ResourcePickerDialog } from "@/components/projects/repository/ResourcePickerDialog";
import { EvidenceList } from "@/components/projects/model/EvidenceList";
import { ConfidenceBadge } from "@/components/projects/model/ConfidenceBadge";
import { LinkTargetLabel } from "@/components/projects/model/LinkTargetLabel";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { FormDialog } from "@/components/admin/FormDialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import {
  componentLabel,
  factValue,
  findDuplicateResourceHints,
  groupEnvVars,
  groupEvidence,
  groupFirstReason,
  groupHighestConfidence,
  groupIsAuto,
  groupIsMissing,
  groupLinks,
  groupLinksByTarget,
  groupProtocols,
  groupStatus,
  type LinkGroup,
} from "@/lib/project-model";
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
  const [selectedGroupKey, setSelectedGroupKey] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);

  if (model.components.length === 0) return null;

  const repositoryId = model.repository.id;
  const projectIds = model.repository.project_ids ?? [];

  const outgoingLinks = selectedComponentId ? groupLinks(model, selectedComponentId).outgoing : model.links;
  const incomingLinks = selectedComponentId ? groupLinks(model, selectedComponentId).incoming : model.incoming_links;
  const outgoingGroups = groupLinksByTarget(outgoingLinks);
  const incomingGroups = groupLinksByTarget(incomingLinks);
  const selectedGroup = [...outgoingGroups, ...incomingGroups].find((g) => g.key === selectedGroupKey) ?? null;

  const fromComponent = selectedComponentId ? model.components.find((c) => c.id === selectedComponentId) : undefined;

  const duplicateHints = findDuplicateResourceHints(model.links, model.resources, selectedComponentId ? [selectedComponentId] : undefined);

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
        {duplicateHints.map((hint) => (
          <DuplicateHintNotice key={`${hint.componentId}:${hint.kind}`} hint={hint} model={model} onReload={onReload} />
        ))}

        <div className="flex items-center justify-between">
          <h3 className="font-semibold">
            {t("repositoryPage.links.outgoing")} · {outgoingLinks.length}
          </h3>
          {fromComponent && (
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              {t("repositoryPage.links.addLink")}
            </Button>
          )}
        </div>
        <Card className="divide-y divide-border p-0">
          {outgoingGroups.length === 0 ? (
            <p className="p-4 text-body text-muted-foreground">{t("repositoryPage.links.empty")}</p>
          ) : (
            outgoingGroups.map((group) => {
              const primary = group.links[0];
              return (
                <LinkRow
                  key={group.key}
                  group={group}
                  selected={group.key === selectedGroupKey}
                  onSelect={() => setSelectedGroupKey(group.key)}
                  icon={
                    targetResourceKind(primary, model) ? (
                      <ResourceKindIcon kind={targetResourceKind(primary, model)!} />
                    ) : targetRole(primary, model) ? (
                      <RoleBadge role={targetRole(primary, model)!} />
                    ) : undefined
                  }
                  label={<LinkTargetLabel link={primary} model={model} />}
                />
              );
            })
          )}
        </Card>

        <h3 className="font-semibold">
          {t("repositoryPage.links.incoming")} · {incomingLinks.length}
        </h3>
        <Card className="divide-y divide-border p-0">
          {incomingGroups.length === 0 ? (
            <p className="p-4 text-body text-muted-foreground">{t("repositoryPage.links.empty")}</p>
          ) : (
            incomingGroups.map((group) => {
              const primary = group.links[0];
              const source = incomingSourceLabel(primary, model);
              return (
                <LinkRow
                  key={group.key}
                  group={group}
                  selected={group.key === selectedGroupKey}
                  onSelect={() => setSelectedGroupKey(group.key)}
                  icon={source.role ? <RoleBadge role={source.role} /> : undefined}
                  label={<span className="font-medium">{source.label}</span>}
                />
              );
            })
          )}
        </Card>
      </div>

      {selectedGroup && (
        <LinkDetailPanel
          key={selectedGroup.key}
          group={selectedGroup}
          model={model}
          repositoryId={repositoryId}
          projectIds={projectIds}
          onReload={onReload}
        />
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
  group,
  selected,
  onSelect,
  icon,
  label,
}: {
  group: LinkGroup;
  selected: boolean;
  onSelect: () => void;
  icon?: ReactNode;
  label: ReactNode;
}) {
  const { t } = useI18n();
  const links = group.links;
  const status = groupStatus(links);
  const protocols = groupProtocols(links);
  const envVars = groupEnvVars(links);
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn("flex w-full flex-wrap items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/40", selected && "bg-muted")}
    >
      <span className="flex shrink-0 items-center gap-1.5">{icon}</span>
      <span className="min-w-0 flex-1 truncate">
        {label}
        {links.length > 1 && (
          <span className="ml-2 text-micro text-muted-foreground">{t("repositoryPage.links.signalsCaption", { count: links.length })}</span>
        )}
      </span>
      <span className="text-caption text-muted-foreground">{protocols.map((p) => t(`projectModel.linkProtocols.${p}`)).join(", ")}</span>
      {envVars.length > 0 && <span className="font-mono text-micro text-muted-foreground">{envVars.join(", ")}</span>}
      {groupIsAuto(links) && <Badge variant="info">{t("repositoryPage.links.auto")}</Badge>}
      {groupIsMissing(links) && <Badge variant="warning">{t("repositoryPage.links.missing")}</Badge>}
      <Badge variant={STATUS_VARIANT[status]}>{t(`projectModel.linkStatuses.${status}`)}</Badge>
    </button>
  );
}

function LinkDetailPanel({
  group,
  model,
  repositoryId,
  projectIds,
  onReload,
}: {
  group: LinkGroup;
  model: RepositoryModel;
  repositoryId: string;
  projectIds: string[];
  onReload: () => void;
}) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [resourceOpen, setResourceOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [linkResourceOpen, setLinkResourceOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [splitOpen, setSplitOpen] = useState(false);
  const [workspaceResource, setWorkspaceResource] = useState<WorkspaceResource | null>(null);

  const links = group.links;
  const primary = links[0];
  const status = groupStatus(links);
  const confidence = groupHighestConfidence(links);
  const reason = groupFirstReason(links);
  const evidence = groupEvidence(links);
  const resourceId = primary.to_resource_id;
  const resourceName = model.resources.find((r) => r.id === resourceId)?.name;

  useEffect(() => {
    if (!resourceId) {
      setWorkspaceResource(null);
      return;
    }
    let cancelled = false;
    api
      .listWorkspaceResources()
      .then((res) => {
        if (!cancelled) setWorkspaceResource(res.resources.find((r) => r.resource.id === resourceId) ?? null);
      })
      .catch(() => {
        if (!cancelled) setWorkspaceResource(null);
      });
    return () => {
      cancelled = true;
    };
  }, [resourceId, resourceName]);

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

  const runAll = (fn: (link: ComponentLink) => Promise<unknown>) => run(() => Promise.all(links.map(fn)));

  const canDelete = links.length === 1 && primary.source === "user";
  const canMakeSeparate = Boolean(workspaceResource && workspaceResource.link_count > links.length);

  return (
    <div className="w-80 shrink-0 space-y-4">
      <Card className="space-y-3 p-4">
        <LinkTargetLabel link={primary} model={model} className="text-body font-semibold" />
        <ConfidenceBadge confidence={confidence} />
        {reason && (
          <div>
            <p className="text-xs text-muted-foreground">{t("repositoryPage.links.reason")}</p>
            <p className="text-body">{reason}</p>
          </div>
        )}
        {evidence.length > 0 && (
          <div>
            <p className="mb-1 text-xs text-muted-foreground">{t("repositoryPage.links.evidence")}</p>
            <EvidenceList evidence={evidence} />
          </div>
        )}
        <p className="text-xs text-muted-foreground">
          {t("repositoryPage.links.source")}: {primary.source === "scan" ? t("repositoryPage.links.sourceScan") : t("repositoryPage.links.sourceUser")}
        </p>

        <div className="flex flex-col gap-1.5 border-t border-border pt-3">
          {status !== "confirmed" && (primary.to_component_id || primary.to_resource_id) && (
            <Button size="sm" disabled={busy} onClick={() => runAll((link) => api.updateLink(link.id, { status: "confirmed" }))}>
              {t("repositoryPage.links.confirm")}
            </Button>
          )}
          {status === "dismissed" ? (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => runAll((link) => api.updateLink(link.id, { status: "suggested" }))}>
              {t("repositoryPage.links.restore")}
            </Button>
          ) : (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => runAll((link) => api.updateLink(link.id, { status: "dismissed" }))}>
              {t("repositoryPage.links.dismiss")}
            </Button>
          )}
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setPickerOpen(true)}>
            {t("repositoryPage.links.retarget")}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setResourceOpen(true)}>
            {t("repositoryPage.links.external")}
          </Button>
          {!primary.to_resource_id && (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => setLinkResourceOpen(true)}>
              {t("repositoryPage.links.resource.link")}
            </Button>
          )}
          {canDelete && (
            <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={busy} onClick={() => setDeleteOpen(true)}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              {t("repositoryPage.links.delete")}
            </Button>
          )}
        </div>
      </Card>

      {workspaceResource && (
        <ResourceDetailCard
          resource={workspaceResource}
          canMakeSeparate={canMakeSeparate}
          busy={busy}
          onMerge={() => setMergeOpen(true)}
          onRename={() => setRenameOpen(true)}
          onMakeSeparate={() => setSplitOpen(true)}
        />
      )}

      <ComponentPickerDialog open={pickerOpen} onOpenChange={setPickerOpen} onPick={(id) => runAll((link) => api.updateLink(link.id, { to_component_id: id }))} />
      <ExternalResourceDialog
        open={resourceOpen}
        onOpenChange={setResourceOpen}
        onSubmit={(resource) => runAll((link) => api.updateLink(link.id, { to_resource: resource }))}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("repositoryPage.links.deleteConfirmTitle")}
        description={t("repositoryPage.links.deleteConfirmDesc")}
        confirmLabel={t("repositoryPage.links.delete")}
        onConfirm={() => run(() => api.deleteLink(primary.id))}
      />

      {!primary.to_resource_id && (
        <ResourcePickerDialog
          open={linkResourceOpen}
          onOpenChange={setLinkResourceOpen}
          title={t("repositoryPage.links.resource.linkTitle")}
          description={t("repositoryPage.links.resource.linkDescription")}
          submitLabel={t("repositoryPage.links.resource.linkSubmit")}
          repositoryId={repositoryId}
          projectIds={projectIds}
          onPick={(picked) => runAll((link) => api.updateLink(link.id, { to_resource_id: picked.resource.id }))}
        />
      )}

      {workspaceResource && (
        <>
          <ResourcePickerDialog
            open={mergeOpen}
            onOpenChange={setMergeOpen}
            title={t("repositoryPage.links.resource.mergeTitle")}
            description={t("repositoryPage.links.resource.mergeDescription")}
            submitLabel={t("repositoryPage.links.resource.mergeSubmit")}
            excludeResourceId={workspaceResource.resource.id}
            defaultKind={workspaceResource.resource.kind}
            repositoryId={repositoryId}
            projectIds={projectIds}
            onPick={async (picked) => {
              await api.mergeResource(workspaceResource.resource.id, picked.resource.id);
              toast.success(t("repositoryPage.links.resource.mergeSuccess"));
              onReload();
            }}
          />
          <RenameResourceDialog
            open={renameOpen}
            onOpenChange={setRenameOpen}
            initialName={workspaceResource.resource.name}
            onSubmit={async (name) => {
              await api.renameResource(workspaceResource.resource.id, name);
              toast.success(t("repositoryPage.links.resource.renameSuccess"));
              onReload();
            }}
          />
          <ConfirmDialog
            open={splitOpen}
            onOpenChange={setSplitOpen}
            variant="default"
            title={t("repositoryPage.links.resource.makeSeparateConfirmTitle")}
            description={t("repositoryPage.links.resource.makeSeparateConfirmDesc")}
            confirmLabel={t("repositoryPage.links.resource.makeSeparate")}
            onConfirm={() =>
              run(async () => {
                await api.splitResource(workspaceResource.resource.id, links.map((l) => l.id));
                toast.success(t("repositoryPage.links.resource.makeSeparateSuccess"));
              })
            }
          />
        </>
      )}
    </div>
  );
}

function ResourceDetailCard({
  resource,
  canMakeSeparate,
  busy,
  onMerge,
  onRename,
  onMakeSeparate,
}: {
  resource: WorkspaceResource;
  canMakeSeparate: boolean;
  busy: boolean;
  onMerge: () => void;
  onRename: () => void;
  onMakeSeparate: () => void;
}) {
  const { t } = useI18n();
  const sharedAcrossProjects = resource.projects.length > 1;
  return (
    <Card className="space-y-3 p-4">
      <div className="flex items-center gap-2">
        <ResourceKindIcon kind={resource.resource.kind} />
        <span className="min-w-0 flex-1 truncate font-semibold">{resource.resource.name}</span>
      </div>
      <p className="text-caption text-muted-foreground">
        {t(`projectModel.resourceKinds.${resource.resource.kind}`)}
        {resource.resource.vendor ? ` · ${resource.resource.vendor}` : ""}
      </p>
      {sharedAcrossProjects && <Badge variant="info">{t("repositoryPage.links.resource.sharedAcrossProjects")}</Badge>}
      {resource.users.length > 0 && (
        <div>
          <p className="mb-1 text-xs text-muted-foreground">{t("repositoryPage.links.resource.usedBy")}</p>
          <ul className="space-y-0.5 text-caption text-muted-foreground">
            {resource.users.map((u) => (
              <li key={u.component_id} className="truncate">
                {u.repository_name}
                {u.component_path && u.component_path !== "." ? `/${u.component_path}` : ""}
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-1.5 border-t border-border pt-3">
        <Button size="sm" variant="outline" disabled={busy} onClick={onMerge}>
          {t("repositoryPage.links.resource.merge")}
        </Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={onRename}>
          {t("repositoryPage.links.resource.rename")}
        </Button>
        {canMakeSeparate && (
          <Button size="sm" variant="outline" disabled={busy} onClick={onMakeSeparate}>
            {t("repositoryPage.links.resource.makeSeparate")}
          </Button>
        )}
      </div>
    </Card>
  );
}

function RenameResourceDialog({
  open,
  onOpenChange,
  initialName,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialName: string;
  onSubmit: (name: string) => Promise<void>;
}) {
  const { t } = useI18n();
  const [name, setName] = useState(initialName);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (open) setName(initialName);
  }, [open, initialName]);

  const submit = async () => {
    if (!name.trim()) return;
    setSubmitting(true);
    try {
      await onSubmit(name.trim());
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("repositoryPage.links.resource.renameTitle")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={submitting || !name.trim()}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label>{t("repositoryPage.externalResource.name")}</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} />
      </div>
    </FormDialog>
  );
}

function DuplicateHintNotice({
  hint,
  model,
  onReload,
}: {
  hint: { componentId: string; kind: ResourceKind; resourceIds: string[] };
  model: RepositoryModel;
  onReload: () => void;
}) {
  const { t } = useI18n();
  const [dialogOpen, setDialogOpen] = useState(false);

  const component = model.components.find((c) => c.id === hint.componentId);
  if (!component) return null;
  const componentName = componentLabel(component, model.repository.name);
  const resources = hint.resourceIds.map((id) => model.resources.find((r) => r.id === id)).filter((r): r is NonNullable<typeof r> => Boolean(r));
  if (resources.length < 2) return null;
  const names = resources.map((r) => r.name).join(", ");
  const kindLabel = t(`projectModel.resourceKinds.${hint.kind}`);

  return (
    <>
      <Notice
        variant="info"
        title={t("repositoryPage.links.duplicate.notice", { component: componentName, count: resources.length, kind: kindLabel, names })}
      >
        <Button size="sm" variant="outline" onClick={() => setDialogOpen(true)}>
          {t("repositoryPage.links.duplicate.mergeButton")}
        </Button>
      </Notice>
      <DuplicateMergeDialog open={dialogOpen} onOpenChange={setDialogOpen} resources={resources} onReload={onReload} />
    </>
  );
}

function DuplicateMergeDialog({
  open,
  onOpenChange,
  resources,
  onReload,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resources: { id: string; name: string }[];
  onReload: () => void;
}) {
  const { t } = useI18n();
  const [chosenId, setChosenId] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (open) setChosenId(resources[0]?.id ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const submit = async () => {
    if (!chosenId) return;
    setSubmitting(true);
    try {
      for (const resource of resources) {
        if (resource.id === chosenId) continue;
        await api.mergeResource(resource.id, chosenId);
      }
      toast.success(t("repositoryPage.links.duplicate.success"));
      onOpenChange(false);
      onReload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("repositoryPage.links.duplicate.dialogTitle")}
      description={t("repositoryPage.links.duplicate.dialogDescription")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={submitting || !chosenId}>
            {t("repositoryPage.links.duplicate.submit")}
          </Button>
        </>
      }
    >
      <fieldset className="rounded-lg border border-border">
        <legend className="sr-only">{t("repositoryPage.links.duplicate.dialogTitle")}</legend>
        <div className="divide-y divide-border">
          {resources.map((resource) => (
            <label key={resource.id} className="flex cursor-pointer items-center gap-3 px-3 py-2 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted">
              <input
                type="radio"
                name="duplicate-merge-target"
                className="h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                checked={chosenId === resource.id}
                onChange={() => setChosenId(resource.id)}
              />
              <span className="text-body">{resource.name}</span>
            </label>
          ))}
        </div>
      </fieldset>
    </FormDialog>
  );
}

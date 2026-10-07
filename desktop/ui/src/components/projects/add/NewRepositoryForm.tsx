import { useId } from "react";
import { COMPONENT_ROLES, REPO_DOC_KINDS, type ComponentRole, type GitHubOwner, type RepoDocKind } from "@/api";
import type { NewRepositoryInput } from "@/components/projects/add/flow-types";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export interface NewRepositoryDraft {
  name: string;
  owner: string;
  description: string;
  role: ComponentRole;
  stack: string;
  notes: string;
  scaffold: boolean;
  docs: RepoDocKind[];
}

export const EMPTY_NEW_REPOSITORY: NewRepositoryDraft = {
  name: "",
  owner: "",
  description: "",
  role: "frontend",
  stack: "",
  notes: "",
  scaffold: true,
  docs: [...REPO_DOC_KINDS],
};

export const STACK_SUGGESTIONS = [
  "Next.js (TypeScript)",
  "React + Vite (TypeScript)",
  "Vue 3 + Vite",
  "Node.js + Express (TypeScript)",
  "NestJS",
  "Go (net/http)",
  "Go + Fiber",
  "Python + FastAPI",
  "Django",
  "Java + Spring Boot",
  "Java + Quarkus",
  "Flutter",
  "SwiftUI (iOS)",
  "Kotlin + Jetpack Compose (Android)",
  "React Native (Expo)",
];

const DOC_LABEL_KEY: Record<RepoDocKind, string> = {
  coding_standards: "repositoryPage.components.docsCodingStandards",
  test_standards: "repositoryPage.components.docsTestStandards",
  architecture: "repositoryPage.components.docsArchitecture",
  local_run: "repositoryPage.components.docsLocalRun",
};

const REPO_NAME_MAX = 100;

/** Mirrors domain.SanitizeRepoName: the one name the repository gets on GitHub
 * and as its local folder, so the preview shows exactly what will exist. */
export function sanitizeRepoName(name: string): string {
  let out = "";
  for (const ch of name.trim().toLowerCase()) {
    if (/[a-z0-9._-]/.test(ch)) out += ch;
    else if (ch === " ") out += "-";
  }
  const clean = out.replace(/^[-._]+|[-._]+$/g, "");
  // Windows refuses a folder named after a device (con, nul, com1…), with or
  // without an extension; repositories move between machines, so no OS gets one.
  const dot = clean.indexOf(".");
  const stem = dot < 0 ? clean : clean.slice(0, dot);
  return /^(con|prn|aux|nul|com[0-9]|lpt[0-9])$/.test(stem) ? stem + "-repo" + (dot < 0 ? "" : clean.slice(dot)) : clean;
}

export type RepoNameError = "required" | "no_letters" | "too_long";

/** Mirrors domain.NewRepoDirName's refusals. */
export function repoNameError(name: string): RepoNameError | null {
  if (!name.trim()) return "required";
  const clean = sanitizeRepoName(name);
  if (!clean) return "no_letters";
  if (clean.length > REPO_NAME_MAX) return "too_long";
  return null;
}

const NAME_ERROR_KEY: Record<Exclude<RepoNameError, "required">, string> = {
  no_letters: "addRepository.newRepo.nameNoLetters",
  too_long: "addRepository.newRepo.nameTooLong",
};

export function toNewRepositoryInput(draft: NewRepositoryDraft): NewRepositoryInput {
  const optional = (value: string) => value.trim() || undefined;
  return {
    name: sanitizeRepoName(draft.name),
    owner: optional(draft.owner),
    description: optional(draft.description),
    role: draft.role,
    stack: optional(draft.stack),
    notes: optional(draft.notes),
    scaffold: draft.scaffold,
    docs: REPO_DOC_KINDS.filter((kind) => draft.docs.includes(kind)),
  };
}

interface NewRepositoryFormProps {
  value: NewRepositoryDraft;
  onChange: (next: NewRepositoryDraft) => void;
  owners: GitHubOwner[];
  disabled?: boolean;
}

/** The "New repository" source's details: what the repository will be, so the
 * bootstrap task can scaffold it and write its reference docs. */
export function NewRepositoryForm({ value, onChange, owners, disabled }: NewRepositoryFormProps) {
  const { t } = useI18n();
  const id = useId();
  const set = <K extends keyof NewRepositoryDraft>(key: K, v: NewRepositoryDraft[K]) => onChange({ ...value, [key]: v });
  const nameError = repoNameError(value.name);
  const nameInvalid = nameError === "no_letters" || nameError === "too_long";
  const sanitized = sanitizeRepoName(value.name);
  const showPreview = nameError === null && sanitized !== value.name.trim();

  const toggleDoc = (kind: RepoDocKind, checked: boolean) =>
    set("docs", checked ? [...value.docs.filter((k) => k !== kind), kind] : value.docs.filter((k) => k !== kind));

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("addRepository.newRepo.title")}</CardTitle>
        <CardDescription>{t("addRepository.newRepo.agentNote")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-name`}>{t("addRepository.newRepo.nameLabel")}</Label>
          <Input
            id={`${id}-name`}
            value={value.name}
            onChange={(e) => set("name", e.target.value)}
            placeholder={t("addRepository.newRepo.namePlaceholder")}
            aria-invalid={nameInvalid || undefined}
            aria-describedby={`${id}-name-hint`}
            disabled={disabled}
            required
          />
          <p id={`${id}-name-hint`} className={cn("text-caption", nameInvalid ? "text-destructive" : "text-muted-foreground")}>
            {nameError === "no_letters" || nameError === "too_long"
              ? t(NAME_ERROR_KEY[nameError])
              : t("addRepository.newRepo.nameHint")}
          </p>
          {showPreview && (
            <p className="text-caption text-muted-foreground">
              {t("addRepository.newRepo.namePreview")} <span className="font-mono text-foreground">{sanitized}</span>
            </p>
          )}
        </div>

        {owners.length > 0 && (
          <div className="space-y-1.5">
            <Label>{t("addRepository.newRepo.ownerLabel")}</Label>
            <Select value={value.owner} onValueChange={(v) => set("owner", v)} disabled={disabled}>
              <SelectTrigger aria-label={t("addRepository.newRepo.ownerLabel")}>
                <SelectValue placeholder={t("addRepository.newRepo.ownerPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {owners.map((o) => (
                  <SelectItem key={o.login} value={o.login}>
                    {o.login} {o.type === "org" ? t("addRepository.source.githubOrgSuffix") : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        <div className="space-y-1.5">
          <Label htmlFor={`${id}-description`}>{t("addRepository.newRepo.descriptionLabel")}</Label>
          <Textarea
            id={`${id}-description`}
            value={value.description}
            onChange={(e) => set("description", e.target.value)}
            placeholder={t("addRepository.newRepo.descriptionPlaceholder")}
            disabled={disabled}
          />
          <p className="text-caption text-muted-foreground">{t("addRepository.newRepo.descriptionHint")}</p>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>{t("addRepository.newRepo.roleLabel")}</Label>
            <Select value={value.role} onValueChange={(v) => set("role", v as ComponentRole)} disabled={disabled}>
              <SelectTrigger aria-label={t("addRepository.newRepo.roleLabel")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {COMPONENT_ROLES.map((role) => (
                  <SelectItem key={role} value={role}>
                    {t(`projectModel.roles.${role}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={`${id}-stack`}>{t("addRepository.newRepo.stackLabel")}</Label>
            <Input
              id={`${id}-stack`}
              list={`${id}-stack-suggestions`}
              value={value.stack}
              onChange={(e) => set("stack", e.target.value)}
              placeholder={t("addRepository.newRepo.stackPlaceholder")}
              disabled={disabled}
            />
            <datalist id={`${id}-stack-suggestions`}>
              {STACK_SUGGESTIONS.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
          </div>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor={`${id}-notes`}>{t("addRepository.newRepo.notesLabel")}</Label>
          <Textarea
            id={`${id}-notes`}
            value={value.notes}
            onChange={(e) => set("notes", e.target.value)}
            placeholder={t("addRepository.newRepo.notesPlaceholder")}
            disabled={disabled}
          />
        </div>

        <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
          <div>
            <Label htmlFor={`${id}-scaffold`}>{t("addRepository.newRepo.scaffoldLabel")}</Label>
            <p className="text-micro text-muted-foreground">{t("addRepository.newRepo.scaffoldHint")}</p>
          </div>
          <Switch
            id={`${id}-scaffold`}
            checked={value.scaffold}
            onCheckedChange={(checked) => set("scaffold", checked)}
            disabled={disabled}
          />
        </div>

        <fieldset className="space-y-2">
          <legend className="text-body font-medium">{t("addRepository.newRepo.docsLabel")}</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {REPO_DOC_KINDS.map((kind) => (
              <div key={kind} className="flex items-center gap-2">
                <Checkbox
                  id={`${id}-doc-${kind}`}
                  checked={value.docs.includes(kind)}
                  onCheckedChange={(checked) => toggleDoc(kind, checked === true)}
                  disabled={disabled}
                />
                <Label htmlFor={`${id}-doc-${kind}`} className="font-normal">
                  {t(DOC_LABEL_KEY[kind])}
                </Label>
              </div>
            ))}
          </div>
        </fieldset>
      </CardContent>
    </Card>
  );
}

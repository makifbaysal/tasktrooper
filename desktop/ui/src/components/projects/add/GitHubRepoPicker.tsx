import { Lock } from "lucide-react";
import { useState } from "react";
import type { GitHubOwner, GitHubRepoInfo } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface GitHubRepoPickerProps {
  owners: GitHubOwner[];
  ownersError: string;
  owner: string;
  onOwnerChange: (owner: string) => void;
  repos: GitHubRepoInfo[];
  loading: boolean;
  selected: Set<string>;
  onToggle: (name: string) => void;
  /** Repository names already linked somewhere in this workspace — checked
   * and disabled rather than hidden, so the user sees why it's unavailable. */
  existingNames: Set<string>;
}

/** The owner select + searchable, scrollable checkbox list for the "From
 * GitHub" source card. Purely presentational: the owner's repo list and the
 * selection set both live in the caller (SourceStep), which also needs the
 * fetched repos to resolve each selection's clone_url at submit time. */
export function GitHubRepoPicker({
  owners,
  ownersError,
  owner,
  onOwnerChange,
  repos,
  loading,
  selected,
  onToggle,
  existingNames,
}: GitHubRepoPickerProps) {
  const { t } = useI18n();
  const [search, setSearch] = useState("");

  if (ownersError) {
    return <p className="text-body text-destructive">{ownersError}</p>;
  }

  const visible = repos.filter((r) => !search.trim() || r.name.toLowerCase().includes(search.trim().toLowerCase()));

  return (
    <div className="space-y-3">
      <div className="space-y-1.5">
        <Label>{t("addRepository.source.githubOwnerLabel")}</Label>
        <Select value={owner} onValueChange={onOwnerChange}>
          <SelectTrigger>
            <SelectValue placeholder={t("addRepository.source.githubOwnerPlaceholder")} />
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
      <Input
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder={t("addRepository.source.githubSearchPlaceholder")}
      />
      <ScrollArea className="h-56 rounded-md border">
        {loading ? (
          <p className="p-4 text-body text-muted-foreground">{t("addRepository.source.githubLoading")}</p>
        ) : visible.length === 0 ? (
          <p className="p-4 text-body text-muted-foreground">{t("addRepository.source.githubEmpty")}</p>
        ) : (
          <div className="divide-y divide-border/60">
            {visible.map((r) => {
              const added = existingNames.has(r.name);
              return (
                <label
                  key={r.full_name}
                  className={cn("flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted/40", added && "opacity-50")}
                >
                  <Checkbox checked={selected.has(r.name)} disabled={added} onCheckedChange={() => onToggle(r.name)} />
                  <span className="flex-1 truncate text-body">{r.name}</span>
                  {r.private && <Lock className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden />}
                  {added && <Badge variant="outline">{t("addRepository.source.githubAdded")}</Badge>}
                </label>
              );
            })}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}

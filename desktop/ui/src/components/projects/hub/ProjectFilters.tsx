import { Search } from "lucide-react";
import { COMPONENT_ROLES, type ComponentRole } from "@/api";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ProjectFiltersProps {
  role: ComponentRole | null;
  onRoleChange: (role: ComponentRole | null) => void;
  query: string;
  onQueryChange: (query: string) => void;
}

/** Role chips (reusing RoleBadge for its per-role color) plus the free-text
 * search box; the parent keeps both in the URL query. */
export function ProjectFilters({ role, onRoleChange, query, onQueryChange }: ProjectFiltersProps) {
  const { t } = useI18n();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          onClick={() => onRoleChange(null)}
          className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Badge variant={role === null ? "default" : "outline"}>{t("projectsHub.filters.allRoles")}</Badge>
        </button>
        {COMPONENT_ROLES.map((r) => (
          <button
            key={r}
            type="button"
            onClick={() => onRoleChange(r)}
            className={cn(
              "rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              role !== null && role !== r && "opacity-50 hover:opacity-100",
            )}
          >
            <RoleBadge role={r} />
          </button>
        ))}
      </div>
      <div className="relative ml-auto min-w-[220px] flex-1 sm:max-w-xs">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder={t("projectsHub.filters.searchPlaceholder")}
          className="h-8 pl-8 text-sm"
        />
      </div>
    </div>
  );
}

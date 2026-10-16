import { BookOpen } from "lucide-react";
import type { DataSourceKindInfo, ObservabilityRecipe, ObservabilitySignal } from "@/api";
import { kindLabel } from "@/components/operations/sources/dataSourceSchema";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

interface RecipeCatalogProps {
  recipes: ObservabilityRecipe[];
  signals: ObservabilitySignal[];
  kinds: DataSourceKindInfo[];
  loading?: boolean;
}

/** Read-only: what the console knows how to read, one recipe per exporter. */
export function RecipeCatalog({ recipes, signals, kinds, loading = false }: RecipeCatalogProps) {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.sources.recipes.${key}`, params);
  const unitOf = new Map(signals.map((s) => [s.id, s.unit]));
  const sorted = [...recipes].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-heading font-semibold">{r("title")}</h2>
        <p className="max-w-3xl text-caption text-muted-foreground">{r("description")}</p>
      </div>
      <Notice variant="info" title={r("customLaterTitle")}>
        {r("customLater")}
      </Notice>

      {loading && recipes.length === 0 ? (
        <Skeleton className="h-48 w-full" />
      ) : sorted.length === 0 ? (
        <Card>
          <EmptyState icon={BookOpen} title={r("empty")} />
        </Card>
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-body" aria-label={r("listLabel")}>
              <thead>
                <tr className="border-b border-border text-left text-caption text-muted-foreground">
                  <th scope="col" className="px-4 py-2.5 font-medium">{r("columns.id")}</th>
                  <th scope="col" className="px-4 py-2.5 font-medium">{r("columns.technology")}</th>
                  <th scope="col" className="px-4 py-2.5 font-medium">{r("columns.sourceKind")}</th>
                  <th scope="col" className="px-4 py-2.5 font-medium">{r("columns.signals")}</th>
                  <th scope="col" className="px-4 py-2.5 font-medium">{r("columns.detect")}</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((recipe) => {
                  const kind = kinds.find((k) => k.kind === recipe.source_kind);
                  const tech = [recipe.applies_to?.technology, recipe.applies_to?.messaging_system]
                    .filter((v, i, all) => v && all.indexOf(v) === i)
                    .join(" · ");
                  const signalIds = Object.keys(recipe.signals ?? {}).sort();
                  return (
                    <tr key={recipe.id} className="border-b border-border/50 align-top last:border-0">
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-mono text-caption font-medium">{recipe.id}</span>
                          {recipe.origin === "user" && <Badge variant="info">{r("origin.user")}</Badge>}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-caption">{tech || "—"}</td>
                      <td className="whitespace-nowrap px-4 py-3 text-caption">
                        {kind ? kindLabel(t, kind) : recipe.source_kind}
                      </td>
                      <td className="px-4 py-3">
                        <ul className="flex flex-wrap gap-1" aria-label={r("signalsOf", { recipe: recipe.id })}>
                          {signalIds.map((id) => {
                            const unit = unitOf.get(id);
                            return (
                              <li key={id}>
                                <Badge variant="outline" className="font-mono font-normal" title={unit ? r("unit", { unit }) : undefined}>
                                  {id}
                                </Badge>
                              </li>
                            );
                          })}
                        </ul>
                      </td>
                      <td className="px-4 py-3 font-mono text-caption text-muted-foreground">
                        {recipe.detect?.metric || "—"}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  );
}

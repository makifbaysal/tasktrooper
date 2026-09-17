import { Database, Search } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type Repository, type WorkspaceChunk, type WorkspaceIndex } from "@/api";
import { ProjectIndexStatus } from "@/components/projects/ProjectIndexStatus";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";

type CodeIndexSearchPanelProps = {
  repositories: Repository[];
  loading?: boolean;
};

type TranslateFn = (key: string, params?: Record<string, string | number>) => string;

function indexStatusLabel(status: WorkspaceIndex["status"], t: TranslateFn): string {
  switch (status) {
    case "completed":
      return t("chatArea.rag.codeSearch.statusReady");
    case "running":
      return t("chatArea.rag.codeSearch.statusIndexing");
    case "pending":
      return t("chatArea.rag.codeSearch.statusPending");
    case "failed":
      return t("chatArea.rag.codeSearch.statusFailed");
    default:
      return status;
  }
}

function truncateContent(content: string, max = 320): string {
  const trimmed = content.trim();
  if (trimmed.length <= max) return trimmed;
  return trimmed.slice(0, max) + "…";
}

export function CodeIndexSearchPanel({ repositories, loading = false }: CodeIndexSearchPanelProps) {
  const { t } = useI18n();
  const [selectedId, setSelectedId] = useState<string>("");
  const [index, setIndex] = useState<WorkspaceIndex | null>(null);
  const [indexLoading, setIndexLoading] = useState(false);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [results, setResults] = useState<WorkspaceChunk[]>([]);

  useEffect(() => {
    if (repositories.length === 0) {
      setSelectedId("");
      return;
    }
    if (!selectedId || !repositories.some((repo) => repo.id === selectedId)) {
      setSelectedId(repositories[0].id);
    }
  }, [repositories, selectedId]);

  const loadIndexStatus = useCallback(async (repositoryId: string) => {
    setIndexLoading(true);
    try {
      const data = await api.getRepositoryIndexStatus(repositoryId);
      setIndex(data);
    } catch {
      setIndex(null);
    } finally {
      setIndexLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!selectedId) {
      setIndex(null);
      setResults([]);
      return;
    }
    void loadIndexStatus(selectedId);
    setResults([]);
  }, [selectedId, loadIndexStatus]);

  const handleSearch = async () => {
    const trimmed = query.trim();
    if (!selectedId || !trimmed) return;
    setSearching(true);
    try {
      const data = await api.searchRepositoryIndex(selectedId, trimmed);
      setResults(data.results ?? []);
      if ((data.results ?? []).length === 0) {
        toast.info(t("chatArea.rag.codeSearch.noMatches"));
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("chatArea.rag.codeSearch.searchFailed"));
      setResults([]);
    } finally {
      setSearching(false);
    }
  };


  if (loading) {
    return (
      <Card className="space-y-4 p-6">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </Card>
    );
  }

  if (repositories.length === 0) {
    return (
      <EmptyState
        icon={Database}
        title={t("chatArea.rag.codeSearch.noRepoTitle")}
        description={t("chatArea.rag.codeSearch.noRepoDescription")}
        action={
          <Button asChild>
            <Link to="/repositories">{t("chatArea.rag.codeSearch.codeRepositories")}</Link>
          </Button>
        }
      />
    );
  }

  return (
    <Card className="space-y-5 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-heading font-semibold">{t("chatArea.rag.codeSearch.title")}</h2>
          <p className="text-caption text-muted-foreground">
            {t("chatArea.rag.codeSearch.subtitle")}
          </p>
        </div>
        {selectedId && <ProjectIndexStatus repositoryId={selectedId} />}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label>{t("chatArea.rag.codeSearch.repository")}</Label>
          <Select value={selectedId} onValueChange={setSelectedId}>
            <SelectTrigger>
              <SelectValue placeholder={t("chatArea.rag.codeSearch.selectRepo")} />
            </SelectTrigger>
            <SelectContent>
              {repositories.map((repo) => (
                <SelectItem key={repo.id} value={repo.id}>
                  {repo.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("chatArea.rag.codeSearch.indexStatus")}</Label>
          <div className="flex min-h-10 items-center gap-2 rounded-md border border-border px-3 text-sm">
            {indexLoading ? (
              <Spinner size="sm" />
            ) : index ? (
              <>
                <Badge variant={index.status === "completed" ? "default" : "secondary"}>
                  {indexStatusLabel(index.status, t)}
                </Badge>
                <span className="text-muted-foreground">
                  {t("chatArea.rag.codeSearch.filesChunks", { files: index.file_count, chunks: index.chunk_count })}
                </span>
              </>
            ) : (
              <span className="text-muted-foreground">{t("chatArea.rag.codeSearch.noIndex")}</span>
            )}
          </div>
        </div>
      </div>

      <div className="flex flex-col gap-2 sm:flex-row">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("chatArea.rag.codeSearch.searchPlaceholder")}
          onKeyDown={(e) => {
            if (e.key === "Enter") void handleSearch();
          }}
          disabled={index?.status !== "completed" || searching}
        />
        <Button
          onClick={() => void handleSearch()}
          disabled={!query.trim() || index?.status !== "completed" || searching}
          className="gap-2 sm:w-auto"
        >
          {searching ? <Spinner size="sm" /> : <Search className="h-4 w-4" />}
          {t("chatArea.rag.codeSearch.search")}
        </Button>
      </div>

      {index && index.status !== "completed" && (
        <p className="text-sm text-muted-foreground">
          {t("chatArea.rag.codeSearch.indexIncomplete")}{" "}
          <Link className="text-primary underline-offset-4 hover:underline" to={`/repositories/${selectedId}/settings`}>
            {t("chatArea.rag.codeSearch.repoSettings")}
          </Link>{" "}
          {t("chatArea.rag.codeSearch.reindexHint")}
        </p>
      )}

      {results.length > 0 && (
        <div className="space-y-3">
          <p className="text-sm font-medium">{t("chatArea.rag.codeSearch.resultsCount", { count: results.length })}</p>
          {results.map((chunk) => (
            <div key={chunk.id} className="rounded-lg border border-border bg-muted/20 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="font-mono text-sm font-medium">
                  {chunk.file_path}
                  {chunk.start_line > 0 && (
                    <span className="text-muted-foreground">
                      {" "}
                      :{chunk.start_line}
                      {chunk.end_line > chunk.start_line ? `-${chunk.end_line}` : ""}
                    </span>
                  )}
                </p>
                {typeof chunk.score === "number" && (
                  <Badge variant="outline">{chunk.score.toFixed(3)}</Badge>
                )}
              </div>
              {(chunk.symbol_name || chunk.kind) && (
                <p className="mt-1 text-xs text-muted-foreground">
                  {[chunk.kind, chunk.symbol_name].filter(Boolean).join(" · ")}
                </p>
              )}
              <pre className="mt-3 overflow-x-auto whitespace-pre-wrap font-mono text-xs text-foreground/90">
                {truncateContent(chunk.content)}
              </pre>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

import { FileUp, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type FileRecord, type Repository } from "@/api";
import { PageContent } from "@/components/layout/PageContent";
import { CodeIndexSearchPanel } from "@/components/rag/CodeIndexSearchPanel";
import { EmbeddingMapPanel } from "@/components/rag/EmbeddingMapPanel";
import { FormDialog } from "@/components/admin/FormDialog";
import { PageHeader } from "@/components/admin/PageHeader";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { CACHE_REPOS } from "@/lib/project-board";
import type { EmbeddingMapSearch } from "@/lib/embeddingMapSearch";
import { formatDate, formatFileSize } from "@/lib/utils";

const CACHE_FILES = "content.files";

export function FilesPage() {
  const { t } = useI18n();
  const [files, setFiles] = useCachedState<FileRecord[]>(CACHE_FILES, []);
  const [repositories, setRepositories] = useCachedState<Repository[]>(CACHE_REPOS, []);
  const [reposLoading, setReposLoading] = useFirstLoad(CACHE_REPOS);
  const [loading, setLoading] = useFirstLoad(CACHE_FILES);
  const [codeSearch, setCodeSearch] = useState<EmbeddingMapSearch | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // No skeleton on a refresh: the list stays on screen and is replaced in
  // place once the new payload lands.
  const refresh = useCallback(async () => {
    try {
      const [filesData, reposData] = await Promise.all([
        api.listFiles(),
        api.listRepositories(),
      ]);
      setFiles(filesData.files ?? []);
      setRepositories(reposData.repositories ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.files.loadFailed"));
    } finally {
      setLoading(false);
      setReposLoading(false);
    }
  }, [t, setFiles, setRepositories, setLoading, setReposLoading]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const openUpload = () => {
    setSelectedFile(null);
    setUploadOpen(true);
  };

  const closeUpload = () => {
    setUploadOpen(false);
    setSelectedFile(null);
  };

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0] ?? null;
    setSelectedFile(file);
    e.target.value = "";
  };

  const handleUpload = async () => {
    if (!selectedFile) return;
    setUploading(true);
    try {
      await api.uploadFile(selectedFile);
      toast.success(t("content.files.uploadedToast", { name: selectedFile.name }));
      closeUpload();
      await refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("content.files.uploadFailed"));
    } finally {
      setUploading(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteId) return;
    setDeleting(true);
    try {
      await api.deleteFile(deleteId);
      toast.success(t("content.files.deletedToast"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.files.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteId(null);
    }
  };

  return (
    <>
      <PageHeader
        title={t("content.files.title")}
        description={t("content.files.description")}
        action={
          <Button onClick={openUpload} className="gap-2">
            <Plus className="h-4 w-4" />
            {t("content.files.upload")}
          </Button>
        }
      />

      <PageContent className="space-y-8 pb-8">
        <CodeIndexSearchPanel
          repositories={repositories}
          loading={reposLoading}
          onSearchResults={setCodeSearch}
        />

        <EmbeddingMapPanel search={codeSearch} onClearSearch={() => setCodeSearch(null)} />

        <div className="space-y-3">
          <div>
            <h2 className="font-semibold">{t("content.files.uploadedTitle")}</h2>
            <p className="text-sm text-muted-foreground">{t("content.files.uploadedSubtitle")}</p>
          </div>

      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : files.length === 0 ? (
        <EmptyState
          icon={FileUp}
          title={t("content.files.emptyTitle")}
          description={t("content.files.emptyDescription")}
          action={
            <Button onClick={openUpload} className="gap-2">
              <Plus className="h-4 w-4" />
              {t("content.files.uploadFirst")}
            </Button>
          }
        />
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("content.files.colFile")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.files.colType")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.files.colSize")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.files.colUploaded")}</th>
                  <th className="px-4 py-3 w-16" />
                </tr>
              </thead>
              <tbody>
                {files.map((file) => (
                  <tr key={file.id} className="border-b border-border last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3 font-medium">{file.filename}</td>
                    <td className="px-4 py-3 text-muted-foreground">{file.content_type}</td>
                    <td className="px-4 py-3 text-muted-foreground">{formatFileSize(file.size_bytes)}</td>
                    <td className="px-4 py-3 text-muted-foreground">{formatDate(file.created_at)}</td>
                    <td className="px-4 py-3">
                      <Button variant="ghost" size="icon" onClick={() => setDeleteId(file.id)}>
                        <Trash2 className="h-4 w-4 text-destructive" />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

        </div>
      </PageContent>

      <FormDialog
        open={uploadOpen}
        onOpenChange={(open) => !open && closeUpload()}
        title={t("content.files.upload")}
        description={t("content.files.uploadDialogDescription")}
        footer={
          <>
            <Button variant="outline" onClick={closeUpload}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleUpload} disabled={!selectedFile || uploading} className="gap-2">
              {uploading ? <Spinner size="sm" /> : <FileUp className="h-4 w-4" />}
              {uploading ? t("content.files.uploading") : t("content.files.uploadAction")}
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <Label>{t("content.files.fileLabel")}</Label>
          <input ref={inputRef} type="file" hidden onChange={handleFileSelect} />
          <div className="flex items-center gap-3">
            <Button type="button" variant="outline" onClick={() => inputRef.current?.click()}>
              {t("content.files.chooseFile")}
            </Button>
            {selectedFile && (
              <span className="truncate text-sm text-muted-foreground">
                {selectedFile.name} ({formatFileSize(selectedFile.size)})
              </span>
            )}
          </div>
        </div>
      </FormDialog>

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
        title={t("content.files.deleteTitle")}
        description={t("content.files.deleteDescription")}
        confirmLabel={t("content.files.deleteConfirm")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </>
  );
}

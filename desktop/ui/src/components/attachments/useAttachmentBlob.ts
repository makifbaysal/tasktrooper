import { useEffect, useState } from "react";
import { api } from "@/api";

/**
 * Fetches an attachment's bytes as a blob and exposes an object URL for
 * rendering. Needed because GET /v1/attachments/{id} requires the
 * Authorization header, which an <img src> cannot send. The object URL is
 * revoked on unmount / id change so previews never leak memory.
 */
export function useAttachmentBlob(id: string | null): { url: string | null; loading: boolean } {
  const [url, setUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!id) {
      setUrl(null);
      return;
    }
    let objectUrl: string | null = null;
    let cancelled = false;
    setLoading(true);
    api
      .fetchAttachmentBlob(id)
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setUrl(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [id]);

  return { url, loading };
}

/**
 * Saves an attachment through an authenticated blob fetch — the click handler
 * for a file that is not an image. It downloads rather than opening a tab:
 * the desktop shell hands every popup to the OS, which cannot open a blob: URL.
 */
export async function downloadAttachmentBlob(id: string, filename: string): Promise<void> {
  const blob = await api.fetchAttachmentBlob(id);
  const objectUrl = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = objectUrl;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 60_000);
}

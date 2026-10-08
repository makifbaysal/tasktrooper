import { useEffect, useState } from "react";
import { api, type AttachmentMeta, type TaskDesign } from "@/api";

export interface TaskDesignState {
  design: TaskDesign | null;
  /** Each referenced design task's image attachments (the designer's saved screenshots), by task id. */
  screenshots: Record<string, AttachmentMeta[]>;
}

const EMPTY: TaskDesignState = { design: null, screenshots: {} };

function isImage(meta: AttachmentMeta): boolean {
  return meta.content_type.startsWith("image/");
}

/**
 * `GET /v1/repositories/:id/tasks/:taskId/design` plus every referenced design
 * task's screenshots. Read once per opened task: approved designs change when
 * someone approves one, not on the drawer's poll.
 */
export function useTaskDesign(repositoryId: string, taskId: string | null, enabled: boolean): TaskDesignState {
  const [state, setState] = useState<TaskDesignState>(EMPTY);

  useEffect(() => {
    setState(EMPTY);
    if (!enabled || !repositoryId || !taskId) return;
    let cancelled = false;
    void (async () => {
      let design: TaskDesign;
      try {
        const answer = await api.getTaskDesign(repositoryId, taskId);
        design = {
          ...answer,
          references: (answer.references ?? []).map((ref) => ({ ...ref, documents: ref.documents ?? [] })),
        };
      } catch {
        return;
      }
      if (cancelled) return;
      setState({ design, screenshots: {} });
      if (design.references.length === 0) return;
      const lists = await Promise.allSettled(
        design.references.map((ref) => api.listTaskAttachments(ref.repository_id, ref.task_id)),
      );
      if (cancelled) return;
      const screenshots: Record<string, AttachmentMeta[]> = {};
      design.references.forEach((ref, index) => {
        const result = lists[index];
        if (result.status === "fulfilled") screenshots[ref.task_id] = (result.value.attachments ?? []).filter(isImage);
      });
      setState({ design, screenshots });
    })();
    return () => {
      cancelled = true;
    };
  }, [enabled, repositoryId, taskId]);

  return state;
}

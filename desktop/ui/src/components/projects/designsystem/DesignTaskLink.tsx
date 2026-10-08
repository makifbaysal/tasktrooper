import { forwardRef, type MouseEvent } from "react";
import { Link, useNavigate, type LinkProps } from "react-router-dom";
import { api } from "@/api";
import { analysisReviewPath } from "@/lib/analysis-review";

interface DesignTaskLinkProps extends Omit<LinkProps, "to"> {
  taskId: string;
  taskKey?: string;
  /** Known when the task came with its repository; otherwise it is looked up by key on click. */
  repositoryId?: string;
}

function boardTaskPath(taskId: string): string {
  return `/board?task=${encodeURIComponent(taskId)}`;
}

/**
 * Opens a design task's analysis review page, which is addressed by
 * repository. A version from a server that predates
 * `source_task_repository_id` (or whose source task is gone) has none: the
 * href then falls back to the board drawer (which works in a new tab) and a
 * plain click resolves the repository by key first.
 */
export const DesignTaskLink = forwardRef<HTMLAnchorElement, DesignTaskLinkProps>(function DesignTaskLink(
  { taskId, taskKey, repositoryId, onClick, children, ...rest },
  ref,
) {
  const navigate = useNavigate();
  const href = repositoryId ? analysisReviewPath(repositoryId, taskId) : boardTaskPath(taskId);

  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (event.defaultPrevented || repositoryId || !taskKey) return;
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    api
      .lookupTask(taskKey)
      .then((task) => navigate(analysisReviewPath(task.repository_id, task.id)))
      .catch(() => navigate(href));
  };

  return (
    <Link ref={ref} to={href} onClick={handleClick} {...rest}>
      {children ?? taskKey}
    </Link>
  );
});

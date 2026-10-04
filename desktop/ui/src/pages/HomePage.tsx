import { Navigate } from "react-router-dom";
import { Spinner } from "@/components/ui/spinner";
import { useWorkspaceOutlet } from "@/hooks/useWorkspaceOutlet";

export function HomePage() {
  const ctx = useWorkspaceOutlet();

  if (ctx?.leadAgent) return <Navigate to={`/agents/${ctx.leadAgent.id}/chat`} replace />;
  if (ctx?.workspaceLoading) {
    return (
      <div className="flex flex-1 items-center justify-center py-20">
        <Spinner size="lg" />
      </div>
    );
  }
  return <Navigate to="/board" replace />;
}

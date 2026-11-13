import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Mascot } from "@/components/ui/mascot";
import { tStatic } from "@/hooks/useI18n";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

// Top-level boundary so an unhandled render error shows a recoverable fallback
// instead of unmounting the whole tree (which left the app on a black screen).
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Application error:", error, info.componentStack);
  }

  private handleReset = () => {
    this.setState({ error: null });
  };

  render() {
    if (this.state.error) {
      return (
        <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-background p-6 text-center text-foreground">
          <Mascot pose="oops" size="xl" />
          <div className="space-y-2">
            <h1 className="text-lg font-semibold">{tStatic("common.errorBoundary.title")}</h1>
            <p className="max-w-md text-sm text-muted-foreground">
              {tStatic("common.errorBoundary.body")}
            </p>
            <p className="max-w-md break-words font-mono text-xs text-destructive">
              {this.state.error.message}
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={this.handleReset}>
              {tStatic("common.errorBoundary.retry")}
            </Button>
            <Button onClick={() => window.location.reload()}>
              {tStatic("common.errorBoundary.reload")}
            </Button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

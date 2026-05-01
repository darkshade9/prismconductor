import { Component, ErrorInfo, ReactNode } from "react";

// Class-based error boundary — React 18 still requires a class for this.
// Catches render-time exceptions in any descendant and renders a fallback,
// preventing a single bad card from blanking the entire board.
//
// Hooked in around <Card /> in the Board so a buggy IssueView field, a
// thrown Date construction, or a malformed activity payload doesn't
// cascade and unmount the whole subtree.
type Props = { children: ReactNode; fallback?: (err: Error) => ReactNode };
type State = { error: Error | null };

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Surface in the dev console so the underlying bug can be diagnosed
    // even when the fallback is rendered.
    // eslint-disable-next-line no-console
    console.error("[ErrorBoundary] caught:", error, info?.componentStack);
  }

  render() {
    if (this.state.error) {
      if (this.props.fallback) return this.props.fallback(this.state.error);
      return (
        <div className="rounded border border-red-700 bg-red-950/40 px-3 py-2 text-xs text-red-300">
          <div className="font-semibold">Render error</div>
          <div className="text-red-400 truncate" title={this.state.error.message}>
            {this.state.error.message}
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import './stores/useThemeStore'   // side-effect: applies saved theme class to <html> before first paint
import './stores/useGlowColorsStore' // side-effect: injects glow keyframes before first render
import App from './App'
import { ErrorBoundary } from './components/ErrorBoundary'

const container = document.getElementById('root')

const root = createRoot(container!)

// Top-level boundary: any descendant render exception (hook-order changes,
// thrown null deref, etc.) renders a recoverable fallback instead of blanking
// the whole app. Per-card boundaries inside <Board /> still catch finer-grained
// crashes; this is the last-resort net.
function RootFallback({ error, onReload }: { error: Error; onReload: () => void }) {
    return (
        <div className="min-h-screen bg-slate-900 text-slate-100 flex items-center justify-center p-8">
            <div className="max-w-2xl w-full rounded-lg border border-red-700 bg-red-950/40 p-6 space-y-4">
                <div>
                    <div className="text-lg font-semibold text-red-200">App render error</div>
                    <div className="text-xs text-red-300 mt-1">
                        The UI hit an unrecoverable render exception. Your conductor backend is unaffected — workers, sessions, and the database are still running. Reload the window to recover.
                    </div>
                </div>
                <pre className="text-[11px] text-red-200/80 bg-black/30 rounded p-3 overflow-auto max-h-64 whitespace-pre-wrap break-all">
{error.name}: {error.message}
{error.stack ? '\n\n' + error.stack : ''}
                </pre>
                <div className="flex items-center gap-2">
                    <button
                        onClick={onReload}
                        className="text-xs px-3 py-1.5 rounded bg-red-700 hover:bg-red-600 text-white"
                    >
                        Reload window
                    </button>
                    <span className="text-[11px] text-red-400/70">
                        Error details are also logged to the Wails DevTools console (Cmd+Opt+I in debug builds).
                    </span>
                </div>
            </div>
        </div>
    )
}

root.render(
    <React.StrictMode>
        <ErrorBoundary fallback={(err) => <RootFallback error={err} onReload={() => window.location.reload()} />}>
            <App/>
        </ErrorBoundary>
    </React.StrictMode>
)

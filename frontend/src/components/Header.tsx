import { useEffect, useRef, useState } from "react";
import { useIssueStore } from "../stores/issueStore";

const DEBOUNCE_MS = 200;

export function TitleSearch() {
  const setSearchQuery = useIssueStore((s) => s.setSearchQuery);
  const [value, setValue] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const timer = setTimeout(() => setSearchQuery(value), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [value, setSearchQuery]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape" && document.activeElement === inputRef.current) {
        setValue("");
        setSearchQuery("");
        inputRef.current?.blur();
        return;
      }
      if (e.key === "/" && document.activeElement !== inputRef.current) {
        const tag = (document.activeElement as HTMLElement)?.tagName?.toLowerCase();
        const editable = (document.activeElement as HTMLElement)?.isContentEditable;
        if (tag === "input" || tag === "textarea" || editable) return;
        e.preventDefault();
        inputRef.current?.focus();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [setSearchQuery]);

  function clear() {
    setValue("");
    setSearchQuery("");
    inputRef.current?.focus();
  }

  return (
    <div className="relative flex items-center">
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="Search title… (/)"
        aria-label="Search cards by title"
        className="w-60 bg-slate-900 border border-slate-700 rounded px-2 py-1 pr-6 text-xs text-slate-200 placeholder-slate-500 focus:outline-none focus:border-slate-500"
      />
      {value && (
        <button
          onClick={clear}
          aria-label="Clear search"
          className="absolute right-1.5 text-slate-500 hover:text-slate-300 text-xs leading-none"
        >
          ✕
        </button>
      )}
    </div>
  );
}

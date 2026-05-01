import { ClipboardSetText } from "../../wailsjs/runtime/runtime";

export async function writeToClipboard(text: string): Promise<void> {
  try {
    await ClipboardSetText(text);
  } catch {
    await navigator.clipboard?.writeText(text).catch(() => {});
  }
}

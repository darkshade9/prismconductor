import { AddWorkspaceForm } from "./AddWorkspaceForm";

export function AddWorkspaceModal({ onDone }: { onDone: () => void }) {
  return <AddWorkspaceForm onDone={onDone} />;
}

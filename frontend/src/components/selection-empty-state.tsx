import { useTranslation } from "react-i18next";

// SelectionEmptyState is the right-pane placeholder shown before a member or
// machine is selected. The members / machines rails used to each carry their
// own byte-identical copy of this; the shared version takes the i18n key so
// callers only differ by their label.
export function SelectionEmptyState({ messageKey }: { messageKey: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex h-full items-center justify-center p-8">
      <p className="text-sm text-control-light">{t(messageKey)}</p>
    </div>
  );
}

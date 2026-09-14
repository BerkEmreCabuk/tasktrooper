import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

/**
 * What a browser gets in place of the environment check.
 *
 * Not a disabled button and not a hidden step: both leave a person hunting for
 * what unlocks something that nothing in this tab ever can. The check probes
 * this machine through the desktop shell's bridge, so the honest answer is the
 * sentence.
 */
export function DesktopOnlyNotice() {
  const { t } = useI18n();
  return (
    <Notice variant="info" title={t("setup.desktopOnly.title")}>
      <p>{t("setup.desktopOnly.body")}</p>
    </Notice>
  );
}

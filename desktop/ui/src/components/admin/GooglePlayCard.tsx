import { Play } from "lucide-react";
import { type StoreCredentialView } from "@/api";
import { StoreAppsBrowser } from "@/components/admin/StoreAppPickerDialog";
import { StoreCredentialForm } from "@/components/admin/StoreCredentialsSection";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface GooglePlayCardProps {
  /** Undefined while the vault is still loading or could not be read. */
  credential?: StoreCredentialView;
  onChanged: () => void;
}

export function GooglePlayCard({ credential, onChanged }: GooglePlayCardProps) {
  const { t } = useI18n();
  return (
    <Card className="mt-4 w-full space-y-3 p-6">
      <Label className="flex items-center gap-2">
        <Play className="h-4 w-4" />
        {t("settingsPages.integrations.play.title")}
      </Label>
      <p className="text-sm text-muted-foreground">{t("settingsPages.integrations.play.description")}</p>
      <StoreCredentialForm provider="google_play" credential={credential} onChanged={onChanged} />
      <StoreAppsBrowser provider="google_play" configured={credential?.configured ?? false} />
    </Card>
  );
}

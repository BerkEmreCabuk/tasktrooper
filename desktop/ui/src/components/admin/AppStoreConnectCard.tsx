import { Apple } from "lucide-react";
import { type StoreCredentialView } from "@/api";
import { StoreAppsBrowser } from "@/components/admin/StoreAppPickerDialog";
import { StoreCredentialForm } from "@/components/admin/StoreCredentialsSection";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface AppStoreConnectCardProps {
  /** Undefined while the vault is still loading or could not be read. */
  credential?: StoreCredentialView;
  onChanged: () => void;
}

export function AppStoreConnectCard({ credential, onChanged }: AppStoreConnectCardProps) {
  const { t } = useI18n();
  return (
    <Card className="mt-4 w-full space-y-3 p-6">
      <Label className="flex items-center gap-2">
        <Apple className="h-4 w-4" />
        {t("settingsPages.integrations.asc.title")}
      </Label>
      <p className="text-sm text-muted-foreground">{t("settingsPages.integrations.asc.description")}</p>
      <StoreCredentialForm provider="asc" credential={credential} onChanged={onChanged} />
      <StoreAppsBrowser provider="asc" configured={credential?.configured ?? false} />
    </Card>
  );
}

import { useParams } from "react-router-dom";
import { DeploySettingsSection } from "@/components/projects/DeploySettingsSection";
import { PageHeader } from "@/components/admin/PageHeader";
import { useI18n } from "@/hooks/useI18n";

// DeploySettingsPage — the deploy section on a page of its own, for the
// operations matrix, which links straight here from a cell.
//
// The section itself is what the repository's own settings screen mounts under
// its Deploy tab: the addresses and the hosting link belong with the rest of
// that repository's configuration, and a second copy of the editor is how they
// drifted apart before.
export function DeploySettingsPage() {
  const { t } = useI18n();
  const { repositoryId = "" } = useParams();

  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader
        title={t("projectAdmin.prodOps.deployTitle")}
        description={t("projectAdmin.prodOps.deploySubtitle")}
      />

      <DeploySettingsSection repositoryId={repositoryId} />
    </div>
  );
}

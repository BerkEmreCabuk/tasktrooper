import { Rocket } from "lucide-react";
import { type MatrixCell, type MatrixRepo, type MatrixView } from "@/api";
import { DeploymentStatusCell } from "@/components/operations/DeploymentStatusCell";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";

interface DeploymentMatrixProps {
  view: MatrixView;
  onSelect: (repo: MatrixRepo, cell: MatrixCell) => void;
}

/**
 * DeploymentMatrix — repository x environment grid answering "what is live
 * where, right now" at a glance. A chronological feed can't answer that
 * question across every repository at once; this can, because every row is
 * a repository and every column is an environment.
 */
export function DeploymentMatrix({ view, onSelect }: DeploymentMatrixProps) {
  const { t } = useI18n();

  if (!view.repos || view.repos.length === 0) {
    return (
      <Card>
        <CardContent className="pt-6">
          <EmptyState icon={Rocket} title={t("operations.deployments.empty")} />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardContent className="pt-6">
        {/* Wide content (one column per env, across every repository) scrolls
            in its own container so the page body never scrolls horizontally. */}
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-3 font-medium">{t("operations.deployments.repository")}</th>
                {view.envs.map((env) => (
                  <th key={env} className="px-4 py-3 font-medium capitalize">
                    {env}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {view.repos.map((repo) => (
                <tr key={repo.id}>
                  <td className="px-4 py-3 align-top font-medium">{repo.name}</td>
                  {repo.cells.map((cell) => (
                    <td key={cell.env} className="px-2 py-2 align-top">
                      {/* The cell owns its own single interactive element — a
                          <button> that opens the drawer, or a <Link> straight
                          to deploy settings when unconfigured — so it never
                          nests a link inside a button. */}
                      <DeploymentStatusCell cell={cell} repositoryId={repo.id} onSelect={() => onSelect(repo, cell)} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}

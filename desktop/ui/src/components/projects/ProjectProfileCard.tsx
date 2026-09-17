import { CheckCircle2, FileText, RefreshCw, X } from "lucide-react";
import { useState } from "react";
import type { ProfileProposal, ProfileSection, RepositoryProfile } from "@/api";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { formatDate } from "@/lib/utils";

interface ProjectProfileCardProps {
  profile: RepositoryProfile | null;
  refreshing: boolean;
  onRefresh: () => void;
  onApplyProposal: (proposalId: string) => Promise<void>;
  onDismissProposal: (proposalId: string) => Promise<void>;
}

/**
 * The project profile as the platform actually stores it: parser-derived
 * sections (stack, commands, CI, deploy, git workflow) and agent-written
 * judgment sections, each with the files it was read from.
 *
 * Evidence is rendered, not hidden, because it is what separates this from the
 * generic advice the profile used to contain: a claim you can click through to
 * a file is a claim you can check.
 */
export function ProjectProfileCard({
  profile,
  refreshing,
  onRefresh,
  onApplyProposal,
  onDismissProposal,
}: ProjectProfileCardProps) {
  const { t } = useI18n();
  const [busyProposalId, setBusyProposalId] = useState<string | null>(null);

  const sections = profile?.sections ?? [];
  const pendingProposals = (profile?.proposals ?? []).filter((p) => p.status === "pending");
  const appliedProposals = (profile?.proposals ?? []).filter((p) => p.status === "applied");
  const staleCount = sections.filter((s) => s.stale).length;

  const runProposalAction = async (proposalId: string, action: (id: string) => Promise<void>) => {
    setBusyProposalId(proposalId);
    try {
      await action(proposalId);
    } finally {
      setBusyProposalId(null);
    }
  };

  return (
    <Card className="w-full space-y-4 p-6 xl:col-span-2">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold">{t("projectAdmin.projectSettings.profileTitle")}</h2>
          {staleCount > 0 && (
            <Badge variant="outline" className="border-amber-500/50 text-amber-500">
              {t("projectAdmin.projectSettings.profileStaleCount", { count: staleCount })}
            </Badge>
          )}
        </div>
        <Button variant="outline" size="sm" onClick={onRefresh} disabled={refreshing}>
          <RefreshCw className={refreshing ? "mr-2 h-4 w-4 animate-spin" : "mr-2 h-4 w-4"} />
          {refreshing
            ? t("projectAdmin.projectSettings.profileRefreshing")
            : t("projectAdmin.projectSettings.profileRefresh")}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">{t("projectAdmin.projectSettings.profileDesc")}</p>

      {pendingProposals.length > 0 && (
        <div className="space-y-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-4">
          <div>
            <h3 className="text-sm font-medium">{t("projectAdmin.projectSettings.proposalsTitle")}</h3>
            <p className="text-xs text-muted-foreground">
              {t("projectAdmin.projectSettings.proposalsDesc")}
            </p>
          </div>
          <div className="divide-y rounded-md border bg-background">
            {pendingProposals.map((proposal) => (
              <ProposalRow
                key={proposal.id}
                proposal={proposal}
                busy={busyProposalId === proposal.id}
                onApply={() => runProposalAction(proposal.id, onApplyProposal)}
                onDismiss={() => runProposalAction(proposal.id, onDismissProposal)}
              />
            ))}
          </div>
        </div>
      )}

      {appliedProposals.length > 0 && (
        <p className="text-xs text-muted-foreground">
          <CheckCircle2 className="mr-1 inline h-3 w-3 text-emerald-500" />
          {t("projectAdmin.projectSettings.proposalsAutoApplied", {
            fields: appliedProposals.map((p) => p.label).join(", "),
          })}
        </p>
      )}

      {profile?.profile_updated_at && (
        <p className="text-xs text-muted-foreground">
          {t("projectAdmin.projectSettings.profileUpdatedAt", { date: formatDate(profile.profile_updated_at) })}
        </p>
      )}

      {sections.length > 0 ? (
        <div className="max-h-[32rem] space-y-3 overflow-y-auto pr-1">
          {sections.map((section) => (
            <ProfileSectionBlock key={section.id || section.section} section={section} />
          ))}
        </div>
      ) : profile?.profile_md ? (
        <div className="max-h-96 overflow-y-auto rounded-md border border-border/60 p-4">
          <MarkdownContent content={profile.profile_md} />
        </div>
      ) : (
        <EmptyState
          icon={FileText}
          title={t("projectAdmin.projectSettings.profileEmptyTitle")}
          description={t("projectAdmin.projectSettings.profileEmptyDesc")}
        />
      )}
    </Card>
  );
}

function ProfileSectionBlock({ section }: { section: ProfileSection }) {
  const { t } = useI18n();
  // A section the dictionary does not know yet (backend added one, UI not
  // redeployed) falls back to its raw id rather than rendering the lookup key.
  const titleKey = `projectAdmin.projectSettings.profileSections.${section.section}`;
  const translated = t(titleKey);
  const title = translated === titleKey ? section.section : translated;
  return (
    <div className="rounded-md border border-border/60 p-4">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-medium">{title}</h3>
        <Badge variant="outline" className="text-micro uppercase">
          {t(`projectAdmin.projectSettings.profileOrigin.${section.origin}`)}
        </Badge>
        {section.stale && (
          <Badge variant="outline" className="border-amber-500/50 text-amber-500">
            {t("projectAdmin.projectSettings.profileStale")}
          </Badge>
        )}
      </div>
      <MarkdownContent content={section.body_md} />
      {(section.evidence?.length ?? 0) > 0 && (
        <p className="mt-2 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
          <span>{t("projectAdmin.projectSettings.profileEvidence")}</span>
          {section.evidence?.map((e) => (
            <code key={`${e.path}:${e.line ?? 0}`} className="rounded bg-muted px-1 py-0.5">
              {e.line ? `${e.path}:${e.line}` : e.path}
            </code>
          ))}
        </p>
      )}
    </div>
  );
}

function ProposalRow({
  proposal,
  busy,
  onApply,
  onDismiss,
}: {
  proposal: ProfileProposal;
  busy: boolean;
  onApply: () => void;
  onDismiss: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
      <div className="min-w-0">
        <p className="text-sm">{proposal.label}</p>
        <p className="text-xs text-muted-foreground">
          {proposal.current
            ? t("projectAdmin.projectSettings.proposalReplaces", { current: proposal.current })
            : t("projectAdmin.projectSettings.proposalFillsEmpty")}
          {(proposal.evidence?.length ?? 0) > 0 && ` · ${proposal.evidence?.map((e) => e.path).join(", ")}`}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Button size="sm" disabled={busy} onClick={onApply}>
          <CheckCircle2 className="mr-1.5 h-3.5 w-3.5" />
          {t("projectAdmin.projectSettings.proposalApply")}
        </Button>
        <Button variant="outline" size="sm" disabled={busy} onClick={onDismiss}>
          <X className="mr-1.5 h-3.5 w-3.5" />
          {t("projectAdmin.projectSettings.proposalDismiss")}
        </Button>
      </div>
    </div>
  );
}

import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { HostingPanel } from "./HostingPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { vercelProjectDetails, gcloudResourceDetails } = vi.hoisted(() => ({
  vercelProjectDetails: vi.fn(),
  gcloudResourceDetails: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, vercelProjectDetails, gcloudResourceDetails },
  };
});

function renderPanel(props: Parameters<typeof HostingPanel>[0]) {
  return render(
    <I18nProvider>
      <HostingPanel {...props} />
    </I18nProvider>,
  );
}

describe("HostingPanel provider choice per scope kind", () => {
  beforeEach(() => {
    // Radix's Select scrolls the active option into view; jsdom has no such
    // method and the omission surfaces as an unhandled error, not a failure.
    Element.prototype.scrollIntoView = vi.fn();
    vercelProjectDetails.mockReset();
    gcloudResourceDetails.mockReset();
    vercelProjectDetails.mockRejectedValue(Object.assign(new Error("not found"), { status: 404 }));
    gcloudResourceDetails.mockRejectedValue(Object.assign(new Error("not found"), { status: 404 }));
  });

  it("offers the hosted runtimes for a backend repository", async () => {
    renderPanel({ repositoryId: "repo-1", scopeKind: "backend" });

    expect(await screen.findByRole("button", { name: /Vercel/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Google Cloud/ })).toBeInTheDocument();
  });

  // A mobile scope publishes through a store console: the server refuses a
  // hosted-runtime target for it, so offering one here would be a dead end.
  it("offers no hosted runtime for a mobile repository", async () => {
    renderPanel({ repositoryId: "repo-1", scopeKind: "mobile" });

    await waitFor(() => expect(screen.queryByRole("button", { name: /Vercel/ })).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /Google Cloud/ })).not.toBeInTheDocument();
    expect(vercelProjectDetails).not.toHaveBeenCalled();
  });

});

import { MemoryManager } from "@/components/agent/MemoryManager";
import { PageContent } from "@/components/layout/PageContent";

export function SharedMemoryPage() {
  return (
    <PageContent>
      <MemoryManager mode="shared" />
    </PageContent>
  );
}

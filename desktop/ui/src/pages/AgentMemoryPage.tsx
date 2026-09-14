import { useParams } from "react-router-dom";
import { MemoryManager } from "@/components/agent/MemoryManager";

export function AgentMemoryPage() {
  const { agentId } = useParams();
  if (!agentId || agentId === "new") return null;
  return <MemoryManager mode="agent" agentId={agentId} />;
}

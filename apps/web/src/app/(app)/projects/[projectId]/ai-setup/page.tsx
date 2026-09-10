import { AIProjectSetup } from "@/features/ai-assistant/components/AIProjectSetup";

export default async function ProjectAISetupPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return <AIProjectSetup projectId={projectId} />;
}

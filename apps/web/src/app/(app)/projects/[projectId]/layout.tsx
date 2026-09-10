import { ProjectTabs } from "@/features/projects/components/ProjectTabs";

export default async function ProjectWorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return (
    <div className="flex flex-col gap-6">
      <ProjectTabs projectId={projectId} />
      {children}
    </div>
  );
}

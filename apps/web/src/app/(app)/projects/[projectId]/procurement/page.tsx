import { ProjectProcurement } from "@/features/procurement/components/ProjectProcurement";
export default async function ProcurementPage({ params }: { params: Promise<{ projectId: string }> }) { const { projectId } = await params; return <ProjectProcurement projectId={projectId} />; }

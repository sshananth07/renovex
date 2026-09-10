import { PropertyDetail } from "@/features/properties/components/PropertyDetail";

export default async function ProjectPropertyPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return <PropertyDetail projectId={projectId} />;
}

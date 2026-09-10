import { listClients, type Client } from "@/features/clients/api";
import { listProjects, type Project } from "@/features/projects/api";
import { listCostItems, listEstimates, listQuotations } from "@/features/operations/api";
import { listRFQs } from "@/features/procurement/api";

async function allPages<T>(load: (page: number, pageSize: number) => Promise<{ items?: T[] | null; total: number }>) {
  const pageSize=100; const first=await load(1, pageSize); const pages=Math.ceil(first.total/pageSize); const rest=await Promise.all(Array.from({length:Math.max(0,pages-1)},(_,i)=>load(i+2,pageSize))); return [first,...rest].flatMap((page)=>page.items ?? []);
}

export async function getDashboardData() {
  const [projects, clients] = await Promise.all([
    allPages<Project>((page, pageSize) => listProjects({ page, pageSize })),
    allPages<Client>((page, pageSize) => listClients({ page, pageSize })),
  ]);
  const projectDetails = await Promise.all(projects.map(async (project) => {
    const [costs, estimates, quotations, rfqs] = await Promise.all([listCostItems(project.id), listEstimates(project.id), listQuotations(project.id), listRFQs(project.id)]);
    return { project, costs, estimates, quotations, rfqs: rfqs ?? [] };
  }));
  return { projects, clients, projectDetails };
}

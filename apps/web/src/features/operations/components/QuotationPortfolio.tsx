"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { formatMoney } from "@/lib/formatting/money";
import { getDashboardData } from "@/features/dashboard/api";

export function QuotationPortfolio() {
  const data=useQuery({queryKey:["dashboard"],queryFn:getDashboardData});
  const rows=(data.data?.projectDetails ?? []).flatMap(({project,quotations})=>quotations.map((quotation)=>({project,quotation}))).sort((a,b)=>b.quotation.createdAt.localeCompare(a.quotation.createdAt));
  return <div className="flex flex-col gap-5"><PageHeader title="Quotations" description="Customer-facing quotation register across all projects" /><section className="surface-card overflow-hidden"><div className="table-scroll"><table className="w-full min-w-200 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Quotation</th><th>Project</th><th>Version</th><th>Status</th><th>Total</th><th /></tr></thead><tbody>{rows.map(({project,quotation})=><tr key={quotation.id} className="border-b last:border-0"><td className="p-3 font-mono font-semibold">{quotation.quotationNumber}</td><td>{project.name}</td><td>v{quotation.version}</td><td><Badge variant="outline" className="capitalize">{quotation.status}</Badge></td><td className="font-mono font-semibold">{formatMoney(quotation.total.amount,quotation.currency)}</td><td><Link className="font-semibold text-primary hover:underline" href={`/projects/${project.id}/quotations`}>Open</Link></td></tr>)}</tbody></table></div>{!data.isLoading&&rows.length===0&&<p className="p-12 text-center text-sm text-muted-foreground">No quotations yet.</p>}</section></div>;
}

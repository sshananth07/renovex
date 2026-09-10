"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { listPortfolioProcurement } from "../api";

export function ProcurementPortfolio() {
  const portfolio = useQuery({ queryKey: ["procurement", "portfolio"], queryFn: listPortfolioProcurement });
  const rows = portfolio.data ?? [];
  const open = rows.filter((row) => !["awarded", "closed", "cancelled"].includes(row.rfq.status)).length;
  const invited = rows.reduce((sum, row) => sum + row.invitationCount, 0);
  const offers = rows.reduce((sum, row) => sum + row.offerCount, 0);
  const awaiting = rows.reduce((sum, row) => sum + row.reviewRequiredCount, 0);
  const awards = rows.reduce((sum, row) => sum + row.awardCount, 0);
  return <div className="flex flex-col gap-5"><PageHeader title="Procurement" description="Portfolio RFQs, supplier responses and award work" /><div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">{[["Open RFQs", open],["Suppliers invited", invited],["Offers received", offers],["Awaiting review", awaiting],["Awards created", awards]].map(([label, value]) => <section key={label} className="surface-card p-5"><p className="eyebrow">{label}</p><p className="mt-3 text-2xl font-semibold">{value}</p></section>)}</div><section className="surface-card overflow-hidden"><div className="border-b p-4"><h2 className="font-heading font-semibold">RFQ register</h2><p className="text-xs text-muted-foreground">Complete portfolio view assembled from every project’s authoritative RFQ register.</p></div><div className="table-scroll"><table className="w-full min-w-240 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Reference</th><th>Project</th><th>Scope</th><th>Suppliers</th><th>Offers</th><th>Review</th><th>Awards</th><th>Status</th><th>Updated</th><th /></tr></thead><tbody>{rows.map(({ project, rfq, invitationCount, offerCount, reviewRequiredCount, awardCount }) => <tr key={rfq.id} className="border-b last:border-0"><td className="p-3 font-mono font-semibold text-primary">{rfq.rfqNumber}</td><td>{project.name}</td><td>{rfq.title || `${rfq.lines?.length ?? 0} line scope`}</td><td>{invitationCount}</td><td>{offerCount}</td><td>{reviewRequiredCount}</td><td>{awardCount}</td><td><Badge variant="outline" className="capitalize">{rfq.status}</Badge></td><td>{new Date(rfq.updatedAt).toLocaleDateString("en-MY")}</td><td><Link className="font-semibold text-primary hover:underline" href={`/projects/${project.id}/procurement`}>Open</Link></td></tr>)}</tbody></table></div>{!portfolio.isLoading && !portfolio.error && rows.length === 0 && <p className="p-12 text-center text-sm text-muted-foreground">No procurement records yet.</p>}{portfolio.error && <p className="p-6 text-sm text-destructive">The procurement register could not be loaded; portfolio totals are unavailable rather than approximated.</p>}</section></div>;
}

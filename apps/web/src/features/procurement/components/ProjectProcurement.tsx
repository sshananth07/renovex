"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Copy, Mail, MessageCircle, Plus, Send } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { applyFieldErrors } from "@/lib/api/applyFieldErrors";
import type { ApiError } from "@/lib/api/errors";
import { toRFC3339 } from "@/lib/formatting/date";
import { formatMoney } from "@/lib/formatting/money";
import { useMaterials } from "@/features/operations/queries";
import { useWorkItemList } from "@/features/work-items/queries";
import * as api from "../api";
import { useAcknowledgeUnitMismatch, useAddRequirementToRFQ, useArchiveRequirement, useDeleteRequirement, useReviewRequirement, useUpdateRFQ } from "../mutations";
import { useAwardDraft, useAwardRevisions, useComparison, useInvitations, useIssuedVersions, useRequirements, useRFQs, useSuppliers } from "../queries";
import { buildDuplicateCandidateKey, findSimilarRequirement } from "../requirementSimilarity";
import type { RequirementPrimaryAction, RequirementSecondaryAction, RequirementUIContext } from "../requirementPresentation";
import { deriveRFQReadinessState } from "../rfqPresentation";
import { translateProcurementError } from "../procurementErrors";
import { AwardHistory, FinalisedAwardView } from "./AwardResult";
import { RequirementCard } from "./RequirementCard";
import { ResolveDiscrepancyDialog } from "./ResolveDiscrepancyDialog";
import { SupplierForm, type SupplierValues } from "./SupplierForm";

// Every procurement mutation now throws a normalized ApiError (via
// unwrapOrThrow in api.ts), but React Query types mutation.error as the
// generic Error regardless — this narrows the unknown thrown value back,
// matching the same idiom ResolveDiscrepancyDialog uses for the same reason.
function toApiError(error: unknown): ApiError {
  if (typeof error === "object" && error !== null && "kind" in error) return error as ApiError;
  return { kind: "network" };
}

const createRFQSchema = z.object({
  title: z.string(),
  responseDeadline: z.string().min(1, "Response deadline is required"),
  supplierInstructions: z.string(),
});
type CreateRFQValues = z.infer<typeof createRFQSchema>;

// responseDeadline is fixed at RFQ creation and never re-edited here, so the
// cross-field check takes it as an argument rather than a form field — the
// schema is rebuilt each render against the currently-selected RFQ's
// responseDeadline (see editRFQForm's resolver) rather than captured once.
function buildEditRFQSchema(responseDeadline: string | undefined) {
  return z.object({
    deliveryAddress: z.string(),
    requiredByDate: z.string(),
  }).superRefine((values, ctx) => {
    if (!responseDeadline || !values.requiredByDate) return;
    const requiredBy = toRFC3339(values.requiredByDate);
    if (!requiredBy) return;
    if (new Date(requiredBy).getTime() < new Date(responseDeadline).getTime()) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["requiredByDate"],
        message: "The required delivery date must be after the supplier response deadline.",
      });
    }
  });
}
type EditRFQValues = z.infer<ReturnType<typeof buildEditRFQSchema>>;

export function ProjectProcurement({ projectId }: { projectId: string }) {
  const client = useQueryClient();
  const rfqs = useRFQs(projectId);
  const requirements = useRequirements(projectId);
  const materials = useMaterials();
  const suppliers = useSuppliers();
  const [selectedId, setSelectedId] = useState<string>();
  const selected = (rfqs.data ?? []).find((item) => item.id === selectedId) ?? rfqs.data?.[0];
  const rfqScopeRef = useRef<HTMLDivElement>(null);
  // Set by the view-rfq handler when navigating to a different RFQ; consumed
  // (and cleared) by the effect once that RFQ has actually rendered as
  // `selected`, so the scroll targets the newly-rendered scope section
  // rather than racing ahead of React committing the selection change.
  const pendingScrollToRfqIdRef = useRef<string>(undefined);
  // isPending only reflects reality after React commits the next render, so
  // two clicks landing within the same paint window (a fast physical
  // double-click, or two different cards clicked back-to-back) can both pass
  // the isPending check before either disabled button reaches the DOM. This
  // ref is set synchronously inside the click handler itself — no render in
  // between — so the second click is rejected immediately regardless of
  // render timing, with the backend's 409 as the remaining real-concurrency
  // backstop for genuinely separate browser sessions/tabs.
  const addToRfqInFlightRef = useRef(false);
  useEffect(() => {
    if (pendingScrollToRfqIdRef.current && selected?.id === pendingScrollToRfqIdRef.current) {
      rfqScopeRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
      pendingScrollToRfqIdRef.current = undefined;
    }
    // Switching the selected RFQ gives useAddRequirementToRFQ a fresh
    // mutation instance (isPending resets), so the synchronous in-flight
    // guard below must reset in step rather than staying stuck true.
    addToRfqInFlightRef.current = false;
  }, [selected?.id]);
  const versions = useIssuedVersions(selected?.id ?? "", selected?.status !== "draft");
  const issued = versions.data?.at(-1);
  const invitations = useInvitations(selected?.id ?? "");
  const comparison = useComparison(selected?.id ?? "", issued?.id ?? "");
  const awardRevisions = useAwardRevisions(selected?.id ?? "", issued?.id ?? "");
  const hasFinalisedAward = (awardRevisions.data ?? []).length > 0;
  // Award revisions are listed oldest-first (revisionNumber ascending) —
  // §8G: a correction always produces a new revision that supersedes the
  // last, so the latest entry is the current authoritative one to display.
  const latestAwardRevision = (awardRevisions.data ?? []).at(-1);
  const awardDraftQuery = useAwardDraft(selected?.id ?? "", issued?.id ?? "");
  const [createOpen, setCreateOpen] = useState(false); const [requirementOpen, setRequirementOpen] = useState(false); const [inviteOpen, setInviteOpen] = useState(false); const [supplierOpen, setSupplierOpen] = useState(false); const [editRfqOpen, setEditRfqOpen] = useState(false);
  const editRFQForm = useForm<EditRFQValues>({ resolver: zodResolver(buildEditRFQSchema(selected?.responseDeadline)), defaultValues: { deliveryAddress: "", requiredByDate: "" } });
  const updateRFQ = useUpdateRFQ(projectId, selected?.id ?? "");
  const submitEditRFQ = editRFQForm.handleSubmit((values) => {
    if (!selected) return;
    updateRFQ.mutate(
      { expectedRevision: selected.revision, deliveryAddress: values.deliveryAddress || undefined, requiredByDate: toRFC3339(values.requiredByDate) ?? undefined },
      { onSuccess: () => { setEditRfqOpen(false); toast.success("RFQ details updated."); } }
    );
  });
  const createRFQDefaults: CreateRFQValues = { title: "", responseDeadline: "", supplierInstructions: "" };
  const createRFQForm = useForm<CreateRFQValues>({ resolver: zodResolver(createRFQSchema), defaultValues: createRFQDefaults });
  const [createRFQFormError, setCreateRFQFormError] = useState<string>();
  const [materialId, setMaterialId] = useState(""); const [quantity, setQuantity] = useState("1"); const [unit, setUnit] = useState("unit");
  const [approvedDuplicateKey, setApprovedDuplicateKey] = useState<string>();
  const [supplierId, setSupplierId] = useState(""); const [recipientName, setRecipientName] = useState(""); const [recipientEmail, setRecipientEmail] = useState(""); const [expiresAt, setExpiresAt] = useState("");
  const [sharedLinks, setSharedLinks] = useState<Record<string, string>>({});
  const [unawardLineId, setUnawardLineId] = useState<string>();
  const [unawardReason, setUnawardReason] = useState("");
  const [unawardNote, setUnawardNote] = useState("");
  const invalidate = () => { client.invalidateQueries({ queryKey: ["projects", projectId] }); if (selected) client.invalidateQueries({ queryKey: ["rfq-chains", selected.id] }); };
  // RFQ creation is not idempotent (a retry allocates a new RFQ number and
  // creates a second empty draft) — retry is explicit here even though it
  // matches React Query's own mutation default, so the intent is auditable
  // rather than implicit.
  const create = useMutation({
    retry: false,
    mutationFn: (values: CreateRFQValues) => api.createRFQ(projectId, {
      title: values.title || undefined,
      responseDeadline: toRFC3339(values.responseDeadline) ?? undefined,
      supplierInstructions: values.supplierInstructions || undefined,
    }),
    onSuccess: (item) => { invalidate(); setSelectedId(item.id); setCreateOpen(false); createRFQForm.reset(createRFQDefaults); },
    onError: (error) => setCreateRFQFormError(applyFieldErrors(error as unknown as ApiError, createRFQForm.setError, ["title", "responseDeadline", "supplierInstructions"])),
  });
  const submitCreateRFQ = createRFQForm.handleSubmit((values) => { setCreateRFQFormError(undefined); create.mutate(values); });
  const createRequirement = useMutation({ mutationFn: () => api.createRequirement({ projectId, materialId, quantityValue: quantity, quantityUnit: unit }), onSuccess: () => { invalidate(); setRequirementOpen(false); setApprovedDuplicateKey(undefined); } });
  const duplicateCandidateKey = materialId ? buildDuplicateCandidateKey({ materialId, quantityUnit: unit }) : undefined;
  const similarRequirement = materialId ? findSimilarRequirement(requirements.data ?? [], { materialId, quantityUnit: unit }) : undefined;
  const showDuplicateWarning = Boolean(similarRequirement) && approvedDuplicateKey !== duplicateCandidateKey;
  function submitCreateRequirement() {
    if (showDuplicateWarning) return;
    createRequirement.mutate();
  }
  const workItems = useWorkItemList(projectId, { page: 1, pageSize: 100 });
  const workItemsById = useMemo(() => new Map((workItems.data?.items ?? []).map((item) => [item.id, item])), [workItems.data]);
  const reviewRequirement = useReviewRequirement(projectId);
  const archiveRequirement = useArchiveRequirement(projectId);
  const deleteRequirement = useDeleteRequirement(projectId);
  const acknowledgeUnitMismatch = useAcknowledgeUnitMismatch(projectId);
  // One shared mutation instance per selected RFQ: while it's pending, every
  // Add-to-RFQ button targeting this RFQ shares the same isPending state, so
  // clicking one naturally disables the others without extra bookkeeping.
  const addRequirementToRFQ = useAddRequirementToRFQ(projectId, selected?.id ?? "");
  const [discrepancyRequirementId, setDiscrepancyRequirementId] = useState<string>();
  const discrepancyRequirement = (requirements.data ?? []).find((item) => item.id === discrepancyRequirementId);
  const [removeRequirementTarget, setRemoveRequirementTarget] = useState<{ requirementId: string; expectedRevision: number }>();
  const requirementContext: RequirementUIContext = useMemo(
    () => ({
      selectedRfq: selected ? { id: selected.id, number: selected.rfqNumber, status: selected.status, revision: selected.revision } : undefined,
    }),
    [selected]
  );
  function handleRequirementSecondaryAction(action: RequirementSecondaryAction) {
    switch (action.type) {
      case "reject":
        setRemoveRequirementTarget({ requirementId: action.requirementId, expectedRevision: action.expectedRevision });
        return;
    }
  }
  function handleRequirementAction(action: RequirementPrimaryAction) {
    switch (action.type) {
      case "review":
        reviewRequirement.mutate(
          { requirementId: action.requirementId, expectedRevision: action.expectedRevision },
          { onSuccess: () => toast.success("Requirement confirmed.") }
        );
        return;
      case "acknowledge-unit":
        acknowledgeUnitMismatch.mutate(
          { requirementId: action.requirementId, expectedRevision: action.expectedRevision },
          { onSuccess: () => toast.success("Unit mismatch acknowledged.") }
        );
        return;
      case "resolve-discrepancy":
        setDiscrepancyRequirementId(action.requirementId);
        return;
      case "add-to-rfq":
        if (!selected || addToRfqInFlightRef.current) return;
        addToRfqInFlightRef.current = true;
        addRequirementToRFQ.mutate(
          {
            materialRequirementId: action.requirementId,
            expectedRequirementRevision: action.expectedRequirementRevision,
            expectedRfqRevision: selected.revision,
          },
          {
            onSuccess: () => toast.success(`Requirement added to ${selected.rfqNumber}.`),
            onSettled: () => { addToRfqInFlightRef.current = false; },
          }
        );
        return;
      case "view-rfq":
        if (action.isSelectedRfq) {
          rfqScopeRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
        } else {
          pendingScrollToRfqIdRef.current = action.rfqChainId;
          setSelectedId(action.rfqChainId);
        }
        return;
      case "view-split-children":
        // Read-only display; split-creation UI is a separate follow-up.
        return;
    }
  }
  const generateRequirements = useMutation({
    mutationFn: () => api.generateRequirementsFromCosts(projectId),
    onSuccess: (result) => {
      client.invalidateQueries({ queryKey: ["projects", projectId] });
      const parts = [
        result.createdCount ? `${result.createdCount} created` : undefined,
        result.discrepancyCount ? `${result.discrepancyCount} source changed` : undefined,
        result.sourceRemovedCount ? `${result.sourceRemovedCount} source removed` : undefined,
      ].filter(Boolean);
      toast.success(parts.length ? `Synced from project costs: ${parts.join(", ")}.` : "Synced from project costs: no changes.");
    },
  });
  const ready = useMutation({ mutationFn: () => api.readyRFQ(selected!.id, selected!.revision), onSuccess: invalidate });
  const reopen = useMutation({ mutationFn: () => api.reopenRFQ(selected!.id, selected!.revision), onSuccess: () => { invalidate(); toast.success("RFQ reopened to draft."); } });
  const issue = useMutation({ mutationFn: () => api.issueRFQ(selected!.id), onSuccess: invalidate });
  const invite = useMutation({ mutationFn: () => api.createInvitation(selected!.id, { supplierId, recipientName, recipientEmail, expiresAt: new Date(expiresAt).toISOString() }), onSuccess: () => { invalidate(); setInviteOpen(false); } });
  const send = useMutation({ mutationFn: (invitationId: string) => api.sendInvitation(selected!.id, invitationId), onSuccess: invalidate });
  const copyLink = useMutation({ mutationFn: (invitationId: string) => api.copyInvitationLink(selected!.id, invitationId), onSuccess: (result, invitationId) => { setSharedLinks((current) => ({ ...current, [invitationId]: result.url })); navigator.clipboard.writeText(result.url); } });
  const addSupplier = useMutation({ mutationFn: (values: SupplierValues) => api.createSupplier({ name: values.name, contactPerson: values.contactPerson || undefined, email: values.email || undefined, phone: values.phone || undefined, address: values.address || undefined, materialCategories: values.categories.split(",").map((value) => value.trim()).filter(Boolean), notes: values.notes || undefined }), onSuccess: (created) => { client.invalidateQueries({ queryKey: ["suppliers"] }); setSupplierId(created.id); setRecipientName(created.contactPerson || created.name); setRecipientEmail(created.email ?? ""); setSupplierOpen(false); } });
  // hasFinalisedAward already keeps the UI from calling these once an Award
  // is published (Award gates and lineage claims are terminal — M8 design
  // spec §8A.1A), so a mutation error here is always a genuine race (someone
  // else finalised, or the draft's revision moved) rather than the expected
  // "clicked after finalisation" case, and a toast is the right surface for it.
  const selectLine = useMutation({
    mutationFn: async ({ lineId, offerVersionId, offerLineId }: { lineId: string; offerVersionId: string; offerLineId: string }) => {
      const draft = awardDraftQuery.data ?? await api.createAwardDraft(selected!.id, issued!.id);
      return api.selectAwardLine(selected!.id, issued!.id, lineId, { offerVersionId, offerLineId, expectedRevision: draft.revision });
    },
    onSuccess: invalidate,
    onError: (error) => toast.error(translateProcurementError(toApiError(error)).message),
  });
  const unawardLine = useMutation({
    mutationFn: async ({ lineId, reason, note }: { lineId: string; reason: string; note?: string }) => {
      const draft = awardDraftQuery.data ?? await api.createAwardDraft(selected!.id, issued!.id);
      return api.unawardLine(selected!.id, issued!.id, lineId, { reason: reason as never, note, expectedRevision: draft.revision });
    },
    onSuccess: () => { invalidate(); setUnawardLineId(undefined); setUnawardReason(""); setUnawardNote(""); },
    onError: (error) => toast.error(translateProcurementError(toApiError(error)).message),
  });
  const finalize = useMutation({
    mutationFn: () => api.finalizeAward(selected!.id, issued!.id),
    onSuccess: () => { invalidate(); toast.success("Award finalised."); },
    onError: (error) => toast.error(translateProcurementError(toApiError(error)).message),
  });
  const readiness = selected ? deriveRFQReadinessState(selected) : undefined;
  const selectedSupplier = (suppliers.data ?? []).find((item) => item.id === supplierId);
  const queryError = rfqs.error ?? requirements.error ?? materials.error ?? suppliers.error ?? versions.error ?? invitations.error ?? comparison.error ?? awardRevisions.error;
  const mutationError = createRequirement.error ?? generateRequirements.error ?? ready.error ?? reopen.error ?? issue.error ?? invite.error ?? send.error ?? copyLink.error ?? addSupplier.error ?? selectLine.error ?? unawardLine.error ?? finalize.error ?? updateRFQ.error ?? reviewRequirement.error ?? acknowledgeUnitMismatch.error ?? archiveRequirement.error ?? deleteRequirement.error;
  const mutationErrorMessage = mutationError ? translateProcurementError(toApiError(mutationError)).message : undefined;

  return <div className="flex flex-col gap-5"><PageHeader title="Procurement" description="Project RFQs, supplier invitations, offer comparison and awards" actions={<div className="flex gap-2"><Button variant="outline" onClick={() => setRequirementOpen(true)}><Plus />Requirement</Button><Button onClick={() => setCreateOpen(true)}><Plus />Create RFQ</Button></div>} />
    {(queryError || mutationError) && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">{mutationErrorMessage ?? "Project procurement data could not be loaded. Retry after checking the API connection."}</div>}
    <div className="grid gap-4 lg:grid-cols-[19rem_minmax(0,1fr)]"><aside className="flex flex-col gap-2">{(rfqs.data ?? []).map((rfq) => <button key={rfq.id} className={`surface-card p-4 text-left ${rfq.id === selected?.id ? "border-primary bg-accent/30" : ""}`} onClick={() => setSelectedId(rfq.id)}><span className="flex items-center justify-between gap-2"><strong>{rfq.rfqNumber}</strong><Badge variant="outline" className="capitalize">{rfq.status}</Badge></span><span className="mt-2 block text-sm">{rfq.title || "Untitled RFQ"}</span><span className="mt-1 block text-xs text-muted-foreground">{rfq.lines?.length ?? 0} scope lines</span></button>)}{!rfqs.isLoading && (rfqs.data ?? []).length === 0 && <div className="surface-card p-8 text-center text-sm text-muted-foreground">No RFQs for this project.</div>}</aside>
    <div className="min-w-0 space-y-4">{selected ? <><section className="surface-card p-5"><div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex items-center gap-2"><h2 className="font-heading text-xl font-semibold">{selected.rfqNumber}</h2><Badge variant="outline" className="capitalize">{selected.status}</Badge></div><p className="mt-1 text-sm text-muted-foreground">{selected.title || "Procurement scope"} · Revision {selected.revision}</p>{!versions.isLoading && !issued && <p className="mt-1 text-xs text-muted-foreground">No issued versions yet</p>}</div><div className="flex flex-col items-end gap-2">{selected.status === "draft" && <><Button onClick={() => ready.mutate()} disabled={!readiness?.canAttemptMarkReady || ready.isPending}>{ready.isPending ? "Marking ready…" : "Mark ready"}</Button>{readiness?.message && <p className="text-xs text-muted-foreground">{readiness.message}</p>}<Button variant="outline" size="sm" onClick={() => { editRFQForm.reset({ deliveryAddress: selected.deliveryAddress ?? "", requiredByDate: selected.requiredByDate ?? "" }); setEditRfqOpen(true); }}>Edit RFQ details</Button></>}<div className="flex flex-wrap gap-2">{selected.status === "ready" && <>{!issued && <Button variant="outline" onClick={() => reopen.mutate()} disabled={reopen.isPending}>{reopen.isPending ? "Reopening…" : "Reopen draft"}</Button>}<Button onClick={() => issue.mutate()}>Issue RFQ</Button></>}{issued && <Button onClick={() => setInviteOpen(true)}><Send />Invite supplier</Button>}</div></div></div></section>
      <section ref={rfqScopeRef} className="surface-card overflow-hidden"><div className="flex items-center justify-between border-b p-4"><div><h3 className="font-heading font-semibold">RFQ scope</h3><p className="text-xs text-muted-foreground">Material Requirements are claimed into immutable issued scope.</p></div></div><div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Material</th><th>Quantity</th><th>Specification</th></tr></thead><tbody>{(selected.lines ?? []).map((line) => <tr key={line.id} className="border-b last:border-0"><td className="p-3 font-medium">{line.materialName}</td><td className="font-mono">{line.quantity.value} {line.quantity.unit}</td><td>{line.specification || "—"}</td></tr>)}</tbody></table></div></section>
      <section className="surface-card overflow-hidden"><div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:justify-between"><div><h3 className="font-heading font-semibold">Material requirements</h3><p className="text-xs text-muted-foreground">Review and unit/source-change resolution are required before a requirement can be added to this RFQ.</p></div><div className="flex flex-col items-start gap-1 sm:items-end"><Button variant="outline" size="sm" onClick={() => generateRequirements.mutate()} disabled={generateRequirements.isPending}>{generateRequirements.isPending ? "Syncing…" : "Sync from project costs"}</Button><p className="text-xs text-muted-foreground">Creates or updates requirements from material-linked, Work Item-linked project costs.</p></div></div>
        {(requirements.data ?? []).length === 0
          ? <p className="p-8 text-center text-sm text-muted-foreground">No material requirements for this project.</p>
          : <div className="grid gap-3 p-4 sm:grid-cols-2">{(requirements.data ?? []).map((requirement) => <RequirementCard
              key={requirement.id}
              requirement={requirement}
              context={{ ...requirementContext, workItemName: requirement.workItemId ? workItemsById.get(requirement.workItemId)?.description : undefined }}
              pending={
                (reviewRequirement.isPending && reviewRequirement.variables?.requirementId === requirement.id) ||
                (acknowledgeUnitMismatch.isPending && acknowledgeUnitMismatch.variables?.requirementId === requirement.id) ||
                (archiveRequirement.isPending && archiveRequirement.variables?.requirementId === requirement.id) ||
                // Every Add-to-RFQ action targeting the currently-selected
                // RFQ is disabled while any one of them is in flight — the
                // mutation is shared per selected RFQ, not per requirement,
                // to avoid two cards racing stale RFQ revisions against each
                // other (design spec §11).
                (requirement.status === "reviewed" && addRequirementToRFQ.isPending)
              }
              onAction={handleRequirementAction}
              onSecondaryAction={handleRequirementSecondaryAction}
            />)}</div>}
      </section>
      {issued && <section className="surface-card overflow-hidden"><div className="border-b p-4"><h3 className="font-heading font-semibold">Supplier invitations</h3><p className="text-xs text-muted-foreground">Send by system email, copy the secure link, or share the same link through WhatsApp.</p></div><div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Recipient</th><th>Status</th><th>Expires</th><th>Share</th></tr></thead><tbody>{(invitations.data ?? []).map((item) => <tr key={item.id} className="border-b last:border-0"><td className="p-3 font-medium">{item.recipientName}<span className="block text-xs text-muted-foreground">{item.recipientEmail}</span></td><td><Badge variant="outline" className="capitalize">{item.status}</Badge></td><td>{new Date(item.expiresAt).toLocaleDateString("en-MY")}</td><td><div className="flex gap-1"><Button variant="ghost" size="icon-sm" aria-label="Send email" onClick={() => send.mutate(item.id)}><Mail /></Button><Button variant="ghost" size="icon-sm" aria-label="Copy secure link" onClick={() => copyLink.mutate(item.id)}><Copy /></Button>{sharedLinks[item.id] && <a className="inline-flex size-8 items-center justify-center rounded-md hover:bg-muted" aria-label="Share secure link in WhatsApp" href={`https://wa.me/?text=${encodeURIComponent(`Please respond to this RFQ using your secure link: ${sharedLinks[item.id]}`)}`} target="_blank" rel="noreferrer"><MessageCircle className="size-4" /></a>}</div></td></tr>)}</tbody></table></div></section>}
      {comparison.data && issued && (
        latestAwardRevision
          ? <FinalisedAwardView
              revision={latestAwardRevision}
              invitations={invitations.data ?? []}
              rfqChainId={selected.id}
              versionId={issued.id}
              rfqNumber={selected.rfqNumber}
              rfqTitle={selected.title}
              comparison={<ComparisonPanel comparison={comparison.data} issued={issued} draft={awardRevisionAsDraft(latestAwardRevision)} finalised selecting={false} onSelect={() => {}} onUnaward={() => {}} onFinalize={() => {}} />}
            />
          : <ComparisonPanel comparison={comparison.data} issued={issued} draft={awardDraftQuery.data} finalised={false} selecting={selectLine.isPending} onSelect={(lineId, offerVersionId, offerLineId) => selectLine.mutate({ lineId, offerVersionId, offerLineId })} onUnaward={(lineId) => setUnawardLineId(lineId)} onFinalize={() => finalize.mutate()} />
      )}
      {(awardRevisions.data ?? []).length > 0 && <AwardHistory revisions={awardRevisions.data ?? []} />}
    </> : <div className="surface-card flex min-h-72 items-center justify-center text-sm text-muted-foreground">Select or create an RFQ.</div>}</div></div>

    <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); setCreateRFQFormError(undefined); if (!open) createRFQForm.reset(createRFQDefaults); }}><DialogContent><DialogHeader><DialogTitle>Create RFQ</DialogTitle></DialogHeader><form onSubmit={submitCreateRFQ} className="grid gap-4">
      {createRFQFormError && <p className="text-sm text-destructive">{createRFQFormError}</p>}
      <div className="grid gap-1.5"><Label>Title</Label><Input {...createRFQForm.register("title")} /></div>
      <div className="grid gap-1.5"><Label htmlFor="create-rfq-response-deadline">Response deadline</Label><Input id="create-rfq-response-deadline" type="datetime-local" {...createRFQForm.register("responseDeadline")} />{createRFQForm.formState.errors.responseDeadline?.message && <p className="text-xs text-destructive">{createRFQForm.formState.errors.responseDeadline.message}</p>}</div>
      <div className="grid gap-1.5"><Label>Supplier instructions</Label><Textarea {...createRFQForm.register("supplierInstructions")} /></div>
      <Button type="submit" disabled={create.isPending}>{create.isPending ? "Creating…" : "Create draft RFQ"}</Button>
    </form></DialogContent></Dialog>
    <Dialog open={requirementOpen} onOpenChange={(open) => { setRequirementOpen(open); if (!open) setApprovedDuplicateKey(undefined); }}><DialogContent><DialogHeader><DialogTitle>Add Material Requirement</DialogTitle></DialogHeader><div className="grid gap-4"><div className="grid gap-1.5"><Label>Material</Label><Select value={materialId} onValueChange={(v) => { setMaterialId(v ?? ""); const material=(materials.data ?? []).find((item) => item.id === v); if(material) setUnit(material.unit); }}><SelectTrigger><SelectValue placeholder="Select material" /></SelectTrigger><SelectContent>{(materials.data ?? []).map((item) => <SelectItem value={item.id} key={item.id}>{item.name}</SelectItem>)}</SelectContent></Select></div><div className="grid grid-cols-2 gap-4"><div className="grid gap-1.5"><Label htmlFor="requirement-quantity">Quantity</Label><Input id="requirement-quantity" value={quantity} onChange={(e) => setQuantity(e.target.value)} /></div><div className="grid gap-1.5"><Label htmlFor="requirement-unit">Unit</Label><Input id="requirement-unit" value={unit} onChange={(e) => setUnit(e.target.value)} /><p className="text-xs text-muted-foreground">Unit only, e.g. bag, kg, m²</p></div></div>
      {showDuplicateWarning && similarRequirement && <div className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm"><p className="font-medium">Similar requirement already exists</p><p className="mt-1 text-muted-foreground">{similarRequirement.materialName} · {similarRequirement.requiredQuantity.value} {similarRequirement.requiredQuantity.unit}{similarRequirement.specification ? ` · ${similarRequirement.specification}` : ""} · {similarRequirement.status}</p><p className="mt-2 text-xs text-muted-foreground">You can still create another requirement if this represents separate procurement demand.</p><div className="mt-3 flex gap-2"><Button size="sm" variant="outline" onClick={() => setRequirementOpen(false)}>View existing</Button><Button size="sm" onClick={() => setApprovedDuplicateKey(duplicateCandidateKey)}>Create another anyway</Button></div></div>}
      <Button disabled={!materialId || createRequirement.isPending} onClick={submitCreateRequirement}>{createRequirement.isPending ? "Creating…" : "Create requirement"}</Button>
    </div></DialogContent></Dialog>
    <Dialog open={inviteOpen} onOpenChange={setInviteOpen}><DialogContent><DialogHeader><DialogTitle>Invite supplier</DialogTitle></DialogHeader><div className="grid gap-4"><div className="grid gap-1.5"><div className="flex items-center justify-between"><Label>Supplier</Label><Button variant="ghost" size="sm" onClick={() => setSupplierOpen(true)}><Plus />Add supplier</Button></div><Select value={supplierId} onValueChange={(v) => { const id=v ?? ""; setSupplierId(id); const supplier=(suppliers.data ?? []).find((item) => item.id === id); if(supplier){setRecipientName(supplier.contactPerson || supplier.name); setRecipientEmail(supplier.email ?? "");} }}><SelectTrigger><SelectValue placeholder="Select supplier" /></SelectTrigger><SelectContent>{(suppliers.data ?? []).filter((item) => item.active).map((item) => <SelectItem value={item.id} key={item.id}>{item.name}</SelectItem>)}</SelectContent></Select></div>{selectedSupplier && <p className="text-xs text-muted-foreground">{selectedSupplier.materialCategories?.join(" · ") || "No categories recorded"}</p>}<div className="grid gap-1.5"><Label>Recipient name</Label><Input value={recipientName} onChange={(e) => setRecipientName(e.target.value)} /></div><div className="grid gap-1.5"><Label>Recipient email</Label><Input type="email" value={recipientEmail} onChange={(e) => setRecipientEmail(e.target.value)} /></div><div className="grid gap-1.5"><Label htmlFor="invite-access-expires">Access expires</Label><Input id="invite-access-expires" type="datetime-local" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} /></div><Button disabled={!supplierId || !recipientName || !recipientEmail || !expiresAt} onClick={() => invite.mutate()}>Create invitation</Button></div></DialogContent></Dialog>
    <Dialog open={supplierOpen} onOpenChange={setSupplierOpen}><DialogContent><DialogHeader><DialogTitle>Add supplier to directory</DialogTitle></DialogHeader><SupplierForm pending={addSupplier.isPending} onSubmit={(values) => addSupplier.mutate(values)} /></DialogContent></Dialog>
    <Dialog open={editRfqOpen} onOpenChange={setEditRfqOpen}><DialogContent><DialogHeader><DialogTitle>Edit RFQ details</DialogTitle></DialogHeader><form onSubmit={submitEditRFQ} className="grid gap-4">
      <div className="grid gap-1.5"><Label htmlFor="rfq-delivery-address">Delivery address</Label><Textarea id="rfq-delivery-address" {...editRFQForm.register("deliveryAddress")} /></div>
      <div className="grid gap-1.5"><Label htmlFor="rfq-required-by-date">Required by date</Label><Input id="rfq-required-by-date" type="datetime-local" {...editRFQForm.register("requiredByDate")} />{editRFQForm.formState.errors.requiredByDate?.message && <p className="text-xs text-destructive">{editRFQForm.formState.errors.requiredByDate.message}</p>}</div>
      <Button type="submit" disabled={updateRFQ.isPending}>{updateRFQ.isPending ? "Saving…" : "Save RFQ details"}</Button>
    </form></DialogContent></Dialog>
    {discrepancyRequirement && <ResolveDiscrepancyDialog open requirement={discrepancyRequirement} onClose={() => setDiscrepancyRequirementId(undefined)} onResolved={() => setDiscrepancyRequirementId(undefined)} />}
    <Dialog open={Boolean(unawardLineId)} onOpenChange={(open) => { if (!open) { setUnawardLineId(undefined); setUnawardReason(""); setUnawardNote(""); } }}><DialogContent><DialogHeader><DialogTitle>Unselect this line</DialogTitle></DialogHeader><div className="grid gap-4">
      <div className="grid gap-1.5"><Label>Reason</Label><Select value={unawardReason} onValueChange={(v) => setUnawardReason(v ?? "")}><SelectTrigger><SelectValue placeholder="Select a reason" /></SelectTrigger><SelectContent>
        <SelectItem value="no_acceptable_offer">No acceptable offer</SelectItem>
        <SelectItem value="purchase_deferred">Purchase deferred</SelectItem>
        <SelectItem value="scope_cancelled">Scope cancelled</SelectItem>
        <SelectItem value="retender_required">Retender required</SelectItem>
        <SelectItem value="other">Other</SelectItem>
      </SelectContent></Select></div>
      {unawardReason === "other" && <div className="grid gap-1.5"><Label htmlFor="unaward-note">Note</Label><Textarea id="unaward-note" value={unawardNote} onChange={(e) => setUnawardNote(e.target.value)} /></div>}
      <Button disabled={!unawardReason || unawardLine.isPending} onClick={() => unawardLineId && unawardLine.mutate({ lineId: unawardLineId, reason: unawardReason, note: unawardNote || undefined })}>{unawardLine.isPending ? "Saving…" : "Unselect line"}</Button>
    </div></DialogContent></Dialog>
    <Dialog open={Boolean(removeRequirementTarget)} onOpenChange={(open) => { if (!open) setRemoveRequirementTarget(undefined); }}><DialogContent><DialogHeader><DialogTitle>Remove this requirement?</DialogTitle></DialogHeader><div className="grid gap-3">
      <div className="rounded-lg border p-3"><p className="text-sm font-medium">Archive</p><p className="mt-1 text-xs text-muted-foreground">Keeps the record for reference, marked archived. Reversible in spirit — nothing is destroyed.</p><Button variant="outline" size="sm" className="mt-3" disabled={archiveRequirement.isPending || deleteRequirement.isPending} onClick={() => removeRequirementTarget && archiveRequirement.mutate(removeRequirementTarget, { onSuccess: () => { toast.success("Requirement archived."); setRemoveRequirementTarget(undefined); } })}>{archiveRequirement.isPending ? "Archiving…" : "Archive"}</Button></div>
      <div className="rounded-lg border border-destructive/30 p-3"><p className="text-sm font-medium text-destructive">Delete permanently</p><p className="mt-1 text-xs text-muted-foreground">Removes the requirement entirely. This cannot be undone.</p><Button variant="destructive" size="sm" className="mt-3" disabled={archiveRequirement.isPending || deleteRequirement.isPending} onClick={() => removeRequirementTarget && deleteRequirement.mutate(removeRequirementTarget, { onSuccess: () => { toast.success("Requirement deleted."); setRemoveRequirementTarget(undefined); } })}>{deleteRequirement.isPending ? "Deleting…" : "Delete permanently"}</Button></div>
    </div></DialogContent></Dialog>
  </div>;
}

// Once an Award is finalised, its draft is consumed and useAwardDraft
// returns null — but the read-only comparison replay still needs to know
// which line went to which offer, to mark it "✓ Awarded". This projects the
// finalised revision's own awardedLines into the same lineDecisions shape
// ComparisonPanel already reads, so it needs no finalised-specific branch of
// its own for "what was selected".
function awardRevisionAsDraft(revision: api.AwardRevision): Pick<api.AwardDraft, "lineDecisions"> {
  return {
    lineDecisions: (revision.awardedLines ?? []).map((line) => ({
      issuedRfqLineId: line.issuedRfqLineId,
      stableLineageId: line.stableLineageId,
      decision: "selected",
      offerVersionId: line.offerVersionId,
      offerLineId: line.offerLineId,
    })),
  };
}

function ComparisonPanel({ comparison, issued, draft, finalised, selecting, onSelect, onUnaward, onFinalize }: { comparison: api.Comparison; issued: api.IssuedVersion; draft?: Pick<api.AwardDraft, "lineDecisions"> | null; finalised: boolean; selecting: boolean; onSelect: (lineId: string, offerVersionId: string, offerLineId: string) => void; onUnaward: (lineId: string) => void; onFinalize: () => void }) {
  const offers = comparison.offers ?? [];
  const lineMap = new Map((issued.lines ?? []).map((line) => [line.id, line]));
  const allLines = offers.flatMap((offer) => (offer.lines ?? []).map((line) => ({ offer, line })));
  const lineIds = [...new Set(allLines.map(({ line }) => line.issuedRfqLineId))];
  const selectedOffers = new Map((draft?.lineDecisions ?? []).filter((decision) => decision.offerVersionId).map((decision) => [decision.issuedRfqLineId, decision.offerVersionId]));
  const selectableTotals = offers.filter((offer) => offer.selectable).map((offer) => offer.indicativeGrandTotal.amount.amount);
  const lowestTotal = selectableTotals.length ? Math.min(...selectableTotals) : undefined;
  return <section className="surface-card overflow-hidden">
    <div className="flex items-center justify-between border-b p-4">
      <div><h3 className="font-heading font-semibold">Offer comparison</h3><p className="text-xs text-muted-foreground">{finalised ? "The prices and offers Supplier submitted at the time of Award." : "Prices are authoritative Supplier submissions; every Award selection remains explicit."}</p></div>
      {!finalised && draft && <Button onClick={onFinalize}>Create award</Button>}
    </div>
    <div className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-3">{offers.map((offer) => {
      const selectedCount = [...selectedOffers.values()].filter((id) => id === offer.offerVersionId).length;
      const isLowest = offer.selectable && offer.indicativeGrandTotal.amount.amount === lowestTotal;
      return <article className={`rounded-xl border p-4 ${selectedCount ? "border-primary bg-accent/20" : ""}`} key={offer.offerVersionId}>
        <div className="flex items-start justify-between gap-2"><div><p className="font-semibold">{offer.supplierName || "Unnamed supplier"}</p><p className="text-xs text-muted-foreground">Offer v{offer.versionNumber}</p></div><div className="flex flex-wrap justify-end gap-1">{isLowest && <Badge className="bg-emerald-100 text-emerald-700">Lowest total</Badge>}{selectedCount > 0 && <Badge>{selectedCount} {finalised ? "awarded" : "selected"}</Badge>}</div></div>
        <p className="mt-3 font-mono text-lg font-semibold">{formatMoney(offer.indicativeGrandTotal.amount.amount, comparison.currency)}</p>
        <div className="mt-2 grid gap-1 text-xs text-muted-foreground"><p>Quoted subtotal {formatMoney(offer.indicativeQuotedSubtotal.amount.amount, comparison.currency)}</p><p>Offer tax {formatMoney(offer.offerLevelTaxAmount.amount, offer.offerLevelTaxAmount.currency)}</p><p>Valid until {new Date(offer.validUntil).toLocaleDateString("en-MY")}</p></div>
        {offer.supplierNotes && <p className="mt-3 text-xs text-muted-foreground">{offer.supplierNotes}</p>}
        {!offer.selectable && <Badge variant="outline" className="mt-3">Incomplete or review required</Badge>}
      </article>;
    })}</div>
    <div className="table-scroll"><table className="w-full min-w-240 text-sm"><thead><tr className="border-y bg-muted/40 text-left"><th className="p-3">Scope line</th><th>Quantity</th>{offers.map((offer) => <th className="px-2" key={offer.offerVersionId}><span className="block">{offer.supplierName || "Unnamed supplier"}</span><span className="block text-[11px] font-normal text-muted-foreground">Offer v{offer.versionNumber}</span></th>)}</tr></thead><tbody>{lineIds.map((lineId) => {
      const priced = offers.map((offer) => (offer.lines ?? []).find((item) => item.issuedRfqLineId === lineId)).filter((line) => line?.selectable && line.unitPrice).map((line) => line!.unitPrice!.amount);
      const lowestLine = priced.length ? Math.min(...priced) : undefined;
      const scope = lineMap.get(lineId);
      return <tr className="border-b last:border-0" key={lineId}>
        <td className="p-3 font-medium">{scope?.materialName ?? "RFQ line"}<span className="block max-w-xs text-xs font-normal text-muted-foreground">{scope?.specification}</span></td>
        <td className="font-mono">{scope ? `${scope.quantity.value} ${scope.quantity.unit}` : "—"}</td>
        {offers.map((offer) => {
          const line = (offer.lines ?? []).find((item) => item.issuedRfqLineId === lineId);
          const selected = selectedOffers.get(lineId) === offer.offerVersionId;
          const isLowest = line?.unitPrice?.amount === lowestLine;
          if (!line?.selectable || !line.unitPrice) {
            return <td className="px-2" key={offer.offerVersionId}><span className="text-xs capitalize text-muted-foreground">{line?.responseStatus?.replaceAll("_", " ") ?? "No response"}</span></td>;
          }
          if (finalised) {
            // Read-only, result-oriented: never a disabled control masquerading
            // as an interactive one. Awarded lines are visually distinguished
            // from the rest of this Supplier's (non-awarded) priced lines.
            return <td className="px-2" key={offer.offerVersionId}><div className="flex flex-col items-start gap-1">
              {selected
                ? <span className="inline-flex items-center gap-1 rounded-md bg-emerald-100 px-2 py-1 text-xs font-medium text-emerald-700">✓ Awarded</span>
                : <span className="font-mono text-sm text-muted-foreground">{formatMoney(line.unitPrice.amount, line.unitPrice.currency)}</span>}
              <span className="font-mono text-xs text-muted-foreground">{formatMoney(line.unitPrice.amount, line.unitPrice.currency)} / {scope?.quantity.unit ?? "unit"}</span>
              {isLowest && <span className="text-[11px] font-medium text-emerald-700">Lowest line price</span>}
            </div></td>;
          }
          return <td className="px-2" key={offer.offerVersionId}><div className="flex flex-col items-start gap-1">
            <Button
              variant={selected ? "default" : "outline"}
              size="sm"
              disabled={selecting}
              onClick={() => (selected ? onUnaward(lineId) : onSelect(lineId, offer.offerVersionId, line.offerLineId))}
            >
              {formatMoney(line.unitPrice.amount, line.unitPrice.currency)} · {selected ? "Selected — unselect" : "Select"}
            </Button>
            {isLowest && <span className="text-[11px] font-medium text-emerald-700">Lowest line price</span>}
            {line.leadTime && <span className="text-[11px] text-muted-foreground">{line.leadTime}</span>}
          </div></td>;
        })}
      </tr>;
    })}</tbody></table></div>
  </section>;
}


# Project Status Management Design

**Status:** Approved by the product owner on 2026-08-11

## Goal

Make the persisted Project lifecycle status deliberately editable from Project
Overview while keeping lifecycle status independent from the Property/Spaces/
Work Items setup progression and every downstream commercial/procurement action.

## Authoritative contract

- Read the current value from `ProjectDTO.status`.
- Write through `PATCH /projects/{id}/status` with `{ "status": value }`.
- The allowed values are exactly the backend `ProjectStatus` values already
  represented by the F1 status presentation mapping:
  `lead`, `site_visit`, `estimating`, `quotation_sent`,
  `quotation_approved`, `in_progress`, `completed`, and `closed`.
- Continue using `projectStatusLabel`, `projectStatusTone`, and `StatusBadge` for
  presentation. No Figma-only statuses are introduced.
- The backend remains responsible for validating the requested destination.
  The frontend does not encode a transition graph.

The generated OpenAPI currently types the request field as `string`, although
the backend validates the eight-value enum. The frontend therefore exports one
ordered status option collection from the existing authoritative F1 mapping so
the selector cannot drift from its labels and badge presentation.

## Interaction

The current status badge in the Project Overview identity card becomes an
accessible button with a downward chevron. Activating it opens a dialog:

1. Title: **Change project status**.
2. Read-only **Current status** rendered with the existing badge.
3. **New status** selector containing all eight backend statuses.
4. Explanatory text: **Changing the lifecycle status does not alter project
   setup progress.**
5. **Cancel** and **Update status** actions.

Opening the dialog initializes the selection to the persisted current status.
Changing the selection does not send a request. **Update status** is disabled
while the selected status equals the current status or while the request is in
flight. Cancel closes without mutation.

## Success and failure

Submission calls only `PATCH /projects/{id}/status`. On success the updated
Project becomes the detail cache value, status-bearing queries are invalidated,
and the dialog closes. Invalidation covers Project detail/Overview, all Project
lists (including Client-associated lists), and Dashboard/project-summary data.

If the backend rejects the request, its normalized problem detail is shown in
the dialog. The selected value and dialog remain open so the contractor can
review or retry.

## Separation from setup and activity

`deriveSetupProgress` remains unchanged and continues to depend only on
Property existence, Space count, and Work Item count. No setup, Work Item,
Estimate, Quotation, RFQ, Supplier Offer, or Award mutation changes Project
status. Project creation continues to accept the backend default of `lead`.

## Projects list filtering

The current `GET /projects` contract supports `clientId`, pagination, search,
sort, and order, but no status query parameter. This correction therefore does
not add a status filter. Project rows continue to display the persisted status.
A filter can be added later only after the backend/OpenAPI exposes it.

## Tests

Focused component tests prove that the badge opens the dialog, the selector
contains all eight statuses, same-status submission is disabled, selection is
not an auto-save, explicit confirmation calls the dedicated endpoint, success
updates/invalidate status-bearing queries and closes, failure displays the
backend message and stays open, and setup progress does not change as a side
effect of a lifecycle transition.


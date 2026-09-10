# Phase 1 MVP – AI-Assisted Renovation Project Costing, Quotation, Procurement, and Profitability Platform

## Phase 1 Objective

The Phase 1 product should allow a contractor to manage the complete commercial lifecycle of a renovation project:

Customer inquiry

→ Project creation

→ Space definition

→ Scope of work

→ AI-assisted Work Item generation

→ Material and labour planning

→ Internal cost calculation

→ Profitability analysis

→ Customer-facing Quotation generation

→ Secure Client quotation review

→ Client approval or change request

→ Material requirement generation

→ Supplier RFQ creation

→ Supplier selection from the contractor's Supplier Directory

→ One RFQ Invitation and unique secure link generated per selected Supplier

→ RFQ delivery by email, WhatsApp, or copied secure link

→ Supplier pricing response attributed to the correct Supplier

→ Actual project cost tracking

→ Customer payment tracking

→ Final project profitability

The Phase 1 system should also establish the domain foundation required for future features such as:

* AI-assisted site assessment
* Computer vision defect detection
* AR and LiDAR scanning
* Digital twins
* Material intelligence
* Supplier procurement networks
* Automated supplier comparison
* Purchase Orders
* Blueprint reconstruction
* Renovation visualization
* Architect and engineer collaboration
* Compliance assistance

The core principle is:

**Phase 1 should be a commercially usable contractor platform today while establishing the smallest functional version of the long-term Renovation Project Intelligence architecture.**

The AI principle is:

**AI suggests and assists. The system calculates. The contractor approves.**

AI should accelerate scope creation, resource planning, documentation, quotation writing, and project understanding.

It should not be responsible for authoritative financial calculations, final quantities, contractual commitments, verified construction assessments, or professional engineering conclusions.

The external collaboration principle is:

**External stakeholders receive access only to the specific resource and actions they have been invited to access.**

A Client may access a specific Quotation.

A Supplier may access a specific RFQ.

Future Architects and Engineers may access specific Drawings or Review Requests.

They do not receive unrestricted access to the contractor's company workspace.

---

# 1. Core Domain Model

The Phase 1 system should be built around the following relationship:

```text
Project
    ↓
Space
    ↓
Work Item
    ↓
Resource
    ↓
Cost
    ↓
Estimate
    ↓
Quotation
    ↓
Client Approval
    ↓
Revenue
```

The procurement lifecycle extends this model:

```text
Work Item
    ↓
Material Requirement
    ↓
RFQ
    ↓
Supplier Selection
    ↓
RFQ Invitation
    ↓
Supplier-Specific Access Grant
    ↓
Unique Secure Link
    ↓
Supplier Offer
    ↓
Future Purchase Order
    ↓
Committed Cost
    ↓
Actual Cost
    ↓
Paid Cost
```

Where:

```text
Project
Represents the overall renovation job.

Space
Represents the physical area where work occurs.

Work Item
Represents the actual work being performed.

Resource
Represents materials, labour, subcontractors, equipment, and other resources required.

Cost
Represents the contractor's cost of delivering the work.

Estimate
Represents the contractor's private internal financial calculation.

Quotation
Represents the customer-facing commercial offer.

Approval
Represents a formal action taken by a Client or another authorized stakeholder.

Revenue
Represents the agreed project value and customer payments.

Material Requirement
Represents materials required to deliver approved Work Items.

RFQ
Represents the contractor's material pricing request containing the selected Material Requirements and commercial request details.

RFQ Invitation
Represents the Supplier-specific invitation created when an RFQ is sent to a selected Supplier. Each invitation tracks the intended Supplier, delivery method, unique Access Grant, secure response link, and invitation status.

Supplier Offer
Represents pricing, availability, delivery, and other commercial information returned by a Supplier in response to that Supplier's RFQ Invitation.
```

This relationship should form the foundation of the entire platform.

Future AI and spatial systems should create or enhance these same domain objects rather than introducing disconnected workflows.

---

# 2. Identity, Company Membership, and Access Grants

Phase 1 should separate:

```text
Identity
```

from:

```text
Authorization
```

Identity answers:

```text
Who is this user?
```

Authorization answers:

```text
What is this user allowed to access?
```

The system should support two main access models.

## Internal Access

Internal contractor users receive access through Company Membership.

```text
User
    ↓
Company Membership
    ↓
Role
    ↓
Company Resources
```

Initial roles:

* Company Owner
* Admin
* Employee

More granular internal permissions can be introduced later.

---

## External Access

External stakeholders use scoped Access Grants.

Potential external stakeholders:

* Client
* Supplier
* Future Subcontractor
* Future Architect
* Future Engineer

Example Client grant:

```text
Quotation Access Grant

Resource:
QT-0001

Permissions:
view
download
accept
reject
request_changes
```

Example Supplier grant:

```text
RFQ Access Grant

Resource:
RFQ-00124

Permissions:
view
respond
revise_offer
```

Future example:

```text
Drawing Access Grant

Resource:
Drawing-0021

Permissions:
view
comment
approve
```

A minimal Access Grant should contain:

```text
resourceType
resourceId
granteeType
granteeId
permissions
status
expiresAt
```

External parties without accounts may use secure token-based access.

The original raw token should not be stored in plaintext.

The system should store a secure token hash.

The fundamental security rule is:

```text
External stakeholder
        ↓
Specific Access Grant
        ↓
Specific Resource
        ↓
Specific Allowed Actions
```

Not:

```text
External stakeholder
        ↓
Access to Contractor Workspace
```

---

# 3. Multi-Tenant Company Structure

Every contractor company operates as an independent tenant.

The system should support:

* Company registration
* Company profile
* Company logo
* Registration details
* Address
* Contact information
* Default currency
* Tax information
* Bank and payment information
* Default quotation terms
* Default payment terms

Every company-owned business object should contain:

```text
companyId
```

Examples:

```text
Project
Client
Worker
Material
Supplier
Estimate
Quotation
Cost Item
Payment
RFQ
```

Every backend query must be scoped to the authenticated company.

For example:

```text
Find Project

WHERE

projectId = requestedProject

AND

companyId = authenticatedCompany
```

Tenant isolation should never depend only on frontend filtering.

---

# 4. Client Management

Contractors maintain a Client database.

Each Client contains:

* Name
* Phone
* Email
* Address
* Billing address
* Notes
* Project history

Relationship:

```text
Client
│
├── Project A
├── Project B
└── Project C
```

The Client record should remain separate from individual Projects so returning Clients can have multiple renovation jobs.

A Client does not automatically receive access to a Project.

Access to customer-facing resources is provided through specific Access Grants.

---

# 5. Project Management

Every renovation or construction job becomes a Project.

The Project is the root business entity.

A Project contains references to:

* Client
* Property
* Spaces
* Work Items
* Estimates
* Quotations
* Material Requirements
* RFQs
* RFQ Invitations
* Supplier Offers
* Costs
* Payments
* Documents
* Approvals
* Activity history

Suggested Project statuses:

```text
Lead
↓
Site Visit
↓
Estimating
↓
Quotation Sent
↓
Quotation Approved
↓
In Progress
↓
Completed
↓
Closed
```

The Project itself should remain relatively lightweight.

Do not embed all Work Items, Payments, Costs, Quotations, RFQs, Supplier Offers, and future spatial data into one large MongoDB document.

Related collections should reference the Project through:

```text
projectId
```

---

# 6. Property

A Project should optionally reference a Property.

The Property represents the physical location being renovated.

Store:

* Property address
* Property type
* Notes
* Optional location information

Future versions may expand this into:

```text
Property
    ↓
Building
    ↓
Floor
    ↓
Space
```

For Phase 1, the Property can remain simple.

---

# 7. Space Management

Even though Phase 1 does not include a full Digital Twin, contractors should be able to organize renovation work by physical Space.

Example:

```text
Ahmad Residence

├── Kitchen
├── Living Room
├── Master Bathroom
└── Bedroom 1
```

A Phase 1 Space may contain:

```text
Name
Type
Description
Project ID
```

Example:

```json
{
  "id": "space_001",
  "projectId": "project_123",
  "name": "Master Bathroom",
  "type": "bathroom",
  "schemaVersion": 1
}
```

The Phase 1 UI may simply expose:

```text
Add Area / Room
```

Later, the same Space can evolve into a Digital Twin entity containing:

* Walls
* Floor
* Ceiling
* Doors
* Windows
* Fixtures
* Geometry
* Measurements

This prevents the financial and quotation system from requiring major restructuring when spatial intelligence is introduced later.

---

# 8. Work Items

`WorkItem` should be one of the most important Phase 1 entities.

A Work Item represents actual renovation work.

Examples:

```text
Remove Existing Floor Tiles

Install Waterproofing

Install Ceramic Floor Tiles

Repaint Ceiling

Replace Kitchen Cabinets
```

A Work Item should contain:

```text
Project
Space
Description
Work Type
Quantity
Unit
Status
Source
Verification Status
```

Example:

```json
{
  "id": "work_001",
  "projectId": "project_123",
  "spaceId": "space_001",
  "type": "tile_installation",
  "description": "Supply and install ceramic floor tiles",
  "quantity": 30,
  "unit": "m2",
  "status": "planned",
  "source": "manual",
  "verificationStatus": "confirmed",
  "schemaVersion": 1
}
```

A Work Item can require:

* Materials
* Labour
* Subcontractors
* Equipment
* Transport

The Work Item becomes the bridge between the physical Project and the financial system.

---

# 9. AI-Assisted Scope and Work Item Generation

Phase 1 should include an AI Scope Assistant.

The contractor may enter a simple renovation description such as:

```text
Renovate master bathroom.

Replace existing tiles.

Repaint ceiling.

Replace damaged vanity.
```

The AI can transform this into suggested structured Work Items.

Example:

```text
Suggested Work Items

1. Remove existing floor tiles
2. Remove existing wall tiles
3. Dispose of demolition debris
4. Prepare floor surface
5. Apply waterproofing
6. Install ceramic floor tiles
7. Install ceramic wall tiles
8. Apply tile grout
9. Prepare ceiling surface
10. Repaint ceiling
11. Remove existing vanity
12. Install replacement vanity
```

Workflow:

```text
Contractor Description
        ↓
AI Scope Assistant
        ↓
Suggested Work Items
        ↓
Contractor Reviews
        ↓
Accept / Edit / Reject
        ↓
Confirmed Work Items
```

AI-generated Work Items should initially contain:

```text
source = ai_suggestion

verificationStatus = pending
```

After contractor confirmation:

```text
verificationStatus = confirmed
```

AI should never silently add Work to an Estimate or Quotation without contractor approval.

The AI may also identify potentially missing scope.

Example:

```text
Contractor Scope

Replace Bathroom Floor Tiles
```

AI may suggest:

```text
Possible Missing Items

- Removal of existing tiles
- Debris disposal
- Surface preparation
- Waterproofing inspection
- Tile adhesive
- Tile grout
```

These remain suggestions until accepted.

---

# 10. Material Catalog

Phase 1 should include a structured but intentionally simple Material Catalog.

Store:

* Material name
* Category
* Specification
* Unit
* Default cost
* Currency
* Optional Supplier reference

Example:

```json
{
  "id": "material_001",
  "companyId": "company_001",
  "name": "OPC Cement 50kg",
  "category": "cement",
  "unit": "bag",
  "defaultCost": {
    "amount": 1850,
    "currency": "MYR"
  },
  "schemaVersion": 1
}
```

In this example:

```text
1850 = RM18.50
```

Money should be stored using minor currency units or a safe decimal representation.

Avoid floating-point calculations for financial data.

For Phase 1, contractors should be able to:

* Create Materials
* Edit Materials
* Set default prices
* Assign preferred Suppliers
* Import Material lists from CSV/Excel
* Use company-specific Material prices
* Record their latest purchase price

Do not attempt to build a complete universal construction-material marketplace in Phase 1.

Later, the Catalog can evolve into:

```text
Material Type
↓
Specific Product
↓
Supplier Offer
↓
Contractor Negotiated Price
```

---

# 11. Material Reference Pricing

Contractors require approximate pricing before preparing Quotations.

Phase 1 should support a Reference Pricing layer.

Initial data sources may include:

```text
Starter Material Catalog
+
Contractor Manual Prices
+
Supplier Price Lists
+
CSV / Excel Imports
+
Historical Contractor Purchase Prices
```

Selective public web price extraction may be introduced where permitted and commercially useful.

The system should not imply that a single observed price is the authoritative market price.

Instead, where sufficient data exists, the contractor may see:

```text
Ceramic Tile 600 × 600

Reference Price:
RM32 / m²

Observed Range:
RM28 – RM37 / m²

Contractor Last Purchase:
RM29.50 / m²

Last Updated:
2 days ago
```

The contractor may override the reference price when preparing the internal Estimate.

The Estimate uses the contractor-confirmed value.

Long term, pricing intelligence may incorporate:

```text
Supplier Feeds
+
Supplier Catalog Uploads
+
Web Price Observations
+
RFQ Responses
+
Purchase Orders
+
Actual Supplier Invoices
```

RFQ and transaction data should eventually become more authoritative than public retail pricing.

---

# 12. Supplier Management and Supplier Directory

Contractors should maintain their own company-scoped Supplier Directory.

The Supplier Directory is the primary source from which contractors select Suppliers when sending an RFQ in Phase 1. This avoids the need for a public Supplier marketplace while still allowing the contractor to manage procurement digitally.

Store:

* Supplier name
* Contact person
* Email
* Phone / WhatsApp number
* Address
* Material or product categories supplied
* Notes
* Active / inactive status

A Material may optionally reference:

```text
Preferred Supplier
```

The platform may also use historical procurement data to identify:

```text
Previously Used Supplier
Last Purchase Supplier
Preferred Supplier
Relevant Supplier Category
```

When creating an RFQ, the contractor should be able to:

```text
Search Supplier Directory

Filter by Material Category

Select one or more Suppliers

See Preferred or Previously Used Suppliers

Add a New Supplier without leaving the RFQ workflow
```

Example:

```text
Select Suppliers

Search suppliers...

☑ ABC Building Materials
   Tiles, Cement, Adhesive

☐ XYZ Hardware
   Paint, Tools

☑ Mega Tiles
   Tiles, Sanitary Ware

[ + Add New Supplier ]
```

The system may suggest relevant Suppliers based on the selected Materials, categories, preferred Supplier settings, and previous purchases. These are recommendations only; the contractor makes the final selection.

For Phase 1, Suppliers do not require full platform accounts.

A Supplier may participate through restricted Supplier-specific RFQ access.

Supplier accounts and full Supplier Portals can be introduced later.

---

# 13. Worker Management

Workers should be modeled separately from Project costs.

A Worker record contains:

```text
Name
Role
Default Payment Type
Default Rate
```

Example:

```text
Worker

Name: Ahmad
Role: Tiler
Rate Type: Daily
Rate: RM150/day
```

Supported rate types:

* Hourly
* Daily
* Fixed Project rate
* Per-unit
* Per-square-meter

Workers may participate in multiple Projects.

Do not store Project-specific Labour costs directly inside the Worker record.

---

# 14. AI-Assisted Labour and Trade Suggestions

When a Work Item is created, AI may suggest the likely trade or Worker type required.

Example:

```text
Work Item

Install Bathroom Floor Tiles
```

AI suggestion:

```text
Recommended Trade

Tiler
```

Another example:

```text
Work Item

Replace Bathroom Water Piping
```

AI suggestion:

```text
Recommended Trade

Plumber
```

The contractor then selects:

* An existing Worker
* A Subcontractor
* A custom Labour rate

AI should not determine authoritative Labour pricing.

Labour pricing must come from:

```text
Worker Default Rate

or

Project-Specific Rate

or

Contractor Manual Input
```

---

# 15. Labour Entries

Project Labour should be represented through separate Labour Entries.

Example:

```text
Worker:
Ahmad

Project:
Ahmad Residence

Work Item:
Install Bathroom Floor Tiles

Rate:
RM150/day

Days:
8

Actual Labour Cost:
RM1,200
```

A Labour Entry can reference:

```text
workerId
projectId
workItemId
```

This allows one Worker to work across multiple Projects while maintaining accurate Project-level costing.

For Phase 1, Labour time can be entered manually.

Automatic attendance, clock-in, and payroll processing are not required.

---

# 16. Subcontractor Costs

Subcontracted work should be treated as Project costs.

Examples:

* Electrical work
* Plumbing
* Air-conditioning
* Cabinetry
* Structural work
* Specialist installation

Example:

```text
Subcontractor:
ABC Plumbing

Work Item:
Bathroom Plumbing Upgrade

Estimated Cost:
RM4,500

Actual Cost:
RM4,800
```

Full Subcontractor procurement workflows can be added later.

---

# 17. Resource Model

A Work Item may require multiple Resources.

Example:

```text
Install Bathroom Floor Tiles
│
├── Material
│   ├── Tiles
│   ├── Adhesive
│   └── Grout
│
├── Labour
│   └── Tiler
│
├── Equipment
│   └── Tile Cutter
│
└── Transport
    └── Delivery
```

These Resources are used to generate internal Cost Estimates.

---

# 18. AI-Assisted Resource Suggestions

Phase 1 should include an AI Resource Suggestion Assistant.

When a contractor creates or confirms a Work Item, the system can suggest likely Resources.

Example:

```text
Work Item

Install 30 m² Ceramic Floor Tiles
```

AI suggests:

```text
Suggested Materials

Ceramic Tiles
Tile Adhesive
Tile Grout
Tile Spacers


Suggested Labour

Tiler


Suggested Equipment

Tile Cutter
```

The AI is responsible only for suggesting relevant Resource types.

The deterministic Estimation Engine remains responsible for:

* Quantities
* Waste calculations
* Unit conversions
* Material costs
* Labour costs
* Final totals

The rule is:

```text
AI Suggests

System Calculates

Contractor Approves
```

For example:

```text
AI Suggestion

Tiles required
```

Then the application calculates:

```text
Floor Area

30 m²

Waste Allowance

10%

Required Quantity

33 m²
```

The LLM should not be responsible for authoritative financial or quantity calculations.

---

# 19. Cost Model

The simple Expense concept should be replaced with a broader `CostItem` model.

Every Project Cost belongs to a category.

Categories include:

```text
Material
Labour
Subcontractor
Equipment
Transport
Permit
Professional Fee
Utility
Miscellaneous
```

Costs should eventually support four lifecycle stages:

```text
Estimated
Committed
Actual
Paid
```

For Phase 1, the user interface may focus primarily on:

```text
Estimated

Actual
```

The underlying domain should support the complete lifecycle.

Example:

```text
Estimated Material Cost
RM5,000

↓

Supplier Offer Accepted / Future PO

Committed Cost
RM5,300

↓

Supplier Invoice Received

Actual Cost
RM5,450

↓

Supplier Paid

Paid Cost
RM5,450
```

This allows the system to grow naturally into Procurement.

---

# 20. Internal Estimate

An Estimate is an internal contractor record.

It is not the same entity as a Quotation.

An Estimate contains:

* Work Items
* Resources
* Material costs
* Labour costs
* Subcontractor costs
* Equipment costs
* Other costs
* Markup
* Margin calculations
* Proposed selling price

Example:

```text
Bathroom Renovation

Materials            RM18,000
Labour               RM10,000
Subcontractors        RM4,000
Equipment             RM1,500
Transport             RM1,200
Other                   RM600

Estimated Cost       RM35,300

Target Profit        RM14,700

Proposed Sell Price  RM50,000
```

The contractor may manually override prices and quantities.

The system assists the contractor but does not lock them into calculated values.

The Estimate is private contractor information.

It should not be accessible through Client or Supplier Access Grants.

---

# 21. AI Integration in the Estimation Workflow

The AI should assist Estimate preparation without replacing deterministic calculations.

Workflow:

```text
Project Scope
        ↓
AI Suggests Work Items
        ↓
Contractor Confirms
        ↓
AI Suggests Materials and Trades
        ↓
Contractor Confirms
        ↓
Estimation Engine Calculates
        ↓
Internal Estimate
```

The Estimation Engine calculates:

* Material quantities
* Material costs
* Labour costs
* Subcontractor costs
* Equipment costs
* Transport
* Other Project costs
* Markup
* Margin
* Proposed selling price

AI may also identify potentially missing Cost or Scope categories.

These should always remain reviewable suggestions.

---

# 22. Profitability Preview

Before generating a Quotation, the contractor should see:

```text
Selling Price

Estimated Project Cost

Expected Profit

Profit Margin
```

Example:

```text
Proposed Selling Price      RM50,000

Estimated Costs

Materials                   RM18,000
Labour                      RM10,000
Subcontractors               RM4,000
Equipment                    RM1,500
Transport                    RM1,200
Other                          RM600

Total Estimated Cost        RM35,300

Expected Profit             RM14,700

Expected Profit Margin         29.4%
```

This should be one of the strongest Phase 1 features.

This information is private to the contractor.

---

# 23. Customer-Facing Quotation

A Quotation is the Client-facing commercial document.

It is generated from an Estimate but remains a separate entity.

The Client should see the complete customer-facing selling-price information relevant to the purchase decision.

The Quotation may include:

* Scope of Work
* Work Packages
* Quantities where appropriate
* Units
* Selling unit prices
* Line-item totals
* Discounts
* Taxes
* Deposit requirements
* Payment schedule
* Terms and conditions
* Quotation validity period
* Grand total

Example:

```text
BATHROOM RENOVATION

Demolition Works          RM3,500
Waterproofing             RM2,800
Wall Tiles                RM6,200
Floor Tiles               RM4,000
Plumbing Works            RM3,500
Painting                  RM2,000

Subtotal                 RM22,000

Tax                       RM1,320

Total                    RM23,320
```

The contractor may optionally display quantity-level selling prices.

Example:

```text
Ceramic Floor Tiles

30 m² × RM120/m²

RM3,600
```

The Client should be fully informed about the commercial price they are being asked to approve.

---

# 24. Internal Cost vs Customer Selling Price

The system should maintain a strict separation between:

```text
INTERNAL ESTIMATE
```

and:

```text
CLIENT QUOTATION
```

The Client may see:

```text
Bathroom Flooring Works

RM8,500
```

The Client must not see:

```text
Supplier Material Cost
RM3,100

Worker Wages
RM1,200

Transport Cost
RM300

Markup
RM1,800

Expected Profit
RM2,100
```

Information that should remain private includes:

* Material purchase cost
* Supplier quotations
* Supplier discounts
* Contractor negotiated prices
* Worker wages
* Internal Labour cost
* Subcontractor cost
* Internal overhead
* Markup
* Profit margin
* Expected profit
* Actual Project expenses

The principle is:

```text
Client sees the complete price of what they are buying.

Contractor retains privacy over how that selling price is constructed.
```

---

# 25. Quotation Detail Levels

The contractor may control how selling prices are presented.

## Detailed

```text
Remove Existing Tiles       RM1,500

Waterproofing Works          RM2,800

Supply Ceramic Tiles         RM4,200

Tile Installation            RM3,000
```

## Grouped

```text
Bathroom Flooring Package   RM11,500
```

## Fixed Package

```text
Complete Bathroom Renovation

RM23,320
```

Regardless of presentation format, the Client must see the complete commercial amount they are expected to pay.

---

# 26. AI Quotation Writing Assistant

The Quotation system should include an AI Quotation Writing Assistant.

AI converts structured Work Items into professional Client-facing descriptions.

Example internal Work Item:

```text
tile_installation

Quantity:
30 m²
```

AI-generated wording:

```text
Supply and install new ceramic floor tiles, including surface preparation, tile adhesive application, tile laying, grouting, and final finishing works.
```

The contractor can:

```text
Accept
Edit
Regenerate
```

before issuing the Quotation.

AI may generate descriptions, but all:

* Quantities
* Prices
* Discounts
* Taxes
* Totals
* Payment terms

must come from structured system data.

---

# 27. Quotation Versioning

Quotation versioning should be implemented in Phase 1.

Example:

```text
QT-0001 V1
RM35,000
```

Client requests changes.

```text
QT-0001 V2
RM38,500
```

Older issued Quotations should remain available.

Issued Quotations should be treated as historically stable records.

They should not silently change when:

* Material prices change
* Worker rates change
* Supplier prices change

Quotation Items should contain pricing snapshots.

---

# 28. Client Quotation Portal

When a contractor sends a Quotation, the system creates a scoped Quotation Access Grant.

Example:

```text
Quotation Access Grant

Resource:
QT-0001

Permissions:
view
download
accept
reject
request_changes

Expires:
Quotation Expiry Date
```

The Client receives a secure link.

The Client can see:

* Contractor information
* Project information
* Scope of Work
* Complete Client-facing selling-price breakdown
* Quantities where applicable
* Selling prices
* Discounts
* Taxes
* Payment schedule
* Deposit requirements
* Terms and conditions
* Grand total

The Client can:

```text
[Download Quotation]

[Accept]

[Reject]

[Request Changes]
```

The Client does not gain general access to the contractor's application.

---

# 29. AI Client-Friendly Scope Explanations

The Client Quotation Portal may include optional AI-generated plain-language explanations.

Example:

```text
Technical Scope

Apply waterproofing membrane to bathroom floor before installation of new ceramic tiles.
```

AI explanation:

```text
This waterproofing layer helps reduce the risk of water penetrating the floor structure before the new tiles are installed.
```

AI-generated explanations must not modify:

* Contractual scope
* Quantities
* Prices
* Terms

---

# 30. Approval Model

A general Approval structure should be introduced in Phase 1.

Example:

```json
{
  "subjectType": "quotation",
  "subjectId": "quotation_001",
  "actorType": "client",
  "status": "approved"
}
```

Later, the same model can support:

* Material approval
* Variation Order approval
* Blueprint approval
* Architect approval
* Engineer approval
* Compliance review

---

# 31. Client Quotation Acceptance

When the Client accepts:

```text
Client Opens Secure Link
        ↓
Reviews Quotation
        ↓
Accepts
        ↓
Approval Record Created
        ↓
Quotation Version Locked
        ↓
Quotation Status = Accepted
        ↓
Project Status Updated
```

The system should record:

* Accepted Quotation ID
* Accepted version
* Acceptance date and time
* Client identity where known
* Access Grant used
* Relevant audit metadata

---

# 32. Material Requirement Generation

Once Work Items and quantities are established, the system should create Material Requirements.

Example:

```text
Bathroom Renovation

Ceramic Tiles       33 m²
Tile Adhesive        8 bags
Grout                5 kg
Waterproofing       15 kg
```

Material Requirements should reference:

```text
projectId
workItemId
materialId
requiredQuantity
unit
```

The contractor can review and modify the requirements before Procurement.

---

# 33. Supplier RFQ

Phase 1 should include a lightweight Request for Quotation workflow that begins from approved or reviewed Material Requirements.

An RFQ represents the common request content that may be sent to one or more Suppliers.

The RFQ may contain:

* Project reference
* Requested Materials
* Specifications
* Quantities
* Units
* Delivery location
* Required delivery date
* Contractor notes
* RFQ expiry or response deadline

Workflow:

```text
Material Requirements
        ↓
Contractor Selects Materials
        ↓
Creates RFQ
        ↓
System Opens Supplier Selection
        ↓
Contractor Selects Supplier(s) from Supplier Directory
        ↓
One RFQ Invitation Created per Supplier
        ↓
One Supplier-Specific Access Grant Created per Invitation
        ↓
One Unique Secure Link Generated per Supplier
        ↓
Contractor Sends by Email / WhatsApp / Copy Link
```

The RFQ is the shared request definition.

The RFQ Invitation is the Supplier-specific delivery and access record.

This distinction is important because the same RFQ may be sent to multiple Suppliers, while each Supplier must have its own secure link, invitation status, and response attribution.

The platform remains the system of record for the commercial Procurement workflow.

# 33.1 Supplier Selection Workflow

When the contractor chooses to request Supplier pricing, the system should display the contractor's Supplier Directory.

The contractor can:

* Search by Supplier name
* Filter by Material or product category
* Select one or more Suppliers
* View preferred Suppliers
* View previously used Suppliers
* Add a new Supplier inline

The system may suggest relevant Suppliers using:

```text
Selected Material Categories
+
Preferred Supplier References
+
Previous RFQ History
+
Previous Purchase History
```

Example:

```text
Ceramic Tiles
Tile Adhesive
Grout

Suggested Suppliers

☑ ABC Tiles
  Preferred Supplier for Ceramic Tiles

☑ Mega Tiles
  Previously Purchased From

☐ ABC Hardware
  Supplies Tile Adhesive and Grout
```

Supplier suggestions are advisory only. The contractor decides which Suppliers receive the RFQ.

---

# 33.2 RFQ Invitation Model

For every selected Supplier, the system should create a separate `RFQInvitation` record.

Example:

```text
RFQ-00124
│
├── RFQ Invitation A
│      Supplier: ABC Building Materials
│      Status: Responded
│
├── RFQ Invitation B
│      Supplier: XYZ Hardware
│      Status: Viewed
│
└── RFQ Invitation C
       Supplier: Mega Tiles
       Status: Sent
```

A minimal RFQ Invitation model should contain:

```text
id
companyId
projectId
rfqId
supplierId
accessGrantId
deliveryMethod
recipientEmail
recipientPhone
status
sentAt
viewedAt
respondedAt
expiresAt
```

Possible statuses:

```text
Draft
Sent
Viewed
Responded
Declined
Expired
Cancelled
```

The RFQ Invitation creates the explicit relationship:

```text
RFQ
        ↓
RFQ Invitation
        ↓
Supplier-Specific Access Grant
        ↓
Supplier
        ↓
Supplier Offer
```

This ensures that Supplier responses can always be attributed to the correct Supplier without exposing other Suppliers or their Offers.

---

# 33.3 Supplier-Specific Secure Links and RFQ Delivery

The system must generate a different secure response link for each Supplier selected for an RFQ.

Example:

```text
RFQ-00124
│
├── ABC Supplier
│      └── Unique Secure Link A
│
├── XYZ Supplier
│      └── Unique Secure Link B
│
└── Mega Supplier
       └── Unique Secure Link C
```

Conceptually:

```text
/rfq/respond/{supplier-specific-secure-token}
```

The system should never send one shared guest-access token to multiple Suppliers.

Each secure token is connected to one RFQ Invitation and one Supplier-specific Access Grant.

After Supplier selection, the contractor should see delivery actions per Supplier:

```text
ABC Building Materials
Email: sales@abc.com
WhatsApp: +60...
[Send Email] [Share via WhatsApp] [Copy Secure Link]

XYZ Hardware
Email: sales@xyz.com
WhatsApp: +60...
[Send Email] [Share via WhatsApp] [Copy Secure Link]
```

For Phase 1:

**Email**

The platform may send the RFQ invitation directly by email, containing the Supplier's unique secure link.

**WhatsApp**

The platform may open WhatsApp with a pre-filled message containing the Supplier's unique secure link. The contractor completes the send action. A full WhatsApp Business API integration is not required for the initial MVP.

**Copy Secure Link**

The contractor may copy the Supplier-specific link and send it manually through another communication channel.

Regardless of delivery channel, the link still resolves to the same scoped RFQ Invitation and Access Grant.


---

# 34. Restricted Supplier Access

A Supplier should not receive access to the contractor's general application.

They receive access only to the specific RFQ through the Supplier-specific RFQ Invitation and Access Grant created for them.

Example:

```text
RFQ Access Grant

Resource:
RFQ-00124

RFQ Invitation:
Invitation-ABC-001

Supplier:
ABC Building Materials

Permissions:
view
respond
revise_offer

Expires:
31 July
```

A Supplier can see:

* Requested Materials
* Specifications
* Quantities
* Delivery location
* Required delivery date
* Contractor contact details where appropriate

A Supplier may submit:

* Unit price
* Available quantity
* Stock availability
* Delivery fee
* Estimated delivery date
* Alternative product
* Offer validity
* Notes

The Supplier cannot see:

* Client Quotation
* Internal Estimate
* Contractor Margin
* Expected Profit
* Actual Project costs
* Worker wages
* Other Supplier Offers
* Unrelated Projects

---

# 35. Supplier Guest Access

Phase 1 should not require every Supplier to register an account.

The Supplier receives a Supplier-specific:

```text
Secure RFQ Link
```

The link grants temporary scoped access to that Supplier's RFQ Invitation only.

Conceptually:

```text
/rfq/respond/{secure-token}
```

The backend verifies:

```text
Is the Access Grant active?

Has it expired?

Does it match this RFQ?

Does it allow viewing?

Does it allow responding?
```

If authorized, the Supplier may interact only with that RFQ.

Later, Suppliers with accounts can access the same RFQ through a Supplier Portal.

---

# 36. Supplier Communication

Phase 1 should use a hybrid communication model.

Contractor and Supplier may continue using:

```text
WhatsApp

Email
```

The platform should own the structured Procurement and invitation status.

Example:

```text
RFQ Created
↓
Suppliers Selected
↓
RFQ Invitations Created
↓
Unique Secure Links Generated
↓
Invitation Sent
↓
Supplier Viewed
↓
Supplier Responded
↓
Offer Received
↓
Offer Selected
```

Phase 1 should not build a complete real-time messaging platform.

In-app messaging can be introduced later if Supplier adoption justifies it.

---

# 37. Supplier Offers

A Supplier response should become a structured Supplier Offer.

A Supplier Offer may contain:

* Supplier ID
* RFQ ID
* RFQ Invitation ID
* Material ID
* Product information
* Unit price
* Available quantity
* Delivery fee
* Lead time
* Alternative product
* Offer validity
* Notes

Example:

```text
Supplier:
ABC Building Supplies

Ceramic Tiles:
RM29.50 / m²

Quantity Available:
100 m²

Delivery:
RM150

Lead Time:
2 days
```

---

# 38. Supplier Offer Comparison

Phase 1 may support basic comparison if the same RFQ is sent to multiple Suppliers.

Example:

```text
                  Supplier A    Supplier B    Supplier C

Materials          RM4,100       RM4,050       RM4,400

Delivery             RM150         RM300            RM0

Total               RM4,250       RM4,350       RM4,400

Lead Time           2 days       Same day       3 days
```

This should remain a simple comparison feature.

Phase 1 should not attempt to build a full Procurement marketplace.

---

# 39. Procurement Cost Evolution

The future Cost lifecycle should connect Procurement to profitability.

Example:

```text
Internal Estimated Material Cost

RM4,100

↓

Supplier Offer Accepted

RM4,350

↓

Committed Material Cost

RM4,350

↓

Supplier Invoice

RM4,420

↓

Actual Material Cost

RM4,420
```

The Project's projected profitability can then update automatically.

Phase 1 may implement only part of this lifecycle, but the domain should support it.

---

# 40. Actual Cost Tracking

Once work begins, contractors record actual Costs.

Example:

```text
Category:
Material

Description:
Ceramic Floor Tiles

Supplier:
ABC Tiles

Actual Cost:
RM2,300

Project:
Ahmad Residence
```

Actual Costs should reference:

```text
projectId
```

and where possible:

```text
workItemId
```

This enables profitability analysis at both:

```text
Project Level
```

and:

```text
Work Item Level
```

---

# 41. Estimated vs Actual Costing

The system should compare Estimated and Actual Costs.

Example:

```text
                    ESTIMATED      ACTUAL

Materials            RM18,000     RM19,500

Labour               RM10,000     RM12,400

Subcontractors        RM4,000      RM4,000

Transport             RM1,200      RM1,500

Other                 RM2,100      RM2,300
```

The system calculates:

```text
Estimated Cost

RM35,300


Current Actual Cost

RM39,700


Cost Variance

+RM4,400
```

This enables contractors to understand where margin is being lost.

---

# 42. Customer Payment Tracking

Customer Payments should be tracked separately from Project Costs.

Example:

```text
Contract Value

RM50,000


Deposit Received

RM10,000


Progress Payment

RM15,000


Total Received

RM25,000


Outstanding

RM25,000
```

The system must distinguish:

```text
Profitability
```

from:

```text
Cash Flow
```

A Project can be profitable while still having unpaid customer balances.

---

# 43. Project Profitability Dashboard

The main Project Dashboard should show:

```text
PROJECT

Ahmad Residence Renovation


CONTRACT VALUE

RM50,000


CUSTOMER PAYMENTS

Received                RM25,000
Outstanding             RM25,000


ESTIMATED COST

RM35,300


ACTUAL COST SO FAR

RM27,500


PROJECTED FINAL COST

RM39,700


ORIGINAL EXPECTED PROFIT

RM14,700


CURRENT PROJECTED PROFIT

RM10,300


PROJECTED PROFIT MARGIN

20.6%
```

The contractor may additionally see Procurement impacts such as:

```text
Estimated Material Cost

RM18,000

Current Supplier Commitments

RM19,200

Material Cost Variance

+RM1,200
```

This should become one of the MVP's main commercial selling points.

The Client and Supplier cannot access this Dashboard.

---

# 44. AI-Assisted Project Report Generation

Phase 1 should include AI-assisted report generation using structured Project data.

The system can generate:

* Project summary
* Site visit summary
* Scope-of-Work report
* Estimate summary
* Quotation summary
* Project progress summary

All authoritative numbers must come from structured system data.

The LLM should never independently invent or calculate authoritative financial values.

---

# 45. AI Project Copilot

A lightweight natural-language Project Copilot can be introduced toward the later part of Phase 1.

The contractor can ask:

```text
What is my estimated profit on this Project?

Which Work Items are costing the most?

Which Material Costs are over budget?

How much has the Client paid?

Which Suppliers have received, viewed, or responded to the RFQ?
```

Architecture:

```text
User Question
        ↓
Go Backend Retrieves Authorized Project Data
        ↓
Structured Context
        ↓
LLM
        ↓
Natural-Language Response
```

The LLM should not have unrestricted direct access to MongoDB.

The Go Backend determines:

* Which company owns the data
* Which Project the user can access
* Which records are provided
* Which actions are permitted

---

# 46. Optional Experimental Photo Summarization

Phase 1 may optionally test general photo summarization using a multimodal LLM.

This is not defect detection.

Example:

```text
Possible observations:

- Tiled bathroom wall visible
- Existing vanity present
- Ceiling appears painted
- Dark staining visible near lower wall area
```

These outputs should be labeled:

```text
AI Visual Observation

Not Verified
```

The system should not claim:

```text
Structural crack confirmed

Waterproofing failure confirmed

Mold confirmed

Structural damage detected
```

True automated defect detection is explicitly outside Phase 1.

---

# 47. AI Features Explicitly Excluded From Phase 1

The following should remain outside Phase 1:

```text
Custom Computer Vision Defect Detection

Crack Detection Models

Defect Severity Classification

Structural Damage Assessment

Moisture Diagnosis

Real-Time Computer Vision

AR / LiDAR Reconstruction

Automatic Digital Twin Generation

Blueprint-to-CAD Reconstruction

Depth Estimation

3D Room Reconstruction

Automated Compliance Checking

Photorealistic Construction-Accurate Rendering
```

These require specialized:

* Computer vision
* Machine learning
* Training data
* Model evaluation
* Geometry processing
* Validation
* Professional oversight

They belong in later phases.

---

# 48. Basic Company Dashboard

The Company-level Dashboard should remain relatively simple in Phase 1.

Show:

* Active Projects
* Quotations sent
* Quotations accepted
* Total contract value
* Customer Payments received
* Outstanding Payments
* Total Project Costs
* Estimated Project profits
* Actual or projected profits
* Open RFQs
* Supplier responses awaiting review

Do not build advanced accounting or enterprise financial reporting yet.

Focus on Project profitability and core Procurement visibility.

---

# 49. Documents

Documents should be first-class entities.

Document types may include:

```text
Quotation

Invoice

Receipt

Project Photo

Contract

Report

RFQ

Supplier Offer
```

Structured database data should remain the source of truth.

For example:

```text
Quotation Data
```

is authoritative.

The generated:

```text
PDF
```

is an output artifact.

Later this model can expand to:

* Purchase Orders
* Inspection Reports
* Blueprints
* Variation Orders
* Compliance Documents

---

# 50. Basic Audit History

Phase 1 should record critical business events.

Examples:

```text
Project Created

Work Item Suggested by AI

Work Item Approved by Contractor

Estimate Created

Estimate Updated

Quotation Generated

Quotation Sent

Client Viewed Quotation

Client Requested Changes

Client Accepted Quotation

RFQ Created

Suppliers Selected

RFQ Invitation Created

RFQ Invitation Sent

Supplier Viewed RFQ

Supplier Submitted Offer

Supplier Revised Offer

Supplier Offer Selected

Actual Cost Added

Payment Recorded
```

This does not require full Event Sourcing.

Use:

```text
Current State
+
Audit Events
```

This provides:

* Accountability
* Debugging
* User activity history
* Future Project timelines

---

# 51. Financial Data Integrity

Financial records should follow stricter rules than flexible Project metadata.

Important principles:

* Avoid floating-point money calculations
* Store money using minor units or safe decimals
* Maintain historical Quotation snapshots
* Maintain Supplier Offer snapshots where required
* Prevent silent modification of issued Documents
* Validate all financial inputs
* Maintain clear status transitions
* Use transactions where business consistency requires them

Financial data should be intentionally structured even though the platform uses MongoDB.

---

# 52. Schema Versioning

Important MongoDB documents should contain:

```text
schemaVersion
```

Example:

```json
{
  "schemaVersion": 1
}
```

Migration scripts should be maintained.

Example:

```text
migrations/

001_add_schema_version.go

002_convert_money_to_minor_units.go

003_add_cost_stage.go

004_add_ai_source_metadata.go

005_add_access_grants.go

006_add_rfq_and_supplier_offers.go

007_add_rfq_invitations.go
```

NoSQL flexibility should not replace controlled database evolution.

---

# 53. Idempotency

Critical operations should support idempotency.

Examples:

* Payment creation
* Quotation acceptance
* RFQ submission
* RFQ Invitation creation and delivery
* Supplier Offer submission
* Future Purchase Orders
* AI processing jobs

Example:

```text
Supplier submits Offer

↓

Network retry occurs

↓

Same idempotency key

↓

Offer created once
```

This prevents duplicate transactions and repeated processing.

---

# 54. Optimistic Concurrency

Important editable documents should maintain:

```text
version
```

Example:

```json
{
  "version": 8
}
```

An update succeeds only if the stored version remains:

```text
8
```

After updating:

```text
version = 9
```

This reduces silent overwriting when multiple users edit the same Project, Estimate, Quotation, or Supplier Offer.

---

# 55. MongoDB Design Principles

Use MongoDB with intentional structure.

Collections may include:

```text
users

companies

company_members

access_grants

clients

projects

properties

spaces

work_items

materials

material_requirements

suppliers

workers

labour_entries

cost_items

estimates

quotations

rfqs

rfq_invitations

supplier_offers

payments

documents

approvals

audit_events

ai_suggestions
```

Avoid giant Project documents.

Use references for:

* Work Items
* Costs
* Payments
* Quotations
* RFQs
* RFQ Invitations
* Supplier Offers
* AI Suggestions
* Large Project history

Embedding should only be used for small, bounded data that is almost always read together.

---

# 56. AI Suggestion Data Model

AI-generated recommendations should remain separate from verified business data until approved.

Example:

```json
{
  "id": "suggestion_001",
  "projectId": "project_123",
  "type": "work_item",
  "source": "ai",
  "status": "pending",
  "confidence": 0.82,
  "suggestedData": {
    "type": "waterproofing",
    "description": "Apply waterproofing membrane to bathroom floor"
  }
}
```

Possible statuses:

```text
Pending

Accepted

Modified

Rejected
```

After acceptance, the approved data becomes a real domain object.

This prevents AI output from silently becoming authoritative Project data.

---

# 57. Access Grant Data Model

Access Grants should be introduced in Phase 1.

Example:

```json
{
  "id": "grant_001",
  "resourceType": "quotation",
  "resourceId": "quotation_001",
  "granteeType": "client",
  "granteeId": "client_001",
  "permissions": [
    "view",
    "download",
    "accept",
    "reject",
    "request_changes"
  ],
  "status": "active",
  "expiresAt": "2026-07-31"
}
```

Supplier example:

```json
{
  "id": "grant_002",
  "resourceType": "rfq",
  "resourceId": "rfq_00124",
  "rfqInvitationId": "rfq_invitation_001",
  "granteeType": "supplier",
  "granteeId": "supplier_001",
  "permissions": [
    "view",
    "respond",
    "revise_offer"
  ],
  "status": "active",
  "expiresAt": "2026-07-31"
}
```

Guest access may use:

```text
tokenHash
```

instead of requiring a registered User ID.

---

# 58. Future Digital Twin Compatibility

Even though Phase 1 does not include a Digital Twin, its domain should already support future spatial relationships.

Phase 1:

```text
Master Bathroom
```

Later:

```text
space_uuid_123
```

Eventually:

```text
Master Bathroom
│
├── Wall_001
├── Wall_002
├── Wall_003
├── Floor_001
└── Ceiling_001
```

A future Work Item may reference:

```text
space_uuid_123

and

surface_uuid_456
```

This allows future AI and LiDAR systems to connect physical geometry directly to Work Items and Project Costs.

---

# 59. Future AI and Computer Vision Compatibility

Future Computer Vision systems should produce suggestions compatible with the existing domain.

Example:

```text
Defect Detection Model
        ↓
Possible Defect
        ↓
Contractor Verifies
        ↓
Suggested Repair
        ↓
Create Work Item
        ↓
Assign Materials
        ↓
Assign Labour
        ↓
Calculate Cost
        ↓
Update Estimate
```

Because Phase 1 already contains:

```text
Space

Work Item

Resource

Cost

Estimate
```

future defect detection does not require redesigning the commercial platform.

---

# 60. Future Professional Collaboration Compatibility

The Access Grant system should later extend to professional collaboration.

Example:

```text
Architect

↓

Drawing Access Grant

↓

View
Comment
Approve
```

Engineer:

```text
Engineer

↓

Structural Review Access Grant

↓

View
Comment
Approve
```

Subcontractor:

```text
Subcontractor

↓

Work Package Access Grant

↓

View Scope
Submit Price
Confirm Work
```

The same security architecture supports all external stakeholders.

---

# 61. Phase 1 AI Architecture

AI should communicate through an internal abstraction layer rather than being tightly coupled to one provider.

```text
                        Contractor
                            ↓
                            ▼
                       Go + Huma
                            ↓
              ┌─────────────┼──────────────┐
              │             │              │
              ▼             ▼              ▼
         Domain Engine   AI Gateway    MongoDB
              │             │
              │             ▼
              │        External LLM
              │       / Multimodal API
              │             │
              └─────────────┘
                    Suggestions
```

The internal AI service may expose:

```text
GenerateWorkItemSuggestions

SuggestResources

SuggestTrades

GenerateQuotationDescription

GenerateProjectReport

SummarizeProject

AnswerProjectQuestion

AnalyzePhotoExperimentally
```

The AI provider can later change without redesigning the business domain.

---

# 62. Phase 1 Technical Architecture

Keep the technical architecture intentionally simple.

```text
                   Next.js Web App
                          │
                          ▼
                    Go + Huma API
                          │
           ┌──────────────┼───────────────┐
           ▼              ▼               ▼
       MongoDB            S3            SQS
           │              │               │
      Business Data    Documents     Background Jobs
```

Suggested AWS components:

```text
CloudFront
    ↓
Next.js
    ↓
API Gateway / ALB
    ↓
Go + Huma
    ↓
ECS Fargate
```

Supporting services:

```text
MongoDB Atlas
Application data


Amazon S3
Photos
Documents
Quotation PDFs
RFQ PDFs


AWS Lambda
PDF generation
Email sending
Image processing
Notifications


Amazon SQS
Background jobs
AI processing requests where appropriate


Amazon Cognito
Authentication
```

The AI provider is accessed through the Go Backend's AI Gateway.

---

# 63. Go Application Architecture

Phase 1 should be implemented as a Modular Monolith.

Example:

```text
cmd/

internal/

    identity/

    companies/

    access/

    clients/

    projects/

    properties/

    spaces/

    work/

    materials/

    suppliers/

    procurement/

    labour/

    estimates/

    quotations/

    costs/

    payments/

    documents/

    approvals/

    audit/

    ai/
```

One Go application.

One primary deployment.

Strong Domain boundaries inside the codebase.

Do not introduce Microservices unless scale, team ownership, or operational requirements justify them.

---

# 64. What Phase 1 Should Not Include

Do not build yet:

* Kubernetes
* Kafka
* Temporal
* Dedicated GPU infrastructure
* Real-time Digital Twin streaming
* Full AR/LiDAR scanning
* Computer Vision defect detection
* Custom defect detection model training
* Blueprint reconstruction
* Full Supplier marketplace
* Full live Material market pricing
* Supplier payment processing
* Complex logistics management
* Full accounting
* Full payroll
* Compliance automation
* Architect or Engineer marketplace
* Full in-app Contractor/Supplier chat

The Phase 1 architecture should support future integration with these systems without requiring them to exist yet.

---

# 65. Phase 1 End-to-End User Journey

```text
Contractor Registers
        ↓
Creates Company
        ↓
Adds Client
        ↓
Creates Project
        ↓
Adds Property
        ↓
Defines Spaces

Kitchen
Bathroom
Living Room
        ↓
Describes Renovation Scope

"Renovate bathroom, replace tiles,
waterproof floor and repaint ceiling."
        ↓
AI Scope Assistant
        ↓
Suggests Work Items
        ↓
Contractor Reviews and Confirms
        ↓
AI Resource Assistant
        ↓
Suggests Materials, Trades and Equipment
        ↓
Contractor Reviews and Confirms
        ↓
System Retrieves / Displays Reference Material Prices
        ↓
Contractor Confirms or Overrides Prices
        ↓
Deterministic Estimation Engine
        ↓
Calculates

Materials
Labour
Subcontractors
Equipment
Estimated Cost
        ↓
System Calculates

Expected Profit
Profit Margin
Selling Price
        ↓
AI Quotation Assistant
        ↓
Generates Professional Scope Wording
        ↓
Contractor Reviews
        ↓
Customer-Facing Quotation Created
        ↓
Client Access Grant Created
        ↓
Secure Quotation Link Sent
        ↓
Client Reviews

Scope
Quantities
Selling Prices
Taxes
Payment Terms
Grand Total
        ↓
Client Accepts
        ↓
Approval Recorded
        ↓
Contractor Reviews Material Requirements
        ↓
Creates Supplier RFQ
        ↓
System Opens Supplier Directory
        ↓
System Suggests Relevant / Preferred Suppliers
        ↓
Contractor Selects One or More Suppliers
        ↓
One RFQ Invitation Created per Supplier
        ↓
One Supplier-Specific Access Grant Created per Invitation
        ↓
One Unique Secure Link Generated per Supplier
        ↓
Contractor Sends Each Invitation via

Email
WhatsApp
Copy Secure Link
        ↓
Supplier Opens Their Restricted RFQ Page
        ↓
System Associates Response with Supplier and RFQ Invitation
        ↓
Supplier Submits

Price
Availability
Delivery Fee
Lead Time
Alternatives
        ↓
Contractor Reviews Supplier Offer
        ↓
Contractor Selects Offer
        ↓
Project Begins
        ↓
Contractor Records

Actual Material Costs
Actual Labour Costs
Subcontractor Costs
Other Costs
        ↓
Contractor Records Customer Payments
        ↓
System Compares

Estimated Cost
vs
Committed Cost
vs
Actual Cost
        ↓
System Displays

Contract Value
Actual Cost
Projected Final Cost
Expected Profit
Projected Profit
Outstanding Customer Payments
        ↓
AI Project Copilot

Summarizes
Explains
Generates Reports
Answers Project Questions
```

---

# 66. Phase 1 Core Product Definition

The Phase 1 MVP succeeds when:

**A renovation contractor can organize a Project by physical Space, describe the renovation required, use AI to accelerate Scope and Resource planning, preview approximate Material pricing, estimate the Materials and Labour required, calculate the true expected Project Cost and Profit, generate a transparent Client-facing Quotation with complete selling-price information, securely share that Quotation with the Client for review and approval, select Suppliers from a company Supplier Directory, send Supplier-specific RFQ Invitations through email, WhatsApp, or secure links, receive correctly attributed Supplier Offers through restricted RFQ access, and track whether the Project is actually achieving the expected financial outcome after construction begins.**

The technical foundation established in Phase 1 is:

```text
Project
+
Space
+
Work
+
Resource
+
Cost
+
Estimate
+
Quotation
+
Revenue
+
Access Grant
+
Material Requirement
+
RFQ
+
RFQ Invitation
+
Supplier Offer
+
AI Suggestions
```

Future systems extend the same architecture:

```text
LiDAR
↓
Creates Space and Geometry

Computer Vision
↓
Creates Defect Suggestions

Defect Verification
↓
Creates Work

Material Intelligence
↓
Improves Reference Pricing

Procurement
↓
Creates Committed Costs

Supplier Network
↓
Provides Offers and Availability

Construction
↓
Creates Actual Costs

Client Approval
↓
Creates Contract Revenue

Digital Twin
↓
Connects Physical Objects to Work and Cost

Architect / Engineer Collaboration
↓
Uses Scoped Access Grants

Compliance
↓
Adds Professional and Regulatory Review
```

The platform maintains clear information boundaries:

```text
CONTRACTOR

Internal Estimate
Costs
Margins
Profit
Supplier Offers
Project Financial Dashboard
```

```text
CLIENT

Specific Quotation
Complete Selling-Price Breakdown
Scope
Terms
Payment Schedule
Approval Actions
```

```text
SUPPLIER

Specific RFQ Invitation
Specific RFQ
Requested Materials
Quantities
Specifications
Delivery Requirements
Pricing Response
```

Phase 1 is therefore not a temporary Quotation application.

It is the first commercially usable version of the complete Renovation Project Intelligence and Procurement platform.

AI is introduced only where it accelerates contractor productivity without replacing deterministic calculations, verified Project data, contractual controls, financial truth, or professional judgment.

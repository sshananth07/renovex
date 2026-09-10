package labour

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// LabourEntry represents labour applied to a WorkItem. WorkerID is optional —
// nil for ad-hoc labour paid outside the Worker catalog (design spec §1.3.3).
// Subcontractor costs are never represented as a LabourEntry — they are
// CostItems with Category=subcontractor, created directly through
// internal/costs. CostItemID references the one linked CostItem this entry
// created; the relationship is one-directional — CostItem stores no
// back-reference (design spec §1.3, §22-B).
type LabourEntry struct {
	ID            string            `bson:"_id,omitempty" json:"id"`
	CompanyID     string            `bson:"companyId" json:"companyId"`
	ProjectID     string            `bson:"projectId" json:"projectId"`
	WorkItemID    string            `bson:"workItemId" json:"workItemId"`
	WorkerID      *string           `bson:"workerId,omitempty" json:"workerId,omitempty"`
	WorkerName    string            `bson:"workerName" json:"workerName"`
	Trade         string            `bson:"trade,omitempty" json:"trade,omitempty"`
	Quantity      quantity.Quantity `bson:"-" json:"-"` // never BSON-marshaled directly — see quantityDoc in repository_mongo.go
	Rate          money.Money       `bson:"rate" json:"rate"`
	Cost          money.Money       `bson:"cost" json:"cost"`
	CostItemID    string            `bson:"costItemId" json:"costItemId"`
	Date          time.Time         `bson:"date" json:"date"`
	Notes         string            `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt     time.Time         `bson:"createdAt" json:"createdAt"`
	SchemaVersion int               `bson:"schemaVersion" json:"schemaVersion"`
}

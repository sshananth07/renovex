export interface JourneyData {
  companyName: string;
  email: string;
  password: string;
  clientName: string;
  clientEmail: string;
  projectName: string;
  propertyAddress: string;
  spaceName: string;
  workItemDescription: string;
  editedWorkItemDescription: string;
  quantity: string;
  unit: string;
}

export function uniqueJourneyData(label: string): JourneyData {
  const unique = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return {
    companyName: `F1 ${label} Contractors ${unique}`,
    email: `f1-${label.toLowerCase()}-${unique}@example.test`,
    password: "F1-Journey!2026",
    clientName: `Client ${label} ${unique}`,
    clientEmail: `client-${label.toLowerCase()}-${unique}@example.test`,
    projectName: `Renovation ${label} ${unique}`,
    propertyAddress: `18 Jalan F1 ${label}, Kuala Lumpur ${unique}`,
    spaceName: `Kitchen ${label} ${unique}`,
    workItemDescription: `Install cabinetry ${label} ${unique}`,
    editedWorkItemDescription: `Install fitted cabinetry ${label} ${unique}`,
    quantity: "12.375",
    unit: "m²",
  };
}

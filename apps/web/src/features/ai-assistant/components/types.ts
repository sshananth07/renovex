export interface SpaceAcceptanceOverride {
  name: string;
  spaceType: string;
  description: string;
}

export interface WorkItemAcceptanceInput {
  description: string;
  workType: string;
  scopeLevel: "space" | "project";
  spaceId: string | null;
  quantityValue: string;
  quantityUnit: string;
}

export interface Material {
  id: string;
  name: string;
}

export interface NewMaterialFormInput {
  name: string;
  category: string;
  specification: string;
  unit: string;
  referencePriceAmount: number;
  referencePriceCurrency: string;
}

import { apiClient, setAccessToken } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";

export type MeOutput = components["schemas"]["MeOutputBody"];

export async function login(email: string, password: string): Promise<void> {
  const { data, error } = await apiClient.POST("/auth/login", {
    body: { email, password },
  });
  if (error) {
    throw error;
  }
  setAccessToken(data.accessToken);
}

export async function register(input: {
  email: string;
  password: string;
  companyName: string;
}): Promise<void> {
  const { data, error } = await apiClient.POST("/auth/register", {
    body: input,
  });
  if (error) {
    throw error;
  }
  setAccessToken(data.accessToken);
}

export async function logout(): Promise<void> {
  await apiClient.POST("/auth/logout");
}

export async function getMe(): Promise<MeOutput> {
  const { data, error } = await apiClient.GET("/auth/me");
  if (error) {
    throw error;
  }
  return data;
}

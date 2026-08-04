import { apiRequest } from './client';
import type { Category, Tool } from '../types/api';

export interface ToolFilters {
  categoryID?: string;
  search?: string;
  available?: boolean;
  limit?: number;
  offset?: number;
}

export function getCategories(): Promise<Category[]> {
  return apiRequest('/api/v1/categories');
}

export function getTools(filters: ToolFilters = {}): Promise<Tool[]> {
  const params = new URLSearchParams();
  if (filters.categoryID) params.set('category_id', filters.categoryID);
  if (filters.search) params.set('search', filters.search);
  if (filters.available !== undefined) params.set('available', String(filters.available));
  params.set('limit', String(filters.limit ?? 50));
  params.set('offset', String(filters.offset ?? 0));
  return apiRequest(`/api/v1/tools?${params.toString()}`);
}

export function getTool(id: string): Promise<Tool> {
  return apiRequest(`/api/v1/tools/${encodeURIComponent(id)}`);
}

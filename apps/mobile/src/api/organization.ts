import { apiRequest } from './client';
import type { Organization, OrganizationInput } from '../types/api';

export function getOrganization(token: string): Promise<Organization> {
  return apiRequest('/api/v1/me/organization', { token });
}

export function putOrganization(token: string, input: OrganizationInput): Promise<Organization> {
  return apiRequest('/api/v1/me/organization', { method: 'PUT', token, body: input });
}

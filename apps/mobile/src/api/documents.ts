import { apiRequest } from './client';
import type { ClientDocument, DocumentType } from '../types/api';
import { File } from 'expo-file-system';
export interface UploadAsset {
  uri: string;
  mimeType?: string | null;
  fileName?: string | null;
}

export function getDocuments(token: string): Promise<ClientDocument[]> {
  return apiRequest('/api/v1/me/documents', { token });
}

export function deleteDocument(token: string, documentID: string): Promise<void> {
  return apiRequest(`/api/v1/me/documents/${encodeURIComponent(documentID)}`, {
    method: 'DELETE',
    token,
  });
}

export function submitDocuments(token: string): Promise<void> {
  return apiRequest('/api/v1/me/documents/submit', {
    method: 'POST',
    token,
  });
}

export async function uploadDocument(
  token: string,
  documentType: DocumentType,
  asset: UploadAsset,
): Promise<void> {
  const file = new File(asset.uri);

  if (!file.exists) {
    throw new Error(`Файл не найден: ${asset.uri}`);
  }

  const form = new FormData();

  form.append('document_type', documentType);
  form.append('file', file);

  await apiRequest<void>('/api/v1/me/documents', {
    method: 'POST',
    token,
    body: form,
  });
}

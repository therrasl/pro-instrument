import * as FileSystem from 'expo-file-system/legacy';
import { API_URL, apiRequest } from './client';
import type {
  Payment,
  Rental,
  RentalInput,
  RentalQuote,
  OrderDocument,
  ExtensionQuote,
  RentalExtension,
  InspectionPhoto,
  PhotoPhase,
  PhotoType,
} from '../types/api';

export function getRentalQuote(
  token: string,
  input: RentalInput,
): Promise<RentalQuote> {
  return apiRequest('/api/v1/rentals/quote', {
    method: 'POST',
    token,
    body: input,
    timeoutMs: 10_000,
  });
}

export function getRentalDocuments(token: string, id: string): Promise<OrderDocument[]> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(id)}/documents`, { token });
}

export async function downloadRentalDocument(token: string, rentalID: string, document: OrderDocument): Promise<string> {
  const directory = `${FileSystem.cacheDirectory}order-documents/`;
  await FileSystem.makeDirectoryAsync(directory, { intermediates: true });
  const safeName = `${rentalID}-${document.id}.pdf`;
  const result = await FileSystem.downloadAsync(`${API_URL}${document.download_url}`, `${directory}${safeName}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (result.status !== 200) throw new Error('Не удалось скачать документ.');
  return result.uri;
}

export function createRental(
  token: string,
  input: RentalInput,
): Promise<Rental> {
  return apiRequest('/api/v1/rentals', {
    method: 'POST',
    token,
    body: input,
    timeoutMs: 15_000,
  });
}

export function getRentals(
  token: string,
  limit = 100,
  offset = 0,
): Promise<Rental[]> {
  const params = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });
  return apiRequest(`/api/v1/rentals?${params.toString()}`, { token });
}

export function getRental(token: string, id: string): Promise<Rental> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(id)}`, { token });
}

export function cancelRental(token: string, id: string): Promise<Rental> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
    token,
    timeoutMs: 15_000,
  });
}

export function createRentalPayment(
  token: string,
  id: string,
): Promise<Payment> {
  return apiRequest(
    `/api/v1/rentals/${encodeURIComponent(id)}/payment`,
    {
      method: 'POST',
      token,
      timeoutMs: 20_000,
    },
  );
}

export function getExtensionQuote(
  token: string,
  rentalID: string,
  newEndDate: string,
): Promise<ExtensionQuote> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(rentalID)}/extension-quote`, {
    method: 'POST',
    token,
    body: { new_end_date: newEndDate },
    timeoutMs: 10_000,
  });
}

export function createExtension(
  token: string,
  rentalID: string,
  newEndDate: string,
): Promise<RentalExtension> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(rentalID)}/extensions`, {
    method: 'POST',
    token,
    body: { new_end_date: newEndDate },
    timeoutMs: 20_000,
  });
}

export function getExtensions(
  token: string,
  rentalID: string,
): Promise<RentalExtension[]> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(rentalID)}/extensions`, {
    token,
  });
}

export function getInspectionPhotos(
  token: string,
  rentalID: string,
): Promise<InspectionPhoto[]> {
  return apiRequest(`/api/v1/rentals/${encodeURIComponent(rentalID)}/photos`, {
    token,
  });
}

export async function uploadInspectionPhoto(
  token: string,
  rentalID: string,
  phase: PhotoPhase,
  photoType: PhotoType,
  asset: { uri: string; fileName?: string | null; mimeType?: string | null },
  comment?: string,
): Promise<InspectionPhoto> {
  const form = new FormData();
  form.append('phase', phase);
  form.append('photo_type', photoType);
  if (comment) {
    form.append('comment', comment);
  }
  const filename = asset.fileName || `${photoType}.jpg`;
  const mimeType = asset.mimeType || 'image/jpeg';
  form.append('file', {
    uri: asset.uri,
    name: filename,
    type: mimeType,
  } as unknown as Blob);

  return apiRequest(`/api/v1/rentals/${encodeURIComponent(rentalID)}/photos`, {
    method: 'POST',
    token,
    body: form,
    timeoutMs: 30_000,
  });
}

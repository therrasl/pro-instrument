import { apiRequest } from './client';
import type {
  Payment,
  Rental,
  RentalInput,
  RentalQuote,
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

import { describe, expect, it } from 'vitest';
import { isSafeConfirmationURL } from './payment-url';

describe('payment confirmation URL', () => {
  it('allows only valid HTTPS provider redirects', () => {
    expect(isSafeConfirmationURL('https://yookassa.ru/checkout/123')).toBe(true);
    expect(isSafeConfirmationURL('http://yookassa.ru/checkout/123')).toBe(false);
    expect(isSafeConfirmationURL('pro-instrument://payment-return')).toBe(false);
    expect(isSafeConfirmationURL('not a url')).toBe(false);
  });
});

import { describe, expect, it } from 'vitest';
import { formatPhone, getPhoneError, normalizeOTP } from './auth';

describe('phone input', () => {
  it('formats Russian numbers consistently', () => {
    expect(formatPhone('89991234567')).toBe('+7 (999) 123-45-67');
    expect(formatPhone('9991234567')).toBe('+7 (999) 123-45-67');
  });

  it('requires all ten national digits', () => {
    expect(getPhoneError('+7 (999) 123-45-67')).toBe('');
    expect(getPhoneError('+7 (999) 123')).not.toBe('');
    expect(getPhoneError('')).not.toBe('');
  });
});

describe('OTP input', () => {
  it('accepts paste, strips non-digits and caps the code at six digits', () => {
    expect(normalizeOTP('12 34-56')).toBe('123456');
    expect(normalizeOTP('123456789')).toBe('123456');
  });
});

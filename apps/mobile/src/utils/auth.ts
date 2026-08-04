export function formatPhone(value: string): string {
  let digits = value.replace(/\D/g, '');
  if (!digits) return '';

  if (digits.startsWith('8')) {
    digits = `7${digits.slice(1)}`;
  } else if (!digits.startsWith('7')) {
    digits = `7${digits}`;
  }

  const national = digits.slice(1, 11);
  let formatted = '+7';
  if (national.length > 0) formatted += ` (${national.slice(0, 3)}`;
  if (national.length >= 3) formatted += ')';
  if (national.length > 3) formatted += ` ${national.slice(3, 6)}`;
  if (national.length > 6) formatted += `-${national.slice(6, 8)}`;
  if (national.length > 8) formatted += `-${national.slice(8, 10)}`;
  return formatted;
}

export function getPhoneError(value: string): string {
  if (!value.trim()) return 'Введите номер телефона.';
  if (!/^\+7 \(\d{3}\) \d{3}-\d{2}-\d{2}$/.test(value)) {
    return 'Введите номер полностью: +7 (999) 000-00-00.';
  }
  return '';
}

export function normalizeOTP(value: string): string {
  return value.replace(/\D/g, '').slice(0, 6);
}

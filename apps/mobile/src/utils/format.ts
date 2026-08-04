import type { ClientStatus, DocumentType, StructuredValue } from '../types/api';

export function formatMoney(kopecks: number): string {
  const rubles = kopecks / 100;
  return `${new Intl.NumberFormat('ru-RU', {
    minimumFractionDigits: kopecks % 100 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(rubles)} ₽`;
}

export const clientStatusLabel: Record<ClientStatus, string> = {
  registered: 'Документы не загружены',
  phone_verified: 'Документы не загружены',
  profile_completed: 'Документы не загружены',
  documents_uploaded: 'Документы готовы к отправке',
  pending_verification: 'Документы на проверке',
  verified: 'Документы подтверждены',
  verification_rejected: 'Документы отклонены',
  blocked: 'Доступ ограничен',
};

export const documentTypeLabel: Record<DocumentType, string> = {
  passport_main: 'Главная страница паспорта',
  passport_registration: 'Страница с регистрацией',
  selfie_with_passport: 'Селфи с паспортом',
};

const specificationMetadata: Record<string, { label: string; unit?: string }> = {
  power_w: { label: 'Мощность', unit: 'Вт' },
  impact_energy_j: { label: 'Энергия удара', unit: 'Дж' },
  disc_diameter_mm: { label: 'Диаметр диска', unit: 'мм' },
  cutting_width_cm: { label: 'Ширина скашивания', unit: 'см' },
  collector_l: { label: 'Объём травосборника', unit: 'л' },
  weight_kg: { label: 'Вес', unit: 'кг' },
  voltage_v: { label: 'Напряжение', unit: 'В' },
  battery_capacity_ah: { label: 'Ёмкость аккумулятора', unit: 'А·ч' },
  torque_nm: { label: 'Крутящий момент', unit: 'Н·м' },
  speed_rpm: { label: 'Частота вращения', unit: 'об/мин' },
  drilling_diameter_mm: { label: 'Диаметр сверления', unit: 'мм' },
  cutting_depth_mm: { label: 'Глубина пропила', unit: 'мм' },
  tank_volume_l: { label: 'Объём бака', unit: 'л' },
};

export function formatSpecificationLabel(key: string): string {
  return specificationMetadata[key]?.label ?? 'Дополнительная характеристика';
}

export function formatSpecificationValue(
  key: string,
  value: StructuredValue,
): string {
  const unit = specificationMetadata[key]?.unit;
  if (typeof value !== 'number' || !unit) return displayValue(value);

  return `${new Intl.NumberFormat('ru-RU', {
    maximumFractionDigits: 2,
  }).format(value)} ${unit}`;
}

export function displayValue(value: StructuredValue): string {
  if (value === null) return '—';
  if (Array.isArray(value)) return value.map(displayValue).join(', ');
  if (typeof value === 'object') {
    return Object.entries(value)
      .map(([key, nested]) => `${key}: ${displayValue(nested)}`)
      .join(', ');
  }
  if (typeof value === 'boolean') return value ? 'Да' : 'Нет';
  return String(value);
}

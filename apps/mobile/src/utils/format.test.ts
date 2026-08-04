import { describe, expect, it } from 'vitest';
import {
  displayValue,
  formatMoney,
  formatSpecificationLabel,
  formatSpecificationValue,
} from './format';

describe('formatMoney', () => {
  it('formats whole rubles from kopecks', () => {
    expect(formatMoney(129900)).toContain('1');
    expect(formatMoney(129900)).toContain('299');
    expect(formatMoney(129900)).toContain('₽');
  });
});

describe('displayValue', () => {
  it('renders structured values', () => {
    expect(displayValue({ power: 800, modes: ['eco', 'turbo'] })).toContain('power: 800');
    expect(displayValue(true)).toBe('Да');
  });
});

describe('tool specification formatting', () => {
  it('renders API keys as readable Russian labels', () => {
    expect(formatSpecificationLabel('power_w')).toBe('Мощность');
    expect(formatSpecificationLabel('impact_energy_j')).toBe('Энергия удара');
    expect(formatSpecificationLabel('disc_diameter_mm')).toBe('Диаметр диска');
    expect(formatSpecificationLabel('cutting_width_cm')).toBe('Ширина скашивания');
    expect(formatSpecificationLabel('collector_l')).toBe('Объём травосборника');
  });

  it('adds localized measurement units to numeric values', () => {
    expect(formatSpecificationValue('power_w', 830)).toBe('830 Вт');
    expect(formatSpecificationValue('impact_energy_j', 2.7)).toMatch(/^2[,.]7 Дж$/);
    expect(formatSpecificationValue('disc_diameter_mm', 190)).toBe('190 мм');
  });

  it('never exposes an unknown technical key to the user', () => {
    expect(formatSpecificationLabel('future_api_key')).toBe(
      'Дополнительная характеристика',
    );
  });
});

import { describe, expect, it } from 'vitest';
import type { RentalStatus } from '../types/api';
import {
  buildRentalTimeline,
  historicalRentalStatuses,
  pollingRentalStatuses,
  rentalStatusPresentation,
} from './status';

describe('rental status presentation', () => {
  it('has Russian copy and a next action for every backend status', () => {
    const statuses: RentalStatus[] = [
      'pending_manager',
      'awaiting_payment',
      'paid',
      'preparing',
      'ready',
      'handed_to_courier',
      'rented',
      'awaiting_return',
      'inspection',
      'completed',
      'rejected',
      'cancelled',
      'payment_expired',
    ];

    expect(Object.keys(rentalStatusPresentation)).toHaveLength(statuses.length);
    for (const status of statuses) {
      expect(rentalStatusPresentation[status].label).toBeTruthy();
      expect(rentalStatusPresentation[status].nextAction).toBeTruthy();
    }
  });

  it('uses the customer-facing status labels', () => {
    expect(
      Object.fromEntries(
        Object.entries(rentalStatusPresentation).map(([status, value]) => [
          status,
          value.label,
        ]),
      ),
    ).toEqual({
      pending_manager: 'Проверяем заявку',
      awaiting_payment: 'Можно оплачивать',
      paid: 'Оплачено',
      preparing: 'Готовим инструмент',
      ready: 'Готово к получению',
      handed_to_courier: 'Передано курьеру',
      rented: 'Инструмент у вас',
      awaiting_return: 'Ожидаем возврат',
      inspection: 'Проверяем инструмент',
      completed: 'Аренда завершена',
      rejected: 'Заявка отклонена',
      cancelled: 'Заявка отменена',
      payment_expired: 'Время на оплату истекло',
    });
  });

  it('polls active statuses and stops for terminal statuses', () => {
    expect(pollingRentalStatuses.has('pending_manager')).toBe(true);
    expect(pollingRentalStatuses.has('inspection')).toBe(true);
    expect(pollingRentalStatuses.has('completed')).toBe(false);
    expect(pollingRentalStatuses.has('rejected')).toBe(false);
    expect(historicalRentalStatuses.has('completed')).toBe(true);
    expect(historicalRentalStatuses.has('paid')).toBe(false);
  });

  it('omits the courier stage for self pickup', () => {
    const pickup = buildRentalTimeline('ready', 'self_pickup');
    const courier = buildRentalTimeline('ready', 'courier');

    expect(pickup.some((stage) => stage.key === 'courier')).toBe(false);
    expect(courier.some((stage) => stage.key === 'courier')).toBe(true);
    expect(pickup.find((stage) => stage.key === 'ready')?.state).toBe('current');
  });
});

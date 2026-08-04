import type {
  DeliveryMethod,
  RentalStatus,
} from '../types/api';

export type StatusTone = 'neutral' | 'success' | 'warning' | 'error';

interface RentalStatusPresentation {
  label: string;
  nextAction: string;
  tone: StatusTone;
}

export const rentalStatusPresentation: Record<
  RentalStatus,
  RentalStatusPresentation
> = {
  pending_manager: {
    label: 'Проверяем заявку',
    nextAction: 'Менеджер проверит даты и наличие инструмента.',
    tone: 'warning',
  },
  awaiting_payment: {
    label: 'Можно оплачивать',
    nextAction: 'Оплатите заказ, чтобы мы начали готовить инструмент.',
    tone: 'warning',
  },
  paid: {
    label: 'Оплачено',
    nextAction: 'Оплата прошла. Скоро начнём готовить инструмент.',
    tone: 'success',
  },
  preparing: {
    label: 'Готовим инструмент',
    nextAction: 'Проверяем комплектность и готовим заказ к выдаче.',
    tone: 'neutral',
  },
  ready: {
    label: 'Готово к получению',
    nextAction: 'Инструмент готов. Получите его выбранным способом.',
    tone: 'success',
  },
  handed_to_courier: {
    label: 'Передано курьеру',
    nextAction: 'Инструмент передан курьеру и направляется по указанному адресу.',
    tone: 'neutral',
  },
  rented: {
    label: 'Инструмент у вас',
    nextAction: 'Верните инструмент до окончания аренды.',
    tone: 'success',
  },
  awaiting_return: {
    label: 'Ожидаем возврат',
    nextAction: 'Верните инструмент выбранным способом.',
    tone: 'warning',
  },
  inspection: {
    label: 'Проверяем инструмент',
    nextAction: 'Проверяем состояние и комплектность после возврата.',
    tone: 'neutral',
  },
  completed: {
    label: 'Аренда завершена',
    nextAction: 'Заявка закрыта. Спасибо, что выбрали «Про Инструмент».',
    tone: 'success',
  },
  rejected: {
    label: 'Заявка отклонена',
    nextAction: 'Менеджер не смог подтвердить заявку. Выберите другой инструмент или даты.',
    tone: 'error',
  },
  cancelled: {
    label: 'Заявка отменена',
    nextAction: 'Эта заявка отменена. При необходимости оформите новую.',
    tone: 'error',
  },
  payment_expired: {
    label: 'Время на оплату истекло',
    nextAction: 'Оформите новую заявку или свяжитесь с менеджером.',
    tone: 'error',
  },
};

export const historicalRentalStatuses = new Set<RentalStatus>([
  'completed',
  'rejected',
  'cancelled',
  'payment_expired',
]);

export const pollingRentalStatuses = new Set<RentalStatus>([
  'pending_manager',
  'awaiting_payment',
  'paid',
  'preparing',
  'ready',
  'handed_to_courier',
  'rented',
  'awaiting_return',
  'inspection',
]);

export interface RentalTimelineStage {
  key: string;
  label: string;
  state: 'completed' | 'current' | 'upcoming';
}

const standardStages: Array<{
  key: string;
  label: string;
  status?: RentalStatus;
  courierOnly?: boolean;
}> = [
  { key: 'submitted', label: 'Заявка отправлена' },
  {
    key: 'manager',
    label: 'Проверка менеджером',
    status: 'pending_manager',
  },
  {
    key: 'payment',
    label: 'Ожидает оплаты',
    status: 'awaiting_payment',
  },
  { key: 'paid', label: 'Оплачено', status: 'paid' },
  {
    key: 'preparing',
    label: 'Подготовка инструмента',
    status: 'preparing',
  },
  { key: 'ready', label: 'Готово к получению', status: 'ready' },
  {
    courierOnly: true,
    key: 'courier',
    label: 'Передано курьеру',
    status: 'handed_to_courier',
  },
  { key: 'rented', label: 'Выдано / в аренде', status: 'rented' },
  {
    key: 'awaiting_return',
    label: 'Ожидает возврата',
    status: 'awaiting_return',
  },
  {
    key: 'inspection',
    label: 'Возвращено / на проверке',
    status: 'inspection',
  },
  { key: 'completed', label: 'Аренда завершена', status: 'completed' },
];

const terminalTimelineLabel: Partial<Record<RentalStatus, string>> = {
  rejected: 'Заявка отклонена',
  cancelled: 'Заявка отменена',
  payment_expired: 'Время на оплату истекло',
};

export function buildRentalTimeline(
  status: RentalStatus,
  deliveryMethod: DeliveryMethod,
): RentalTimelineStage[] {
  const stages = standardStages.filter(
    (stage) => !stage.courierOnly || deliveryMethod === 'courier',
  );
  const terminalLabel = terminalTimelineLabel[status];

  if (terminalLabel) {
    const completedKeys =
      status === 'payment_expired'
        ? new Set(['submitted', 'manager', 'payment'])
        : new Set(['submitted']);
    const visibleStages = stages
      .filter((stage) =>
        status === 'payment_expired'
          ? completedKeys.has(stage.key)
          : stage.key === 'submitted',
      )
      .map<RentalTimelineStage>((stage) => ({
        key: stage.key,
        label: stage.label,
        state: 'completed',
      }));

    return [
      ...visibleStages,
      {
        key: status,
        label: terminalLabel,
        state: 'current',
      },
    ];
  }

  const currentIndex =
    status === 'pending_manager'
      ? 1
      : stages.findIndex((stage) => stage.status === status);

  return stages.map<RentalTimelineStage>((stage, index) => ({
    key: stage.key,
    label: stage.label,
    state:
      index < currentIndex
        ? 'completed'
        : index === currentIndex
          ? 'current'
          : 'upcoming',
  }));
}

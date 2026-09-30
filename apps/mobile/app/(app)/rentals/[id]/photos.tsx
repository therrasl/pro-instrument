import { Ionicons } from '@expo/vector-icons';
import * as ImagePicker from 'expo-image-picker';
import { useLocalSearchParams, useRouter, type Href } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Image,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { getTool } from '../../../../src/api/catalog';
import { resolveAPIAssetURL } from '../../../../src/api/client';
import {
  getInspectionPhotos,
  getRental,
  getRentals,
  uploadInspectionPhoto,
} from '../../../../src/api/rentals';
import { useSession } from '../../../../src/auth/session';
import { Button, Page, StateView, Title } from '../../../../src/components/ui';
import { colors, radius, spacing, typography } from '../../../../src/theme/tokens';
import type {
  InspectionPhoto,
  PhotoPhase,
  PhotoType,
  Rental,
} from '../../../../src/types/api';

interface PhotoSlotDefinition {
  type: PhotoType;
  title: string;
  description: string;
  icon: keyof typeof Ionicons.glyphMap;
}

const PHOTO_SLOTS: PhotoSlotDefinition[] = [
  {
    type: 'body',
    title: '1. Общий вид и корпус',
    description: 'Чёткий снимок инструмента целиком для фиксации внешнего состояния',
    icon: 'construct-outline',
  },
  {
    type: 'equipment',
    title: '2. Комплектация',
    description: 'Кейс, сменные насадки, ключи, оснастка и принадлежности',
    icon: 'briefcase-outline',
  },
  {
    type: 'battery',
    title: '3. Аккумулятор / сетевой кабель',
    description: 'Состояние контактов аккумулятора или целостность сетевого провода',
    icon: 'battery-charging-outline',
  },
  {
    type: 'serial_number',
    title: '4. Серийный номер / шильдик',
    description: 'Заводская табличка с серийным номером инструмента и моделью',
    icon: 'barcode-outline',
  },
  {
    type: 'cleanliness',
    title: '5. Чистота и целостность',
    description: 'Отсутствие сколов, трещин, грязи и следов падений',
    icon: 'sparkles-outline',
  },
];

export interface ReturnQueueItem {
  id: string;
  orderNumber: string;
  toolName: string;
}

export default function RentalPhotosScreen() {
  const router = useRouter();
  const { id: rawID } = useLocalSearchParams<{ id: string | string[] }>();
  const rentalID = Array.isArray(rawID) ? rawID[0] : rawID;
  const { token } = useSession();

  const [rental, setRental] = useState<Rental | null>(null);
  const [photos, setPhotos] = useState<InspectionPhoto[]>([]);
  const [phase, setPhase] = useState<PhotoPhase>('handover');
  const [otherReturnRentals, setOtherReturnRentals] = useState<ReturnQueueItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [uploadingSlot, setUploadingSlot] = useState<PhotoType | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(async (isRefresh = false) => {
    if (!token || !rentalID) return;
    if (isRefresh) setRefreshing(true);
    else setLoading(true);
    setError('');

    try {
      const [rentalData, photosData, allRentals] = await Promise.all([
        getRental(token, rentalID),
        getInspectionPhotos(token, rentalID),
        getRentals(token).catch(() => [] as Rental[]),
      ]);
      setRental(rentalData);
      setPhotos(photosData);

      // Auto-select phase strictly based on rental operational stage
      if (['awaiting_return', 'inspection'].includes(rentalData.status)) {
        setPhase('return');
      } else {
        setPhase('handover');
      }

      // Find other rentals awaiting return inspection for sequential queue
      const otherPending = allRentals.filter(
        (r) => r.id !== rentalID && ['awaiting_return', 'inspection'].includes(r.status),
      );
      if (otherPending.length > 0) {
        const summaries = await Promise.all(
          otherPending.map(async (r) => {
            try {
              const t = await getTool(r.tool_id);
              return {
                id: r.id,
                orderNumber: r.order_number ?? r.id.slice(0, 8).toUpperCase(),
                toolName: t.name,
              };
            } catch {
              return {
                id: r.id,
                orderNumber: r.order_number ?? r.id.slice(0, 8).toUpperCase(),
                toolName: 'Инструмент',
              };
            }
          }),
        );
        setOtherReturnRentals(summaries);
      } else {
        setOtherReturnRentals([]);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Не удалось загрузить фотографии.');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [rentalID, token]);

  useEffect(() => {
    void load();
  }, [load]);

  // Stage checks
  const isHandoverStage = Boolean(
    rental && ['rented', 'ready', 'handed_to_courier'].includes(rental.status),
  );
  const isReturnStage = Boolean(
    rental && ['awaiting_return', 'inspection'].includes(rental.status),
  );

  // An inspection phase is ONLY editable if the current rental status matches it:
  // - Handover photos can ONLY be taken during handover stage
  // - Return photos can ONLY be taken during return stage
  const isPhaseEditable =
    (phase === 'handover' && isHandoverStage) ||
    (phase === 'return' && isReturnStage);

  const handleCameraCapture = async (slotType: PhotoType) => {
    if (!token || !rentalID || uploadingSlot) return;

    if (!isPhaseEditable) {
      Alert.alert(
        'Редактирование недоступно',
        phase === 'handover'
          ? 'Фотофиксация при выдаче завершена и зафиксирована.'
          : 'Фотофиксация при возврате доступна только на этапе сдачи инструмента.',
      );
      return;
    }

    try {
      const permission = await ImagePicker.requestCameraPermissionsAsync();
      if (!permission.granted) {
        Alert.alert(
          'Нет доступа к камере',
          'Предоставьте приложению доступ к камере в настройках устройства для фиксации состояния инструмента.',
        );
        return;
      }

      let result: ImagePicker.ImagePickerResult;
      try {
        result = await ImagePicker.launchCameraAsync({
          mediaTypes: ['images'],
          quality: 0.85,
        });
      } catch (cameraErr) {
        Alert.alert(
          'Камера недоступна',
          'Не удалось запустить камеру на устройстве: ' +
            (cameraErr instanceof Error ? cameraErr.message : String(cameraErr)),
        );
        return;
      }

      if (result.canceled || !result.assets || result.assets.length === 0) {
        return;
      }

      const asset = result.assets[0];
      setUploadingSlot(slotType);
      setError('');

      const uploaded = await uploadInspectionPhoto(
        token,
        rentalID,
        phase,
        slotType,
        {
          uri: asset.uri,
          fileName: asset.fileName || `${phase}_${slotType}.jpg`,
          mimeType: asset.mimeType || 'image/jpeg',
        },
      );

      // Replace or add photo in local state
      setPhotos((prev) => {
        const filtered = prev.filter(
          (p) => !(p.phase === phase && p.photo_type === slotType),
        );
        return [...filtered, uploaded];
      });
    } catch (cause) {
      Alert.alert(
        'Ошибка сохранения',
        cause instanceof Error ? cause.message : 'Не удалось сохранить фотографию.',
      );
    } finally {
      setUploadingSlot(null);
    }
  };

  const getPhotoForSlot = (slotType: PhotoType): InspectionPhoto | undefined => {
    return photos.find((p) => p.phase === phase && p.photo_type === slotType);
  };

  const phasePhotosCount = photos.filter((p) => p.phase === phase).length;

  if (loading && !refreshing) {
    return (
      <Page>
        <StateView
          icon="camera-outline"
          title="Загружаем осмотр"
          message="Проверяем сохранённые фотографии инструмента..."
        />
      </Page>
    );
  }

  return (
    <Page>
      <ScrollView
        contentContainerStyle={styles.container}
        refreshControl={
          <RefreshControl
            colors={[colors.primary]}
            tintColor={colors.primary}
            refreshing={refreshing}
            onRefresh={() => void load(true)}
          />
        }
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.header}>
          <Title compact>Фотофиксация инструмента</Title>
          <Text style={styles.intro}>
            {rental?.order_number
              ? `Заказ №${rental.order_number}`
              : 'Фиксация состояния инструмента в реальном времени'}
          </Text>
        </View>

        {/* Phase Segmented Control */}
        <View style={styles.phaseSelector}>
          <Pressable
            style={[
              styles.phaseButton,
              phase === 'handover' && styles.phaseButtonActive,
            ]}
            onPress={() => setPhase('handover')}
          >
            <Ionicons
              name="log-out-outline"
              size={18}
              color={phase === 'handover' ? colors.white : colors.ink}
            />
            <Text
              style={[
                styles.phaseButtonText,
                phase === 'handover' && styles.phaseButtonTextActive,
              ]}
            >
              При выдаче
            </Text>
            {isHandoverStage && phasePhotosCount < 5 ? (
              <View style={styles.phaseAlertDot} />
            ) : null}
          </Pressable>

          <Pressable
            style={[
              styles.phaseButton,
              phase === 'return' && styles.phaseButtonActive,
              isHandoverStage && styles.phaseButtonDisabled,
            ]}
            onPress={() => {
              if (isHandoverStage) {
                Alert.alert(
                  'Этап возврата недоступен',
                  'Фотофиксация при возврате станет доступна только на этапе сдачи инструмента.',
                );
                return;
              }
              setPhase('return');
            }}
          >
            <Ionicons
              name={isHandoverStage ? 'lock-closed-outline' : 'return-down-back-outline'}
              size={18}
              color={
                phase === 'return'
                  ? colors.white
                  : isHandoverStage
                    ? colors.muted
                    : colors.ink
              }
            />
            <Text
              style={[
                styles.phaseButtonText,
                phase === 'return' && styles.phaseButtonTextActive,
                isHandoverStage && styles.phaseButtonTextDisabled,
              ]}
            >
              При возврате
            </Text>
            {isReturnStage && phasePhotosCount < 5 ? (
              <View style={styles.phaseAlertDot} />
            ) : null}
          </Pressable>
        </View>

        {/* Progress & Urgent Enforcement Banner */}
        <View
          style={[
            styles.banner,
            isPhaseEditable && phasePhotosCount < 5 && styles.bannerUrgent,
            phasePhotosCount === 5 && styles.bannerSuccess,
          ]}
        >
          <View style={styles.bannerHeader}>
            <Ionicons
              name={
                phasePhotosCount === 5
                  ? 'checkmark-circle'
                  : isPhaseEditable
                    ? 'alert-circle'
                    : 'information-circle-outline'
              }
              size={22}
              color={
                phasePhotosCount === 5
                  ? colors.success
                  : isPhaseEditable
                    ? colors.error
                    : colors.primary
              }
            />
            <Text
              style={[
                styles.bannerTitle,
                isPhaseEditable && phasePhotosCount < 5 && styles.bannerTitleUrgent,
                phasePhotosCount === 5 && styles.bannerTitleSuccess,
              ]}
            >
              {phase === 'handover' ? 'Осмотр при получении: ' : 'Осмотр при возврате: '}
              {phasePhotosCount} из 5 ракурсов
            </Text>
          </View>
          <Text
            style={[
              styles.bannerText,
              isPhaseEditable && phasePhotosCount < 5 && styles.bannerTextUrgent,
            ]}
          >
            {phasePhotosCount === 5
              ? 'Все 5 обязательных ракурсов зафиксированы на камеру и защищены в сделке.'
              : isPhaseEditable
                ? phase === 'handover'
                  ? 'Внимание! Обязательно сфотографируйте инструмент на камеру при получении. Это подтвердит его целостность и защитит ваш обеспечительный платеж.'
                  : 'Внимание! Обязательно зафиксируйте инструмент на камеру перед сдачей. Возврат обеспечительного платежа производится после проверки состояния.'
                : 'Просмотр архивных снимков данного этапа.'}
          </Text>
        </View>

        {error ? <Text style={styles.errorText}>{error}</Text> : null}

        {/* 5 Slots */}
        <View style={styles.slotsList}>
          {PHOTO_SLOTS.map((slot) => {
            const photo = getPhotoForSlot(slot.type);
            const isUploading = uploadingSlot === slot.type;
            const photoUrl =
              photo?.download_url ||
              (photo ? `/api/v1/rentals/${rentalID}/photos/${photo.id}` : '');

            return (
              <View key={slot.type} style={styles.slotCard}>
                <View style={styles.slotHeader}>
                  <View style={styles.slotIconBox}>
                    <Ionicons name={slot.icon} size={20} color={colors.primary} />
                  </View>
                  <View style={styles.slotTitleBox}>
                    <Text style={styles.slotTitle}>{slot.title}</Text>
                    <Text style={styles.slotDescription}>{slot.description}</Text>
                  </View>
                  {photo ? (
                    <View style={styles.badgeSuccess}>
                      <Ionicons name="checkmark-circle" size={16} color={colors.success} />
                      <Text style={styles.badgeSuccessText}>Готово</Text>
                    </View>
                  ) : (
                    <View
                      style={[
                        styles.badgePending,
                        isPhaseEditable && styles.badgePendingUrgent,
                      ]}
                    >
                      <Text
                        style={[
                          styles.badgePendingText,
                          isPhaseEditable && styles.badgePendingTextUrgent,
                        ]}
                      >
                        Нужно фото
                      </Text>
                    </View>
                  )}
                </View>

                {/* Photo Preview if uploaded */}
                {photo ? (
                  <View style={styles.previewContainer}>
                    <Image
                      source={{ uri: resolveAPIAssetURL(photoUrl) }}
                      style={styles.previewImage}
                      resizeMode="cover"
                    />
                    <View style={styles.previewMeta}>
                      <Text style={styles.previewDate}>
                        {new Intl.DateTimeFormat('ru-RU', {
                          day: 'numeric',
                          month: 'short',
                          hour: '2-digit',
                          minute: '2-digit',
                        }).format(new Date(photo.created_at))}
                      </Text>
                      {photo.file_size ? (
                        <Text style={styles.previewSize}>
                          {Math.round(photo.file_size / 1024)} КБ
                        </Text>
                      ) : null}
                    </View>
                  </View>
                ) : null}

                {/* Action Buttons — ONLY Camera, NO Gallery */}
                <View style={styles.actionRow}>
                  {isUploading ? (
                    <View style={styles.loadingBox}>
                      <ActivityIndicator size="small" color={colors.primary} />
                      <Text style={styles.loadingText}>Сохраняем снимок...</Text>
                    </View>
                  ) : isPhaseEditable ? (
                    <Pressable
                      style={[
                        styles.cameraButton,
                        Boolean(photo) && styles.cameraButtonRetake,
                      ]}
                      onPress={() => void handleCameraCapture(slot.type)}
                    >
                      <Ionicons
                        name={photo ? 'camera-reverse-outline' : 'camera'}
                        size={18}
                        color={photo ? colors.primary : colors.white}
                      />
                      <Text
                        style={[
                          styles.cameraButtonText,
                          Boolean(photo) && styles.cameraButtonTextRetake,
                        ]}
                      >
                        {photo ? 'Переснять на камеру' : 'Сделать снимок на камеру'}
                      </Text>
                    </Pressable>
                  ) : (
                    <View style={styles.readOnlyNote}>
                      <Ionicons
                        name={photo ? 'shield-checkmark-outline' : 'alert-circle-outline'}
                        size={16}
                        color={photo ? colors.success : colors.muted}
                      />
                      <Text style={styles.readOnlyNoteText}>
                        {photo
                          ? phase === 'handover'
                            ? 'Снимок зафиксирован при выдаче'
                            : 'Снимок зафиксирован при возврате'
                          : 'Снимок на данном этапе не зафиксирован'}
                      </Text>
                    </View>
                  )}
                </View>
              </View>
            );
          })}
        </View>

        {/* Completion and Multi-Tool Queue Card */}
        {phase === 'return' && phasePhotosCount === 5 ? (
          <View style={styles.completionCard}>
            <View style={styles.completionHeader}>
              <View style={styles.completionIconBox}>
                <Ionicons name="checkmark-done-circle" size={32} color={colors.success} />
              </View>
              <View style={{ flex: 1, gap: 4 }}>
                <Text style={styles.completionTitle}>
                  {otherReturnRentals.length > 0
                    ? 'Осмотр инструмента завершён (5 из 5)!'
                    : 'Все инструменты успешно зафиксированы! 🎉'}
                </Text>
                <Text style={styles.completionDescription}>
                  {otherReturnRentals.length > 0
                    ? `Снимки сохранены. Следующий на очереди к сдаче: «${otherReturnRentals[0].toolName}» (Заказ №${otherReturnRentals[0].orderNumber}). Всего осталось сдать: ${otherReturnRentals.length}`
                    : 'Все обязательные ракурсы зафиксированы на камеру. Инструменты готовы к передаче, обеспечительный платеж будет возвращен после проверки состояния.'}
                </Text>
              </View>
            </View>

            {otherReturnRentals.length > 0 ? (
              <View style={styles.completionActions}>
                <Button
                  icon="arrow-forward-outline"
                  label={`Перейти к: ${otherReturnRentals[0].toolName}`}
                  onPress={() =>
                    router.replace(
                      `/(app)/rentals/${otherReturnRentals[0].id}/photos` as Href,
                    )
                  }
                />
                <Button
                  label="К списку моих аренд"
                  variant="secondary"
                  onPress={() => router.replace('/(app)/(tabs)/rentals' as Href)}
                />
              </View>
            ) : (
              <View style={styles.completionActions}>
                <Button
                  icon="checkmark-circle-outline"
                  label="Вернуться на главный экран"
                  onPress={() => router.replace('/(app)/(tabs)/rentals' as Href)}
                />
              </View>
            )}
          </View>
        ) : null}
      </ScrollView>
    </Page>
  );
}

const styles = StyleSheet.create({
  container: {
    padding: spacing.lg,
    gap: spacing.lg,
  },
  header: {
    gap: spacing.xs,
  },
  intro: {
    ...typography.caption,
    color: colors.muted,
  },
  phaseSelector: {
    flexDirection: 'row',
    backgroundColor: colors.surfaceSubtle,
    borderRadius: radius.md,
    padding: 4,
    gap: 4,
  },
  phaseButton: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 10,
    borderRadius: radius.sm,
    gap: spacing.xs,
  },
  phaseButtonActive: {
    backgroundColor: colors.primary,
  },
  phaseButtonDisabled: {
    opacity: 0.5,
  },
  phaseButtonText: {
    fontSize: 14,
    fontWeight: '600',
    color: colors.ink,
  },
  phaseButtonTextActive: {
    color: colors.white,
  },
  phaseButtonTextDisabled: {
    color: colors.muted,
  },
  phaseAlertDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    backgroundColor: colors.error,
  },
  banner: {
    backgroundColor: colors.surfaceSubtle,
    borderRadius: radius.md,
    padding: spacing.md,
    gap: spacing.xs,
    borderWidth: 1,
    borderColor: colors.outline,
  },
  bannerUrgent: {
    backgroundColor: '#FFF5F5',
    borderColor: '#FFD6D6',
  },
  bannerSuccess: {
    backgroundColor: colors.successSoft,
    borderColor: colors.success,
  },
  bannerHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  bannerTitle: {
    fontSize: 15,
    fontWeight: '700',
    color: colors.ink,
  },
  bannerTitleUrgent: {
    color: colors.error,
  },
  bannerTitleSuccess: {
    color: colors.success,
  },
  bannerText: {
    ...typography.caption,
    color: colors.muted,
    lineHeight: 18,
  },
  bannerTextUrgent: {
    color: colors.ink,
    fontWeight: '500',
  },
  errorText: {
    color: colors.error,
    fontSize: 14,
  },
  slotsList: {
    gap: spacing.md,
  },
  slotCard: {
    backgroundColor: colors.surface,
    borderRadius: radius.md,
    padding: spacing.md,
    borderWidth: 1,
    borderColor: colors.outline,
    gap: spacing.md,
  },
  slotHeader: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.sm,
  },
  slotIconBox: {
    width: 36,
    height: 36,
    borderRadius: radius.sm,
    backgroundColor: colors.primarySoft,
    alignItems: 'center',
    justifyContent: 'center',
  },
  slotTitleBox: {
    flex: 1,
    gap: 2,
  },
  slotTitle: {
    fontSize: 15,
    fontWeight: '700',
    color: colors.ink,
  },
  slotDescription: {
    ...typography.caption,
    color: colors.muted,
    lineHeight: 16,
  },
  badgeSuccess: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.successSoft,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: radius.pill,
    gap: 4,
  },
  badgeSuccessText: {
    fontSize: 12,
    fontWeight: '700',
    color: colors.success,
  },
  badgePending: {
    backgroundColor: colors.surfaceSubtle,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: radius.pill,
  },
  badgePendingUrgent: {
    backgroundColor: '#FFEAEA',
  },
  badgePendingText: {
    fontSize: 12,
    fontWeight: '600',
    color: colors.muted,
  },
  badgePendingTextUrgent: {
    color: colors.error,
    fontWeight: '700',
  },
  previewContainer: {
    borderRadius: radius.sm,
    overflow: 'hidden',
    backgroundColor: colors.surfaceSubtle,
    borderWidth: 1,
    borderColor: colors.outline,
  },
  previewImage: {
    width: '100%',
    height: 180,
  },
  previewMeta: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.sm,
    paddingVertical: 6,
    backgroundColor: colors.surface,
  },
  previewDate: {
    ...typography.caption,
    color: colors.muted,
  },
  previewSize: {
    ...typography.caption,
    color: colors.muted,
  },
  actionRow: {
    flexDirection: 'row',
  },
  cameraButton: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 12,
    borderRadius: radius.sm,
    backgroundColor: colors.primary,
    gap: 8,
  },
  cameraButtonRetake: {
    backgroundColor: colors.primarySoft,
  },
  cameraButtonText: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.white,
  },
  cameraButtonTextRetake: {
    color: colors.primary,
  },
  readOnlyNote: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: 6,
  },
  readOnlyNoteText: {
    ...typography.caption,
    color: colors.muted,
    fontWeight: '500',
  },
  loadingBox: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 10,
    gap: spacing.sm,
  },
  loadingText: {
    fontSize: 13,
    color: colors.muted,
    fontWeight: '600',
  },
  completionCard: {
    backgroundColor: '#F0FDF4',
    borderWidth: 1.5,
    borderColor: '#86EFAC',
    borderRadius: radius.md,
    padding: spacing.md,
    gap: spacing.md,
  },
  completionHeader: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.sm,
  },
  completionIconBox: {
    width: 44,
    height: 44,
    borderRadius: radius.pill,
    backgroundColor: '#DCFCE7',
    alignItems: 'center',
    justifyContent: 'center',
  },
  completionTitle: {
    fontSize: 16,
    fontWeight: '700',
    color: '#166534',
  },
  completionDescription: {
    fontSize: 13,
    color: '#15803D',
    lineHeight: 18,
  },
  completionActions: {
    gap: spacing.sm,
  },
});

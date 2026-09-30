import { Ionicons } from '@expo/vector-icons';
import * as ImagePicker from 'expo-image-picker';
import { useLocalSearchParams } from 'expo-router';
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
import { resolveAPIAssetURL } from '../../../../src/api/client';
import {
  getInspectionPhotos,
  getRental,
  uploadInspectionPhoto,
} from '../../../../src/api/rentals';
import { useSession } from '../../../../src/auth/session';
import { Page, StateView, Title } from '../../../../src/components/ui';
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

export default function RentalPhotosScreen() {
  const { id: rawID } = useLocalSearchParams<{ id: string | string[] }>();
  const rentalID = Array.isArray(rawID) ? rawID[0] : rawID;
  const { token } = useSession();

  const [rental, setRental] = useState<Rental | null>(null);
  const [photos, setPhotos] = useState<InspectionPhoto[]>([]);
  const [phase, setPhase] = useState<PhotoPhase>('handover');
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
      const [rentalData, photosData] = await Promise.all([
        getRental(token, rentalID),
        getInspectionPhotos(token, rentalID),
      ]);
      setRental(rentalData);
      setPhotos(photosData);

      // Auto-select return phase if rental is returning or completed
      if (
        !isRefresh &&
        ['awaiting_return', 'inspection', 'completed'].includes(rentalData.status)
      ) {
        setPhase('return');
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

  const handlePickAndUpload = async (slotType: PhotoType, source: 'camera' | 'library') => {
    if (!token || !rentalID || uploadingSlot) return;

    try {
      const permission =
        source === 'camera'
          ? await ImagePicker.requestCameraPermissionsAsync()
          : await ImagePicker.requestMediaLibraryPermissionsAsync();

      if (!permission.granted) {
        Alert.alert(
          'Нет доступа',
          source === 'camera'
            ? 'Предоставьте приложению доступ к камере в настройках устройства.'
            : 'Предоставьте приложению доступ к галерее в настройках устройства.',
        );
        return;
      }

      const result =
        source === 'camera'
          ? await ImagePicker.launchCameraAsync({
              mediaTypes: ['images'],
              quality: 0.85,
            })
          : await ImagePicker.launchImageLibraryAsync({
              mediaTypes: ['images'],
              quality: 0.85,
            });

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
        'Ошибка загрузки',
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
            {rental?.order_number ? `Заказ №${rental.order_number}` : 'Контроль состояния оборудования'}
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
          </Pressable>

          <Pressable
            style={[
              styles.phaseButton,
              phase === 'return' && styles.phaseButtonActive,
            ]}
            onPress={() => setPhase('return')}
          >
            <Ionicons
              name="return-down-back-outline"
              size={18}
              color={phase === 'return' ? colors.white : colors.ink}
            />
            <Text
              style={[
                styles.phaseButtonText,
                phase === 'return' && styles.phaseButtonTextActive,
              ]}
            >
              При возврате
            </Text>
          </Pressable>
        </View>

        {/* Progress banner */}
        <View style={styles.banner}>
          <View style={styles.bannerHeader}>
            <Ionicons
              name={phasePhotosCount === 5 ? 'checkmark-circle' : 'information-circle-outline'}
              size={22}
              color={phasePhotosCount === 5 ? colors.success : colors.primary}
            />
            <Text style={styles.bannerTitle}>
              {phase === 'handover' ? 'Осмотр при выдаче: ' : 'Осмотр при возврате: '}
              {phasePhotosCount} из 5 ракурсов
            </Text>
          </View>
          <Text style={styles.bannerText}>
            {phasePhotosCount === 5
              ? 'Все обязательные ракурсы зафиксированы. Фотографии прикреплены к сделке и акту приема-передачи.'
              : 'Сделайте или прикрепите фото по каждому из 5 пунктов ниже для фиксации состояния.'}
          </Text>
        </View>

        {error ? <Text style={styles.errorText}>{error}</Text> : null}

        {/* 5 Slots */}
        <View style={styles.slotsList}>
          {PHOTO_SLOTS.map((slot) => {
            const photo = getPhotoForSlot(slot.type);
            const isUploading = uploadingSlot === slot.type;
            const photoUrl = photo?.download_url || (photo ? `/api/v1/rentals/${rentalID}/photos/${photo.id}` : '');

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
                    <View style={styles.badgePending}>
                      <Text style={styles.badgePendingText}>Нужно фото</Text>
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

                {/* Action Buttons */}
                <View style={styles.actionRow}>
                  {isUploading ? (
                    <View style={styles.loadingBox}>
                      <ActivityIndicator size="small" color={colors.primary} />
                      <Text style={styles.loadingText}>Сохраняем снимок...</Text>
                    </View>
                  ) : (
                    <>
                      <Pressable
                        style={styles.slotButton}
                        onPress={() => void handlePickAndUpload(slot.type, 'camera')}
                      >
                        <Ionicons name="camera-outline" size={18} color={colors.primary} />
                        <Text style={styles.slotButtonText}>
                          {photo ? 'Переснять' : 'Камера'}
                        </Text>
                      </Pressable>

                      <Pressable
                        style={[styles.slotButton, styles.slotButtonOutline]}
                        onPress={() => void handlePickAndUpload(slot.type, 'library')}
                      >
                        <Ionicons name="images-outline" size={18} color={colors.ink} />
                        <Text style={[styles.slotButtonText, { color: colors.ink }]}>
                          Галерея
                        </Text>
                      </Pressable>
                    </>
                  )}
                </View>
              </View>
            );
          })}
        </View>
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
  phaseButtonText: {
    fontSize: 14,
    fontWeight: '600',
    color: colors.ink,
  },
  phaseButtonTextActive: {
    color: colors.white,
  },
  banner: {
    backgroundColor: colors.surfaceSubtle,
    borderRadius: radius.md,
    padding: spacing.md,
    gap: spacing.xs,
    borderWidth: 1,
    borderColor: colors.outline,
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
  bannerText: {
    ...typography.caption,
    color: colors.muted,
    lineHeight: 18,
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
  badgePendingText: {
    fontSize: 12,
    fontWeight: '600',
    color: colors.muted,
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
    gap: spacing.sm,
  },
  slotButton: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 10,
    borderRadius: radius.sm,
    backgroundColor: colors.primarySoft,
    gap: 6,
  },
  slotButtonOutline: {
    backgroundColor: colors.surfaceSubtle,
  },
  slotButtonText: {
    fontSize: 13,
    fontWeight: '700',
    color: colors.primary,
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
});

import DateTimePicker, {
  type DateTimePickerEvent,
} from '@react-native-community/datetimepicker';
import { useRef, useState } from 'react';
import { Keyboard, Modal, Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import type { ProfilePatch } from '../types/api';
import { colors, radius, spacing } from '../theme/tokens';
import { Button, Field } from './ui';

const invalidFullNameCharacters = /[^A-Za-zА-Яа-яЁё\s'-]/gu;
const validFullName = /^[A-Za-zА-Яа-яЁё]+(?:[\s'-][A-Za-zА-Яа-яЁё]+)*$/u;
const validEmail = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/u;

export function getFullNameError(value: string): string {
  const normalized = value.trim();
  if (!normalized) return 'Введите ФИО.';
  if ([...normalized].length < 2) return 'ФИО должно содержать не менее 2 символов.';
  if ([...normalized].length > 200) return 'ФИО не должно быть длиннее 200 символов.';
  if (!validFullName.test(normalized)) {
    return 'Используйте только буквы, пробел, дефис или апостроф.';
  }
  return '';
}

export function getEmailError(value: string): string {
  const normalized = value.trim();
  if (!normalized) return '';
  return validEmail.test(normalized) ? '' : 'Проверьте формат email.';
}

function defaultPickerDate(): Date {
  const date = new Date();
  date.setFullYear(date.getFullYear() - 18);
  date.setHours(12, 0, 0, 0);
  return date;
}

function parseBirthDate(value: string): Date {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return defaultPickerDate();
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]), 12);
  return Number.isNaN(date.getTime()) ? defaultPickerDate() : date;
}

function formatBirthDateValue(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

export function formatBirthDateLabel(value: string): string {
  if (!value) return '';
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  return match ? `${match[3]}.${match[2]}.${match[1]}` : value;
}

function getBirthDateError(value: string): string {
  if (!value) return 'Выберите дату рождения.';
  const date = parseBirthDate(value);
  if (formatBirthDateValue(date) !== value) return 'Проверьте дату рождения.';
  if (date > new Date()) return 'Дата рождения не может быть в будущем.';
  return '';
}

function formatManualDate(value: string): string {
  const digits = value.replace(/\D/g, '').slice(0, 8);
  if (digits.length <= 2) return digits;
  if (digits.length <= 4) return `${digits.slice(0, 2)}.${digits.slice(2)}`;
  return `${digits.slice(0, 2)}.${digits.slice(2, 4)}.${digits.slice(4)}`;
}

function manualDateToISO(value: string): string {
  const match = /^(\d{2})\.(\d{2})\.(\d{4})$/.exec(value);
  return match ? `${match[3]}-${match[2]}-${match[1]}` : '';
}

export function ProfileForm({
  initialFullName = '',
  initialBirthDate = '',
  initialEmail = '',
  initialClientType = 'individual',
  initialCompanyName = '',
  initialINN = '',
  initialKPP = '',
  initialOGRN = '',
  initialLegalAddress = '',
  initialCompanyContact = '',
  submitLabel,
  onSubmit,
}: {
  initialFullName?: string;
  initialBirthDate?: string;
  initialEmail?: string;
  initialClientType?: 'individual' | 'legal_entity';
  initialCompanyName?: string;
  initialINN?: string;
  initialKPP?: string;
  initialOGRN?: string;
  initialLegalAddress?: string;
  initialCompanyContact?: string;
  submitLabel: string;
  onSubmit: (profile: ProfilePatch) => Promise<void>;
}) {
  const [fullName, setFullName] = useState(initialFullName);
  const [birthDate, setBirthDate] = useState(initialBirthDate);
  const [manualDate, setManualDate] = useState(formatBirthDateLabel(initialBirthDate));
  const [email, setEmail] = useState(initialEmail);
  const [clientType, setClientType] = useState(initialClientType);
  const [companyName, setCompanyName] = useState(initialCompanyName);
  const [inn, setINN] = useState(initialINN);
  const [kpp, setKPP] = useState(initialKPP);
  const [ogrn, setOGRN] = useState(initialOGRN);
  const [legalAddress, setLegalAddress] = useState(initialLegalAddress);
  const [companyContact, setCompanyContact] = useState(initialCompanyContact);
  const [fullNameError, setFullNameError] = useState('');
  const [birthDateError, setBirthDateError] = useState('');
  const [emailError, setEmailError] = useState('');
  const [datePickerVisible, setDatePickerVisible] = useState(false);
  const [pickerDate, setPickerDate] = useState(() => parseBirthDate(initialBirthDate));
  const [saving, setSaving] = useState(false);
  const submissionInFlight = useRef(false);

  const updateFullName = (value: string) => {
    const sanitized = value.replace(invalidFullNameCharacters, '');
    setFullName(sanitized);
    if (sanitized !== value) {
      setFullNameError('Используйте только буквы, пробел, дефис или апостроф.');
    } else if (fullNameError) {
      setFullNameError(getFullNameError(sanitized));
    }
  };

  const openDatePicker = () => {
    setPickerDate(parseBirthDate(birthDate));
    setDatePickerVisible(true);
  };

  const selectBirthDate = (event: DateTimePickerEvent, selectedDate?: Date) => {
    if (Platform.OS === 'android') setDatePickerVisible(false);
    if (event.type === 'dismissed' || !selectedDate) return;

    if (Platform.OS === 'ios') {
      setPickerDate(selectedDate);
    } else {
      const value = formatBirthDateValue(selectedDate);
      setBirthDate(value);
      setBirthDateError('');
    }
  };

  const submit = async () => {
    if (submissionInFlight.current) return;
    const nextFullNameError = getFullNameError(fullName);
    const nextBirthDateError = clientType === 'individual' ? getBirthDateError(birthDate) : '';
    const nextEmailError = getEmailError(email);
    setFullNameError(nextFullNameError);
    setBirthDateError(nextBirthDateError);
    setEmailError(nextEmailError);
    if (nextFullNameError || nextBirthDateError || nextEmailError) return;
    if (
      clientType === 'legal_entity' &&
      (!companyName.trim() ||
        !/^(?:\d{10}|\d{12})$/u.test(inn) ||
        !/^(?:\d{13}|\d{15})$/u.test(ogrn) ||
        !legalAddress.trim() ||
        !companyContact.trim() ||
        !email.trim())
    ) {
      return;
    }

    submissionInFlight.current = true;
    setSaving(true);
    Keyboard.dismiss();
    try {
      await onSubmit({
        full_name: fullName.trim(),
        email: email.trim(),
        client_type: clientType,
        ...(clientType === 'individual' ? { birth_date: birthDate } : {}),
        ...(clientType === 'legal_entity' ? {
          company_name: companyName.trim(),
          inn,
          kpp,
          ogrn,
          legal_address: legalAddress.trim(),
          company_contact: companyContact.trim(),
        } : {}),
      });
    } catch {
      // The parent surface owns the user-facing request error.
    } finally {
      submissionInFlight.current = false;
      setSaving(false);
    }
  };

  const formValid =
    !getFullNameError(fullName) &&
    (clientType === 'legal_entity' || !getBirthDateError(birthDate)) &&
    !getEmailError(email) &&
    (clientType === 'individual' || Boolean(
      companyName.trim() && /^(?:\d{10}|\d{12})$/u.test(inn) &&
      (!kpp || /^\d{9}$/u.test(kpp)) && /^(?:\d{13}|\d{15})$/u.test(ogrn) &&
      legalAddress.trim() && companyContact.trim() && email.trim()
    ));

  return (
    <>
      <View style={styles.form}>
        <View style={styles.typeGroup}>
          <Text style={styles.typeLabel}>Тип клиента</Text>
          <View style={styles.segmented}>
            {([['individual', 'Физлицо'], ['legal_entity', 'Юрлицо']] as const).map(([value, label]) => (
              <Pressable
                accessibilityRole="radio"
                accessibilityState={{ checked: clientType === value }}
                key={value}
                onPress={() => setClientType(value)}
                style={[styles.segment, clientType === value && styles.segmentActive]}
              >
                <Text style={[styles.segmentText, clientType === value && styles.segmentTextActive]}>{label}</Text>
              </Pressable>
            ))}
          </View>
        </View>
        <Field
          autoCapitalize="words"
          error={fullNameError}
          label="ФИО"
          onBlur={() => setFullNameError(getFullNameError(fullName))}
          onChangeText={updateFullName}
          placeholder="Иванов Иван Иванович"
          value={fullName}
        />
        {clientType === 'individual' && (Platform.OS === 'ios' || Platform.OS === 'android') ? (
          <Field
            error={birthDateError}
            label="Дата рождения"
            onPress={openDatePicker}
            placeholder="Выберите дату"
            trailingIcon="calendar-outline"
            value={formatBirthDateLabel(birthDate)}
          />
        ) : clientType === 'individual' ? (
          <Field
            error={birthDateError}
            keyboardType="number-pad"
            label="Дата рождения"
            maxLength={10}
            onBlur={() => setBirthDateError(getBirthDateError(birthDate))}
            onChangeText={(value) => {
              const formatted = formatManualDate(value);
              setManualDate(formatted);
              setBirthDate(manualDateToISO(formatted));
            }}
            placeholder="ДД.ММ.ГГГГ"
            value={manualDate}
          />
        ) : null}
        {clientType === 'legal_entity' ? (
          <>
            <Field label="Название организации" onChangeText={setCompanyName} placeholder="ООО «Про Инструмент»" value={companyName} />
            <Field keyboardType="number-pad" label="ИНН" maxLength={12} onChangeText={(value) => setINN(value.replace(/\D/g, ''))} placeholder="10 или 12 цифр" value={inn} />
            <Field keyboardType="number-pad" label="КПП (при наличии)" maxLength={9} onChangeText={(value) => setKPP(value.replace(/\D/g, ''))} placeholder="9 цифр" value={kpp} />
            <Field keyboardType="number-pad" label="ОГРН / ОГРНИП" maxLength={15} onChangeText={(value) => setOGRN(value.replace(/\D/g, ''))} placeholder="13 или 15 цифр" value={ogrn} />
            <Field label="Юридический адрес" maxLength={1000} onChangeText={setLegalAddress} placeholder="Индекс, город, улица, дом" value={legalAddress} />
            <Field label="ФИО / контакт" maxLength={200} onChangeText={setCompanyContact} placeholder="Контактное лицо" value={companyContact} />
          </>
        ) : null}
        <Field
          autoCapitalize="none"
          error={emailError}
          keyboardType="email-address"
          label="Email (необязательно)"
          onBlur={() => setEmailError(getEmailError(email))}
          onChangeText={(value) => {
            setEmail(value);
            if (emailError) setEmailError(getEmailError(value));
          }}
          placeholder="name@example.ru"
          value={email}
        />
        <Button
          disabled={!formValid}
          label={submitLabel}
          loading={saving}
          onPress={() => void submit()}
        />
      </View>

      {clientType === 'individual' && Platform.OS === 'android' && datePickerVisible ? (
        <DateTimePicker
          display="calendar"
          maximumDate={new Date()}
          mode="date"
          onChange={selectBirthDate}
          value={pickerDate}
        />
      ) : null}

      <Modal
        animationType="slide"
        onRequestClose={() => setDatePickerVisible(false)}
        transparent
        visible={clientType === 'individual' && Platform.OS === 'ios' && datePickerVisible}
      >
        <View style={styles.pickerOverlay}>
          <View style={styles.pickerSheet}>
            <Text style={styles.pickerTitle}>Дата рождения</Text>
            <DateTimePicker
              display="inline"
              locale="ru-RU"
              maximumDate={new Date()}
              mode="date"
              onChange={selectBirthDate}
              style={styles.picker}
              value={pickerDate}
            />
            <View style={styles.pickerActions}>
              <View style={styles.pickerAction}>
                <Button
                  label="Отмена"
                  onPress={() => setDatePickerVisible(false)}
                  variant="secondary"
                />
              </View>
              <View style={styles.pickerAction}>
                <Button
                  label="Выбрать"
                  onPress={() => {
                    const value = formatBirthDateValue(pickerDate);
                    setBirthDate(value);
                    setBirthDateError('');
                    setDatePickerVisible(false);
                  }}
                />
              </View>
            </View>
          </View>
        </View>
      </Modal>
    </>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.lg },
  typeGroup: { gap: spacing.sm },
  typeLabel: { color: colors.ink, fontSize: 14, lineHeight: 19, fontWeight: '600' },
  segmented: { flexDirection: 'row', gap: spacing.xs, padding: spacing.xs, borderRadius: radius.button, backgroundColor: colors.surfaceStrong },
  segment: { flex: 1, minHeight: 44, alignItems: 'center', justifyContent: 'center', borderRadius: radius.sm },
  segmentActive: { backgroundColor: colors.surface },
  segmentText: { color: colors.muted, fontSize: 14, lineHeight: 19, fontWeight: '700' },
  segmentTextActive: { color: colors.primary },
  pickerOverlay: {
    flex: 1,
    justifyContent: 'flex-end',
    padding: spacing.lg,
    backgroundColor: colors.scrim,
  },
  pickerSheet: {
    width: '100%',
    maxWidth: 420,
    alignSelf: 'center',
    padding: spacing.xl,
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
  },
  pickerTitle: {
    color: colors.ink,
    fontSize: 20,
    lineHeight: 26,
    fontWeight: '800',
  },
  picker: { width: '100%', minHeight: 330 },
  pickerActions: { flexDirection: 'row', gap: spacing.md },
  pickerAction: { flex: 1 },
});

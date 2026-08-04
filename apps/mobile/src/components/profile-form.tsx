import DateTimePicker, {
  type DateTimePickerEvent,
} from '@react-native-community/datetimepicker';
import { useRef, useState } from 'react';
import { Modal, Platform, StyleSheet, Text, View } from 'react-native';
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
  submitLabel,
  onSubmit,
}: {
  initialFullName?: string;
  initialBirthDate?: string;
  initialEmail?: string;
  submitLabel: string;
  onSubmit: (profile: ProfilePatch) => Promise<void>;
}) {
  const [fullName, setFullName] = useState(initialFullName);
  const [birthDate, setBirthDate] = useState(initialBirthDate);
  const [manualDate, setManualDate] = useState(formatBirthDateLabel(initialBirthDate));
  const [email, setEmail] = useState(initialEmail);
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
    const nextBirthDateError = getBirthDateError(birthDate);
    const nextEmailError = getEmailError(email);
    setFullNameError(nextFullNameError);
    setBirthDateError(nextBirthDateError);
    setEmailError(nextEmailError);
    if (nextFullNameError || nextBirthDateError || nextEmailError) return;

    submissionInFlight.current = true;
    setSaving(true);
    try {
      await onSubmit({
        full_name: fullName.trim(),
        birth_date: birthDate,
        email: email.trim(),
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
    !getBirthDateError(birthDate) &&
    !getEmailError(email);

  return (
    <>
      <View style={styles.form}>
        <Field
          autoCapitalize="words"
          error={fullNameError}
          label="ФИО"
          onBlur={() => setFullNameError(getFullNameError(fullName))}
          onChangeText={updateFullName}
          placeholder="Иванов Иван Иванович"
          value={fullName}
        />
        {Platform.OS === 'ios' || Platform.OS === 'android' ? (
          <Field
            error={birthDateError}
            label="Дата рождения"
            onPress={openDatePicker}
            placeholder="Выберите дату"
            trailingIcon="calendar-outline"
            value={formatBirthDateLabel(birthDate)}
          />
        ) : (
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
        )}
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

      {Platform.OS === 'android' && datePickerVisible ? (
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
        visible={Platform.OS === 'ios' && datePickerVisible}
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

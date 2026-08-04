export const colors = {
  background: '#F5F6F4',
  surface: '#FFFFFF',
  surfaceSubtle: '#EEF1EF',
  surfaceStrong: '#E2E7E4',
  ink: '#10201C',
  muted: '#586A64',
  primary: '#103F59',
  primaryPressed: '#092D42',
  primarySoft: '#DCEAF0',
  accent: '#E9682A',
  accentSoft: '#FDE9DF',
  success: '#1E6A4A',
  successSoft: '#DFF2E8',
  warning: '#98501B',
  warningSoft: '#FCEBDC',
  error: '#B32632',
  errorSoft: '#F9E3E5',
  outline: '#D3DCD8',
  imageOutline: 'rgba(0, 0, 0, 0.10)',
  scrim: 'rgba(16, 32, 28, 0.44)',
  white: '#FFFFFF',
} as const;

export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  xxl2: 24,
  xxl: 32,
  hero: 48,
} as const;

export const typography = {
  title: { fontSize: 30, lineHeight: 36, fontWeight: '800' as const, letterSpacing: -0.7 },
  section: { fontSize: 19, lineHeight: 24, fontWeight: '700' as const },
  body: { fontSize: 16, lineHeight: 23 },
  label: { fontSize: 14, lineHeight: 19, fontWeight: '600' as const },
  caption: { fontSize: 13, lineHeight: 18 },
} as const;

export const radius = {
  sm: 8,
  md: 12,
  lg: 16,
  button: 12,
  pill: 999,
} as const;

export const shadow = {
  shadowColor: '#10201C',
  shadowOffset: { width: 0, height: 2 },
  shadowOpacity: 0.06,
  shadowRadius: 6,
  elevation: 1,
} as const;

export const controlHeights = {
  compact: 44,
  default: 52,
  large: 56,
} as const;

export const iconSizes = {
  sm: 16,
  md: 20,
  lg: 24,
  xl: 32,
} as const;

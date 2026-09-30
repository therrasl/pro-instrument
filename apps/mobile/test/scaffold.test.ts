import { describe, expect, it } from 'vitest';
import appJson from '../app.json';

describe('Android form layout', () => {
  it('resizes the activity when the keyboard opens', () => {
    expect(appJson.expo.android.softwareKeyboardLayoutMode).toBe('resize');
  });
});

import { describe, expect, it } from 'vitest';

import { sharedPackageName } from './index.js';

describe('shared package', () => {
  it('exposes its package name', () => {
    expect(sharedPackageName).toBe('@pro-instrument/shared');
  });
});

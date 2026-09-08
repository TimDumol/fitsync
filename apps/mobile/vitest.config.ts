import { defineConfig } from 'vitest/config';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const mobileRoot = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  resolve: {
    alias: {
      'expo-modules-core': path.join(mobileRoot, 'test/expo-modules-core.ts'),
    },
  },
});

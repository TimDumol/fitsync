import Constants from 'expo-constants';

type AppExtra = { buildTimestamp?: string };

const extra = Constants.expoConfig?.extra as AppExtra | undefined;

export const appVersion = Constants.expoConfig?.version?.trim() || 'unknown';
export const appBuildTimestamp = process.env.EXPO_PUBLIC_BUILD_TIMESTAMP?.trim() || extra?.buildTimestamp || 'unknown';

export const appBuildInfo = {
  version: appVersion,
  buildTimestamp: appBuildTimestamp,
};

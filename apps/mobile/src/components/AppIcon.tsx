import MaterialCommunityIcons from '@expo/vector-icons/MaterialCommunityIcons';
import type { ComponentProps } from 'react';

export type AppIconName = ComponentProps<typeof MaterialCommunityIcons>['name'];

export function AppIcon({ name, size = 20, color }: { name: AppIconName; size?: number; color: string }): JSX.Element {
  return <MaterialCommunityIcons name={name} size={size} color={color} />;
}

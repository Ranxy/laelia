// lib/toast.ts
//
// The single app-wide toast manager, created through Base UI's official
// factory so the Toast.Provider accepts it directly: no structural shim and
// no `as any` cast at the provider level (audit 07 F-Bug-6).

import { Toast } from "@base-ui/react/toast";

export const toastManager = Toast.createToastManager();

export type ToastOptions = Parameters<typeof toastManager.add>[0];

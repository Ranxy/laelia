import { create } from "zustand";
import type { AppStoreState } from "./types";
import { createAuthSlice } from "./auth";

export const useAppStore = create<AppStoreState>()((...args) => ({
  ...createAuthSlice(...args),
}));

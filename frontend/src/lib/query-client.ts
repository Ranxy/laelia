import { QueryClient } from "@tanstack/react-query";

// 应用级唯一 QueryClient：全应用唯一的服务器缓存入口（ADR-1），后续 slice
// 内核逐步迁入。retry=1 是弱网下单次静默刷新的兜底，避免瞬时抖动直接报错。
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 30_000, gcTime: 5 * 60_000, retry: 1 },
  },
});

import { useQuery } from "@tanstack/react-query";
import { api, ProviderNode } from "@/lib/api";

/**
 * Fetches nodes with inference providers discovered by the node-probe DaemonSet.
 * Auto-refreshes every 30s to match the probe interval.
 */
export function useProviderNodes(enabled: boolean) {
  return useQuery<ProviderNode[]>({
    queryKey: ["provider-nodes"],
    queryFn: () => api.providers.nodes(),
    enabled,
    staleTime: 30_000,
    refetchInterval: 30_000,
  });
}

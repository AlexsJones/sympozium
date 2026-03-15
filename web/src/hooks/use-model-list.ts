import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

// ── Static fallback model lists ──────────────────────────────────────────────

const OPENAI_MODELS = [
  "gpt-4o",
  "gpt-4o-mini",
  "gpt-4-turbo",
  "gpt-4",
  "gpt-3.5-turbo",
  "o1",
  "o1-mini",
  "o3-mini",
];

const ANTHROPIC_MODELS = [
  "claude-sonnet-4-20250514",
  "claude-3-5-haiku-20241022",
  "claude-3-opus-20240229",
  "claude-3-5-sonnet-20241022",
];

const AZURE_MODELS = ["gpt-4o", "gpt-4", "gpt-35-turbo"];

// ── Fetchers ─────────────────────────────────────────────────────────────────

async function fetchOpenAIModels(apiKey: string): Promise<string[]> {
  const res = await fetch("https://api.openai.com/v1/models", {
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  if (!res.ok) throw new Error("Failed to fetch OpenAI models");
  const data = await res.json();
  return (data.data as { id: string }[])
    .map((m) => m.id)
    .filter(
      (id) =>
        id.startsWith("gpt-") ||
        id.startsWith("o1") ||
        id.startsWith("o3") ||
        id.startsWith("o4")
    )
    .sort((a, b) => a.localeCompare(b));
}

async function fetchAnthropicModels(apiKey: string): Promise<string[]> {
  const res = await fetch("https://api.anthropic.com/v1/models", {
    headers: {
      "x-api-key": apiKey,
      "anthropic-version": "2023-06-01",
      "anthropic-dangerous-direct-browser-access": "true",
    },
  });
  if (!res.ok) throw new Error("Failed to fetch Anthropic models");
  const data = await res.json();
  return (data.data as { id: string }[]).map((m) => m.id).sort();
}

async function fetchProviderModelsViaProxy(baseURL: string): Promise<string[]> {
  const res = await api.providers.models(baseURL);
  return res.models;
}

// ── Hook ─────────────────────────────────────────────────────────────────────

/**
 * Fetches the model list for a given provider + API key.
 * For local providers (ollama, custom) with a baseURL, proxies through the backend.
 * Falls back to a curated static list if the API call fails or no key is given.
 */
export function useModelList(provider: string, apiKey: string, baseURL?: string) {
  const isLocalProvider = provider === "ollama" || provider === "custom";
  const canFetchLocal = isLocalProvider && !!baseURL;
  const canFetchCloud = !!apiKey && (provider === "openai" || provider === "anthropic");

  const query = useQuery<string[]>({
    queryKey: ["provider-models", provider, apiKey, baseURL],
    queryFn: async () => {
      if (canFetchLocal) return fetchProviderModelsViaProxy(baseURL!);
      if (provider === "openai" && apiKey) return fetchOpenAIModels(apiKey);
      if (provider === "anthropic" && apiKey) return fetchAnthropicModels(apiKey);
      throw new Error("no-fetch");
    },
    enabled: canFetchLocal || canFetchCloud,
    staleTime: 5 * 60 * 1000, // cache 5 min
    retry: false,
  });

  // Static fallback when fetch isn't available or failed
  const fallback = (() => {
    switch (provider) {
      case "openai":
        return OPENAI_MODELS;
      case "anthropic":
        return ANTHROPIC_MODELS;
      case "azure-openai":
        return AZURE_MODELS;
      default:
        return [];
    }
  })();

  return {
    models: query.data ?? fallback,
    isLoading: query.isLoading && query.fetchStatus !== "idle",
    isLive: !!query.data, // true if we got real data from the API
  };
}

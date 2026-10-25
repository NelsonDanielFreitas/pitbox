export const env = {
  apiBaseUrl: window.location.origin,
  isDev: import.meta.env.DEV,
} as const;

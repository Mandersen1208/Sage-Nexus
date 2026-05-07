const defaultManagerUrl = `${window.location.protocol}//${window.location.hostname}:8090`;

export const MANAGER_URL: string =
  import.meta.env["VITE_MANAGER_URL"] ?? defaultManagerUrl;
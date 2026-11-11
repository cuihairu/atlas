// Dark/light theme preference (TODO v0.1.18): module-level state with the
// same listener pattern as i18n, persisted to localStorage. Default dark
// (the original dashboard look).
import { useEffect, useReducer } from 'react';

export type ThemeMode = 'dark' | 'light';

let current: ThemeMode = 'dark';
try {
  const saved = localStorage.getItem('atlas-theme');
  if (saved === 'light' || saved === 'dark') current = saved;
} catch {
  // storage unavailable → keep default
}

const listeners = new Set<() => void>();

export function getTheme(): ThemeMode {
  return current;
}

export function setTheme(mode: ThemeMode): void {
  if (mode === current) return;
  current = mode;
  try {
    localStorage.setItem('atlas-theme', mode);
  } catch {
    // ignore
  }
  listeners.forEach((fn) => fn());
}

/** React binding: re-renders the component when the theme changes. */
export function useTheme(): ThemeMode {
  const [, force] = useReducer((x: number) => x + 1, 0);
  useEffect(() => {
    listeners.add(force);
    return () => {
      listeners.delete(force);
    };
  }, [force]);
  return current;
}

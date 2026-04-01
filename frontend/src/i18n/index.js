import { register, init, waitLocale, getLocaleFromNavigator } from 'svelte-i18n';

export const SUPPORTED_LOCALES = [
  { code: 'en', label: 'English' },
  { code: 'fr', label: 'Français' },
  { code: 'de', label: 'Deutsch' },
  { code: 'es', label: 'Español' },
  { code: 'zh', label: '中文' },
];

register('en', () => import('./en.json'));
register('fr', () => import('./fr.json'));
register('de', () => import('./de.json'));
register('es', () => import('./es.json'));
register('zh', () => import('./zh.json'));

/**
 * Initialize i18n with saved locale or browser detection.
 * Call this before mounting the Svelte app.
 */
export function setupI18n() {
  let savedLocale;
  try {
    const settings = JSON.parse(localStorage.getItem('settings') || '{}');
    savedLocale = settings.locale;
  } catch (e) {
    // ignore
  }

  const detected = getLocaleFromNavigator()?.split('-')[0] || 'en';
  const supported = SUPPORTED_LOCALES.map(l => l.code);
  const initialLocale = savedLocale && supported.includes(savedLocale) ? savedLocale : (supported.includes(detected) ? detected : 'en');

  init({
    fallbackLocale: 'en',
    initialLocale,
  });

  return waitLocale();
}

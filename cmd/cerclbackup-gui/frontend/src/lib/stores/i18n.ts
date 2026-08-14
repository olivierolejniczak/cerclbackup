import { writable, derived } from 'svelte/store';
import en from '../../locales/en.json';
import fr from '../../locales/fr.json';
import { GetLanguage, SetLanguage } from '../../../wailsjs/go/main/App';

type Dict = Record<string, string>;
const dicts: Record<string, Dict> = { en, fr };

export const lang = writable<string>('en');

GetLanguage().then((l) => lang.set(l)).catch(() => {});

export function setLang(l: string) {
  lang.set(l);
  SetLanguage(l).catch(() => {});
}

export const t = derived(lang, ($lang) => {
  const dict = dicts[$lang] ?? dicts.en;
  return (key: string, vars?: Record<string, string | number>) => {
    let s = dict[key] ?? dicts.en[key] ?? key;
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        s = s.replaceAll(`{${k}}`, String(v));
      }
    }
    return s;
  };
});

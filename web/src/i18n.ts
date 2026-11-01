import en from './locales/en.json'
export type Language = 'zh-CN' | 'en'
const saved=localStorage.getItem('viya-language')
export const language:Language=saved==='en'||(!saved&&!navigator.language.toLowerCase().startsWith('zh'))?'en':'zh-CN'
document.documentElement.lang=language
export function setLanguage(next:Language){if(next===language)return;localStorage.setItem('viya-language',next);window.location.reload()}

// Network failures surface as a bare "Failed to fetch" TypeError message; map
// the common variants to a friendly, actionable prompt instead of leaking the
// raw English string into every error banner.
const friendlyErrors: Record<string, string> = {
  'Failed to fetch': '网络请求失败，请检查服务是否运行',
  'NetworkError when attempting to fetch resource.': '网络请求失败，请检查服务是否运行',
  'Load failed': '网络请求失败，请检查服务是否运行',
  'Failed to fetch dynamically imported module': '页面资源加载失败，请刷新重试',
}
const friendlyErrorsEn: Record<string, string> = {
  'Failed to fetch': 'Network request failed. Check whether the service is running.',
  'NetworkError when attempting to fetch resource.': 'Network request failed.',
  'Load failed': 'Network request failed.',
  'Failed to fetch dynamically imported module': 'A page resource failed to load. Refresh to retry.',
}
export function t(text:string|undefined|null):string {
  if(!text)return "";
  if(language==='en')return (en as Record<string,string>)[text]??friendlyErrorsEn[text]??text;
  return friendlyErrors[text]??text;
}

export function tr(parts:TemplateStringsArray,...values:unknown[]):string {const key=parts.reduce((out,part,i)=>out+(i?'{'+(i-1)+'}':'')+part,'');const result=t(key);return result.replace(/\{(\d+)\}/g,(_,i)=>String(values[Number(i)]??''))}

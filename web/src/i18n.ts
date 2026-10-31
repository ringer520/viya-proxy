import en from './locales/en.json'
export type Language = 'zh-CN' | 'en'
const saved=localStorage.getItem('viya-language')
export const language:Language=saved==='en'||(!saved&&!navigator.language.toLowerCase().startsWith('zh'))?'en':'zh-CN'
document.documentElement.lang=language
export function t(text:string|undefined|null):string {if(!text)return "";return language==='en'?(en as Record<string,string>)[text]??text:text}
export function setLanguage(next:Language){if(next===language)return;localStorage.setItem('viya-language',next);window.location.reload()}

export function tr(parts:TemplateStringsArray,...values:unknown[]):string {const key=parts.reduce((out,part,i)=>out+(i?'{'+(i-1)+'}':'')+part,'');const result=t(key);return result.replace(/\{(\d+)\}/g,(_,i)=>String(values[Number(i)]??''))}

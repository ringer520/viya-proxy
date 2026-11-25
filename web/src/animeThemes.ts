import sakuraPreview from './assets/themes/sakura-preview.webp'
import neonPreview from './assets/themes/neon-preview.webp'
import lunarPreview from './assets/themes/lunar-preview.webp'

/** Artwork is bundled locally; no third-party image requests or runtime downloads. */
export const animeThemes = [
  { id: 'sakura', label: '樱庭花信', subtitle: 'SAKURA ATELIER', hint: '樱花 · 学院 · 柔光', accent: '#a84d74', preview: sakuraPreview },
  { id: 'neon', label: '霓虹回路', subtitle: 'NEON CIRCUIT', hint: '都市 · 科幻 · 霓虹', accent: '#79e6f1', preview: neonPreview },
  { id: 'lunar', label: '月海星谣', subtitle: 'LUNAR REVERIE', hint: '月夜 · 幻想 · 星辉', accent: '#d9c391', preview: lunarPreview },
] as const
